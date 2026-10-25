package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"agent-vm/internal/agent"
	"agent-vm/internal/bootstrap"
	"agent-vm/internal/host"
	"agent-vm/internal/model"
	"agent-vm/internal/tart"
)

type SetupPlan struct {
	Selection                                Selection
	Agent, Image, Identity                   string
	Managed, Create, SSH, Tools, InstallTart bool
}

func (p SetupPlan) Needed() bool { return p.Create || p.SSH || p.Tools || p.InstallTart }
func (p SetupPlan) Summary() string {
	var steps []string
	if p.InstallTart {
		steps = append(steps, "Download checksum-verified Tart into the private data directory (review license: https://tart.run/licensing/)")
	}
	if p.Create {
		steps = append(steps, "Download/cache upstream image "+p.Image+" and create persistent VM "+p.Selection.VM)
	}
	if p.SSH {
		steps = append(steps, "Configure dedicated SSH access and verify the guest host key through Tart")
	}
	if p.Tools {
		steps = append(steps, "Install "+p.Agent+" and required tools inside the guest; downloads may be large")
	}
	return strings.Join(steps, "\n")
}

type setupState struct {
	Image         string            `json:"image"`
	SSH           bool              `json:"ssh"`
	Agents        map[string]string `json:"agents"`
	PendingAgents map[string]string `json:"pending_agents,omitempty"`
}

func (a *App) setupPath(id string) string { return filepath.Join(a.Data, "bootstrap", id+".json") }
func (a *App) readSetup(v model.VM) (setupState, bool, error) {
	var state setupState
	b, err := os.ReadFile(a.setupPath(v.ID))
	if os.IsNotExist(err) {
		return state, false, nil
	}
	if err != nil {
		return state, false, err
	}
	if err = json.Unmarshal(b, &state); err != nil {
		return state, false, fmt.Errorf("read VM setup state: %w", err)
	}
	if state.Image != v.Image || state.Agents == nil {
		return state, false, fmt.Errorf("VM setup state does not match owned image; refusing automatic bootstrap")
	}
	if state.PendingAgents == nil {
		state.PendingAgents = map[string]string{}
	}
	return state, true, nil
}
func (a *App) writeSetup(v model.VM, state setupState) error {
	path := a.setupPath(v.ID)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	b, err := json.Marshal(state)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), "setup-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
func setupVersion(name string) (string, error) {
	adapter, err := agent.Get(name)
	if err != nil {
		return "", err
	}
	pins, err := bootstrap.Pins()
	return adapter.Version + "/" + pins.Revision, err
}

// PlanSetup resolves --new once, but makes no VM, key, or package changes.
func (a *App) PlanSetup(ctx context.Context, o Selection, selected string) (SetupPlan, error) {
	unlock, err := a.lock(ctx)
	if err != nil {
		return SetupPlan{}, err
	}
	defer unlock()
	return a.planSetupLocked(ctx, o, selected)
}
func (a *App) planSetupLocked(ctx context.Context, o Selection, selected string) (SetupPlan, error) {
	version, err := setupVersion(selected)
	if err != nil {
		return SetupPlan{}, err
	}
	name, osName, err := Select(a.Config, o)
	if err != nil {
		return SetupPlan{}, err
	}
	p := SetupPlan{Selection: Selection{VM: name, OS: osName}, Agent: selected, Image: a.Config.Guest(osName).BaseImage, Identity: a.Config.Effective(a.Data).IdentityFile}
	vs, err := a.Store.VMs(ctx)
	if err != nil {
		return p, err
	}
	found := false
	for _, v := range vs {
		if v.Name != name && v.ID != name {
			continue
		}
		found = true
		if o.OS != "" && o.OS != v.OS {
			return p, fmt.Errorf("VM %s is %s, incompatible with --os %s", v.Name, v.OS, o.OS)
		}
		p.Selection = Selection{VM: v.Name, OS: v.OS}
		p.Image = v.Image
		state, managed, err := a.readSetup(v)
		if err != nil {
			return p, err
		}
		p.Managed = managed
		p.SSH = managed && !state.SSH
		p.Tools = managed && state.Agents[selected] != version && state.PendingAgents[selected] != version
		break
	}
	if !found && a.Config.Builtin(osName) {
		p.Managed = true
		p.Create = true
		p.SSH = true
		p.Tools = true
	}
	if !p.Managed {
		return p, nil
	}
	if a.Config.Guest(p.Selection.OS).User != "admin" {
		return p, fmt.Errorf("upstream defaults require guest user admin; use a prepared custom image for a different user")
	}
	if err = a.Tart.Check(ctx); err != nil {
		var versionErr *tart.UnsupportedVersion
		privateAlternative := errors.As(err, &versionErr) && a.Tart.Executable() == "tart"
		if !errors.Is(err, exec.ErrNotFound) && !privateAlternative {
			return p, err
		}
		p.InstallTart = true
	}
	if !p.Needed() {
		return p, nil
	}
	b, err := a.Runner.Run(ctx, host.Command{Name: "sw_vers", Args: []string{"-productVersion"}})
	if err != nil {
		return p, fmt.Errorf("check automatic setup host support: %w", err)
	}
	major, err := strconv.Atoi(strings.Split(strings.TrimSpace(string(b)), ".")[0])
	if err != nil || major < 14 || (p.Selection.OS == model.MacOS && major < 27) {
		return p, fmt.Errorf("automatic setup requires macOS 14+ for Linux and macOS 27+ for the default macOS 27 guest; current host: %s", strings.TrimSpace(string(b)))
	}
	return p, nil
}

// Setup executes only actions covered by the approved plan. Existing custom VMs
// never enter this path. The lock also protects identity generation and retries.
func (a *App) Setup(ctx context.Context, approved SetupPlan, progress io.Writer) (model.VM, error) {
	unlock, err := a.lock(ctx)
	if err != nil {
		return model.VM{}, err
	}
	defer unlock()
	p, err := a.planSetupLocked(ctx, approved.Selection, approved.Agent)
	if err != nil {
		return model.VM{}, err
	}
	if !p.Managed {
		return model.VM{}, fmt.Errorf("VM setup ownership changed; retry to review the current plan")
	}
	if (p.InstallTart && !approved.InstallTart) || (p.Create && !approved.Create) || (p.SSH && !approved.SSH) || (p.Tools && !approved.Tools) || p.Image != approved.Image || p.Identity != approved.Identity {
		return model.VM{}, fmt.Errorf("setup requirements changed; retry to approve the current plan")
	}
	stage := func(s string) {
		if progress != nil {
			fmt.Fprintln(progress, s)
		}
	}
	if p.InstallTart {
		stage("Downloading and verifying private Tart installation")
		if err = a.installTart(ctx, progress); err != nil {
			return model.VM{}, err
		}
		if err = a.Tart.Check(ctx); err != nil {
			return model.VM{}, err
		}
		stage("Tart installation verified")
	}
	var v model.VM
	if p.Create {
		stage("Downloading image and creating VM: " + p.Selection.VM)
		v, err = a.createLocked(ctx, p.Selection.VM, p.Selection.OS, progress)
	} else {
		v, err = a.Store.VM(ctx, p.Selection.VM)
	}
	if err != nil {
		return v, err
	}
	stage("Owned VM selected: " + v.Name)
	v, err = a.startLocked(ctx, v)
	if err != nil {
		return v, err
	}
	state, managed, err := a.readSetup(v)
	if err != nil {
		return v, err
	}
	if !managed {
		return v, fmt.Errorf("VM has no managed bootstrap intent; refusing automatic changes")
	}
	if !state.SSH {
		publicKey, err := a.ensureIdentity(ctx, p.Identity)
		if err != nil {
			return v, err
		}
		bootCtx, cancel := context.WithTimeout(ctx, time.Duration(a.Config.BootSeconds)*time.Second)
		err = a.Tart.AgentReady(bootCtx, v.Name)
		cancel()
		if err != nil {
			return v, err
		}
		script, err := bootstrap.SSH(a.Config.Guest(v.OS).User, publicKey, v.OS)
		if err != nil {
			return v, err
		}
		// Escalation is inside this disposable guest, never on the host.
		output, err := a.Tart.ExecRoot(ctx, v.Name, script, nil, progress)
		if err != nil {
			return v, fmt.Errorf("guest SSH bootstrap failed; VM %s preserved: %w", v.Name, err)
		}
		c, err := a.Connection(ctx, v, false)
		if err != nil {
			return v, err
		}
		if err = c.EnrollPublicKey(ctx, string(output), true); err != nil {
			return v, err
		}
		if _, err = a.Connection(ctx, v, true); err != nil {
			return v, err
		}
		state.SSH = true
		if err = a.writeSetup(v, state); err != nil {
			return v, err
		}
		stage("Guest SSH access and host key verified: " + v.Name)
	}
	version, err := setupVersion(p.Agent)
	if err != nil {
		return v, err
	}
	if state.Agents[p.Agent] != version {
		adapter, err := agent.Get(p.Agent)
		if err != nil {
			return v, err
		}
		c, err := a.Connection(ctx, v, true)
		if err != nil {
			return v, err
		}
		if state.PendingAgents[p.Agent] != version {
			script, err := bootstrap.Tools(v.OS, p.Agent)
			if err != nil {
				return v, err
			}
			logPath := filepath.Join(a.Data, "bootstrap", v.ID+"-"+p.Agent+".log")
			log, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
			if err != nil {
				return v, err
			}
			stage("Installing " + p.Agent + " in " + v.Name + "; diagnostics: " + logPath)
			_, installErr := c.Run(ctx, "/bin/sh -s", strings.NewReader(script), log, log, false)
			closeErr := log.Close()
			if installErr != nil {
				if p.Agent == "cursor" && v.OS == model.MacOS && host.ExitCode(installErr) == 42 {
					state.PendingAgents[p.Agent] = version
					if err = a.writeSetup(v, state); err != nil {
						return v, err
					}
					return v, &LoginRequired{Agent: p.Agent, VM: v.Name, UnlockKeychain: true}
				}
				return v, fmt.Errorf("guest tool installation failed; VM %s preserved; retry with --vm %s; diagnostics: %s: %w", v.Name, v.Name, logPath, installErr)
			}
			if closeErr != nil {
				return v, closeErr
			}
		}
		if err = a.checkAgentTools(ctx, c, adapter); err != nil {
			return v, err
		}
		state.Agents[p.Agent] = version
		delete(state.PendingAgents, p.Agent)
		if err = a.writeSetup(v, state); err != nil {
			return v, err
		}
		stage("Guest tools verified: " + p.Agent + " in " + v.Name)
	}
	return v, nil
}

func (a *App) ensureIdentity(ctx context.Context, path string) (string, error) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		if a.Config.IdentityFile != "" {
			return "", fmt.Errorf("configured SSH identity does not exist: %s", path)
		}
		if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			return "", err
		}
		dir, err := os.MkdirTemp(filepath.Dir(path), "key-")
		if err != nil {
			return "", err
		}
		defer os.RemoveAll(dir)
		temp := filepath.Join(dir, "id_ed25519")
		if _, err = a.Runner.Run(ctx, host.Command{Name: "ssh-keygen", Args: []string{"-q", "-t", "ed25519", "-N", "", "-f", temp}}); err != nil {
			return "", err
		}
		if err = os.Link(temp, path); err != nil {
			return "", err
		}
	} else if err != nil {
		return "", err
	} else if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return "", fmt.Errorf("SSH identity must be a regular private file with permissions 0600 or stricter")
	}
	if a.Config.IdentityFile != "" {
		if info, err := os.Lstat(path + ".pub"); err == nil && info.Mode().IsRegular() {
			b, err := os.ReadFile(path + ".pub")
			if err != nil {
				return "", err
			}
			return strings.TrimSpace(string(b)), nil
		}
	}
	b, err := a.Runner.Run(ctx, host.Command{Name: "ssh-keygen", Args: []string{"-y", "-P", "", "-f", path}})
	if err != nil {
		return "", fmt.Errorf("read SSH public identity without a password prompt: %w", err)
	}
	return strings.TrimSpace(string(b)), nil
}

func (a *App) installTart(ctx context.Context, progress io.Writer) error {
	pins, err := bootstrap.Pins()
	if err != nil {
		return err
	}
	parent := filepath.Join(a.Data, "tools")
	if err = os.MkdirAll(parent, 0700); err != nil {
		return err
	}
	dir, err := os.MkdirTemp(parent, "tart-install-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	archive := filepath.Join(dir, "tart.tar.gz")
	if _, err = a.Runner.Run(ctx, host.Command{Name: "curl", Args: []string{"--disable", "--fail", "--location", "--retry", "3", "--connect-timeout", "20", "--proto", "=https", "--proto-redir", "=https", pins.Tart.URL, "-o", archive}, Out: progress, Err: progress}); err != nil {
		return fmt.Errorf("download private Tart: %w", err)
	}
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	sum := sha256.New()
	_, err = io.Copy(sum, f)
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if hex.EncodeToString(sum.Sum(nil)) != pins.Tart.SHA256 {
		return fmt.Errorf("private Tart archive checksum mismatch; refusing installation")
	}
	extract := filepath.Join(dir, "extracted")
	if err = os.Mkdir(extract, 0700); err != nil {
		return err
	}
	if _, err = a.Runner.Run(ctx, host.Command{Name: "tar", Args: []string{"-xzf", archive, "-C", extract}, Err: progress}); err != nil {
		return fmt.Errorf("extract private Tart: %w", err)
	}
	return os.Rename(extract, filepath.Join(parent, "tart-"+pins.Tart.Version))
}
