package app_test

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"agent-vm/internal/app"
	"agent-vm/internal/host"
	"agent-vm/internal/model"
	u "agent-vm/internal/testutil"
)

type setupMachine struct {
	base        *machine
	mu          sync.Mutex
	tools       []string
	failTools   bool
	lockedTools bool
	failSSH     bool
	publicKey   string
	missingTart bool
	missingBrew bool
	hostVersion string
	tartVersion string
	failIP      bool
}

func (m *setupMachine) Run(ctx context.Context, c host.Command) ([]byte, error) {
	if c.Name == "curl" {
		return nil, os.WriteFile(c.Args[len(c.Args)-1], []byte("corrupt archive"), 0600)
	}
	if c.Name == "brew" {
		if m.missingBrew {
			return nil, exec.ErrNotFound
		}
		return []byte("Homebrew 6"), nil
	}
	if c.Name == "tart" && c.Args[0] == "--version" && m.tartVersion != "" {
		return []byte(m.tartVersion), nil
	}
	if c.Name == "tart" && c.Args[0] == "ip" && m.failIP {
		return nil, fmt.Errorf("IP readiness failed")
	}
	if c.Name == "tart" && m.missingTart {
		return nil, exec.ErrNotFound
	}
	if c.Name == "sw_vers" {
		if m.hostVersion != "" {
			return []byte(m.hostVersion), nil
		}
		return []byte("27.0.0"), nil
	}
	if c.Name == "ssh-keygen" {
		return (host.Exec{}).Run(ctx, c)
	}
	if c.Name == "tart" && c.Args[0] == "exec" {
		b, err := io.ReadAll(c.In)
		if err != nil {
			return nil, err
		}
		if string(b) == "true\n" {
			return nil, nil
		}
		m.mu.Lock()
		defer m.mu.Unlock()
		if m.failSSH {
			return nil, fmt.Errorf("SSH bootstrap interrupted")
		}
		return []byte(m.publicKey), nil
	}
	if c.Name == "ssh" && len(c.Args) > 1 && c.In != nil {
		b, err := io.ReadAll(c.In)
		if err != nil {
			return nil, err
		}
		c.In = strings.NewReader(string(b))
		if strings.Contains(string(b), "selected=") {
			m.mu.Lock()
			defer m.mu.Unlock()
			m.tools = append(m.tools, string(b))
			if m.lockedTools {
				return nil, &host.ExitError{Tool: "ssh", Code: 42}
			}
			if m.failTools {
				return nil, fmt.Errorf("installation interrupted")
			}
			return nil, nil
		}
	}
	return m.base.Run(ctx, c)
}
func (m *setupMachine) Detach(ctx context.Context, c host.Command, path string) error {
	return m.base.Detach(ctx, c, path)
}
func setupFixture(t *testing.T) (*app.App, *setupMachine) {
	t.Helper()
	a, base := fixture(t)
	a.Config.Linux.BaseImage = ""
	key := filepath.Join(t.TempDir(), "key")
	_, err := (host.Exec{}).Run(t.Context(), host.Command{Name: "ssh-keygen", Args: []string{"-q", "-t", "ed25519", "-N", "", "-f", key}})
	u.OK(t, err)
	b, err := os.ReadFile(key + ".pub")
	u.OK(t, err)
	m := &setupMachine{base: base, publicKey: string(b)}
	a.Runner = m
	a.Tart.Runner = m
	a.Config.IdentityFile = ""
	return a, m
}
func approve(app.SetupPlan) error { return nil }

func TestRunFirstSetupStopsBeforeConversationWhenLoginIsRequired(t *testing.T) {
	a, m := setupFixture(t)
	m.base.authMissing = true
	_, err := a.Run(t.Context(), app.RunOptions{Agent: "codex", Source: t.TempDir(), ApproveSetup: approve})
	u.Error(t, err)
	u.Equal(t, strings.Contains(err.Error(), "not authenticated"), true)
	u.Equal(t, m.base.clones, 1)
	u.Equal(t, len(m.tools), 1)
	sessions, listErr := a.Store.Sessions(t.Context())
	u.OK(t, listErr)
	u.Equal(t, len(sessions), 0)
}
func TestRunSetupDeclinedMakesNoVMKeyOrTools(t *testing.T) {
	a, m := setupFixture(t)
	_, err := a.Run(t.Context(), app.RunOptions{Agent: "codex", Source: t.TempDir(), ApproveSetup: func(app.SetupPlan) error { return fmt.Errorf("declined") }})
	u.Error(t, err)
	u.Equal(t, err.Error(), "declined")
	u.Equal(t, m.base.clones, 0)
	u.Equal(t, len(m.tools), 0)
	_, statErr := os.Stat(a.Config.Effective(a.Data).IdentityFile)
	u.Equal(t, os.IsNotExist(statErr), true)
}
func TestRunSetupRequiresExplicitConsent(t *testing.T) {
	a, m := setupFixture(t)
	_, err := a.Run(t.Context(), app.RunOptions{Agent: "codex", Source: t.TempDir()})
	u.Error(t, err)
	u.Equal(t, strings.Contains(err.Error(), "--yes"), true)
	u.Equal(t, m.base.clones, 0)
}
func TestRunNewUsesPreparedVMForLaunchAndRetry(t *testing.T) {
	a, m := setupFixture(t)
	s, err := a.Run(t.Context(), app.RunOptions{Selection: app.Selection{New: true}, Agent: "codex", Source: t.TempDir(), ApproveSetup: approve})
	u.OK(t, err)
	u.Equal(t, m.base.clones, 1)
	u.Equal(t, strings.HasPrefix(s.VM, "agent-linux-"), true)
	plan, err := a.PlanSetup(t.Context(), app.Selection{VM: s.VM}, "codex")
	u.OK(t, err)
	u.Equal(t, plan.Needed(), false)
	u.Equal(t, plan.Selection.VM, s.VM)
}
func TestRunInstallsOnlyRequestedAgent(t *testing.T) {
	a, m := setupFixture(t)
	p, err := a.PlanSetup(t.Context(), app.Selection{}, "codex")
	u.OK(t, err)
	v, err := a.Setup(t.Context(), p, nil)
	u.OK(t, err)
	u.Equal(t, strings.Contains(m.tools[0], "selected='codex'"), true)
	p, err = a.PlanSetup(t.Context(), app.Selection{VM: v.Name}, "claude")
	u.OK(t, err)
	u.Equal(t, p.Tools, true)
	u.Equal(t, p.SSH, false)
	m.base.agentVersion = "2.1.310"
	_, err = a.Setup(t.Context(), p, nil)
	u.OK(t, err)
	u.Equal(t, len(m.tools), 2)
	u.Equal(t, strings.Contains(m.tools[1], "selected='claude'"), true)
	p, err = a.PlanSetup(t.Context(), app.Selection{VM: v.Name}, "codex")
	u.OK(t, err)
	u.Equal(t, p.Needed(), false)
}
func TestManagedSetupResumesAfterInterruptedToolInstallation(t *testing.T) {
	a, m := setupFixture(t)
	p, err := a.PlanSetup(t.Context(), app.Selection{}, "codex")
	u.OK(t, err)
	m.failTools = true
	v, err := a.Setup(t.Context(), p, nil)
	u.Error(t, err)
	before, readErr := os.ReadFile(a.Config.Effective(a.Data).IdentityFile)
	u.OK(t, readErr)
	p, err = a.PlanSetup(t.Context(), app.Selection{VM: v.Name}, "codex")
	u.OK(t, err)
	u.Equal(t, p.SSH, false)
	u.Equal(t, p.Tools, true)
	m.failTools = false
	_, err = a.Setup(t.Context(), p, nil)
	u.OK(t, err)
	after, readErr := os.ReadFile(a.Config.Effective(a.Data).IdentityFile)
	u.OK(t, readErr)
	u.Equal(t, before, after)
	u.Equal(t, m.base.clones, 1)
}
func TestConcurrentSetupCreatesOneVM(t *testing.T) {
	a, m := setupFixture(t)
	p, err := a.PlanSetup(t.Context(), app.Selection{}, "codex")
	u.OK(t, err)
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for range 2 {
		wg.Go(func() { _, err := a.Setup(t.Context(), p, nil); results <- err })
	}
	wg.Wait()
	close(results)
	for err := range results {
		u.OK(t, err)
	}
	u.Equal(t, m.base.clones, 1)
	u.Equal(t, len(m.tools), 1)
}
func TestManualOwnedVMIsNeverAutomaticallyBootstrapped(t *testing.T) {
	a, m := setupFixture(t)
	v := model.VM{ID: model.ID(), Name: "manual", OS: model.Linux, Image: "custom-v1", State: "stopped"}
	u.OK(t, a.Store.AddVM(t.Context(), v))
	p, err := a.PlanSetup(t.Context(), app.Selection{VM: v.Name}, "codex")
	u.OK(t, err)
	u.Equal(t, p.Managed, false)
	u.Equal(t, p.Needed(), false)
	u.Equal(t, len(m.tools), 0)
}
func TestLoginInstructionsKeepCustomPathsAndExactVM(t *testing.T) {
	e := &app.LoginRequired{Agent: "codex", VM: "agent-macos-12345678"}
	s := e.Instructions([]string{"agent-vm", "--config", "/tmp/custom config.toml", "--data-dir", "/tmp/custom-data"})
	u.Equal(t, strings.Contains(s, "'/tmp/custom config.toml'"), true)
	u.Equal(t, strings.Contains(s, "'--vm' 'agent-macos-12345678'"), true)
	u.Equal(t, strings.Contains(s, "'--new'"), false)
}

func TestSetupMissingTartOffersInstallationInSinglePlan(t *testing.T) {
	a, m := setupFixture(t)
	m.missingTart = true
	p, err := a.PlanSetup(t.Context(), app.Selection{}, "codex")
	u.OK(t, err)
	u.Equal(t, p.InstallTart, true)
	u.Equal(t, strings.Contains(p.Summary(), "checksum-verified Tart"), true)
	u.Equal(t, m.base.clones, 0)
}
func TestSetupMissingHomebrewDoesNotBlockPrivateTartInstallation(t *testing.T) {
	a, m := setupFixture(t)
	m.missingTart = true
	m.missingBrew = true
	p, err := a.PlanSetup(t.Context(), app.Selection{}, "codex")
	u.OK(t, err)
	u.Equal(t, p.InstallTart, true)
	u.Equal(t, m.base.clones, 0)
}
func TestUnsupportedHostFailsBeforeCreatingVM(t *testing.T) {
	a, m := setupFixture(t)
	m.hostVersion = "13.0"
	_, err := a.PlanSetup(t.Context(), app.Selection{}, "codex")
	u.Error(t, err)
	u.Equal(t, strings.Contains(err.Error(), "macOS 14+"), true)
	u.Equal(t, m.base.clones, 0)
}
func TestMacOSDefaultRequiresCompatibleHost(t *testing.T) {
	a, m := setupFixture(t)
	a.Config.MacOS.BaseImage = ""
	m.hostVersion = "26.0"
	_, err := a.PlanSetup(t.Context(), app.Selection{OS: model.MacOS}, "codex")
	u.Error(t, err)
	u.Equal(t, strings.Contains(err.Error(), "macOS 27+"), true)
	u.Equal(t, m.base.clones, 0)
}
func TestInvalidProjectFailsBeforeSetupApproval(t *testing.T) {
	a, m := setupFixture(t)
	approvals := 0
	_, err := a.Run(t.Context(), app.RunOptions{Agent: "codex", Source: filepath.Join(t.TempDir(), "missing"), ApproveSetup: func(app.SetupPlan) error { approvals++; return nil }})
	u.Error(t, err)
	u.Equal(t, approvals, 0)
	u.Equal(t, m.base.clones, 0)
}
func TestInterruptedSSHBootstrapPreservesIdentityAndRetriesSameVM(t *testing.T) {
	a, m := setupFixture(t)
	p, err := a.PlanSetup(t.Context(), app.Selection{}, "codex")
	u.OK(t, err)
	m.failSSH = true
	v, err := a.Setup(t.Context(), p, nil)
	u.Error(t, err)
	key, readErr := os.ReadFile(a.Config.Effective(a.Data).IdentityFile)
	u.OK(t, readErr)
	p, err = a.PlanSetup(t.Context(), app.Selection{VM: v.Name}, "codex")
	u.OK(t, err)
	u.Equal(t, p.SSH, true)
	m.failSSH = false
	_, err = a.Setup(t.Context(), p, nil)
	u.OK(t, err)
	after, readErr := os.ReadFile(a.Config.Effective(a.Data).IdentityFile)
	u.OK(t, readErr)
	u.Equal(t, after, key)
	u.Equal(t, m.base.clones, 1)
}
func TestInterruptedCloneConfigurationIsRecoveredWithoutSecondClone(t *testing.T) {
	a, m := setupFixture(t)
	p, err := a.PlanSetup(t.Context(), app.Selection{}, "codex")
	u.OK(t, err)
	m.base.failSet = true
	v, err := a.Setup(t.Context(), p, nil)
	u.Error(t, err)
	m.base.failSet = false
	p, err = a.PlanSetup(t.Context(), app.Selection{VM: v.Name}, "codex")
	u.OK(t, err)
	_, err = a.Setup(t.Context(), p, nil)
	u.OK(t, err)
	u.Equal(t, m.base.clones, 1)
}
func TestDoctorDoesNotReportUnusedOSImage(t *testing.T) {
	a, _ := setupFixture(t)
	checks := a.DoctorOS(t.Context(), model.Linux)
	names := []string{}
	for _, check := range checks {
		names = append(names, check.Name)
	}
	u.Equal(t, strings.Contains(strings.Join(names, ","), "macos_image"), false)
	u.Equal(t, strings.Contains(strings.Join(names, ","), "linux_image"), true)
}

func TestRetryWithoutConsentReusesInstalledToolsAndStopsAtLogin(t *testing.T) {
	a, m := setupFixture(t)
	m.base.authMissing = true
	source := t.TempDir()
	_, err := a.Run(t.Context(), app.RunOptions{Agent: "codex", Source: source, ApproveSetup: approve})
	u.Error(t, err)
	_, err = a.Run(t.Context(), app.RunOptions{Agent: "codex", Source: source})
	u.Error(t, err)
	u.Equal(t, strings.Contains(err.Error(), "not authenticated"), true)
	u.Equal(t, m.base.clones, 1)
	u.Equal(t, len(m.tools), 1)
}

func TestInterruptedBootPreservesOwnedVMForRetry(t *testing.T) {
	a, m := setupFixture(t)
	p, err := a.PlanSetup(t.Context(), app.Selection{}, "codex")
	u.OK(t, err)
	m.failIP = true
	v, err := a.Setup(t.Context(), p, nil)
	u.Error(t, err)
	m.failIP = false
	p, err = a.PlanSetup(t.Context(), app.Selection{VM: v.Name}, "codex")
	u.OK(t, err)
	u.Equal(t, p.Create, false)
	_, err = a.Setup(t.Context(), p, nil)
	u.OK(t, err)
	u.Equal(t, m.base.clones, 1)
}

func TestPrivateTartChecksumMismatchNeverExtractsOrCreatesVM(t *testing.T) {
	a, m := setupFixture(t)
	m.missingTart = true
	p, err := a.PlanSetup(t.Context(), app.Selection{}, "codex")
	u.OK(t, err)
	_, err = a.Setup(t.Context(), p, nil)
	u.Error(t, err)
	u.Equal(t, strings.Contains(err.Error(), "checksum mismatch"), true)
	u.Equal(t, m.base.clones, 0)
}

func TestRunNewMacOSUsesGoldenGateImageAndOneVM(t *testing.T) {
	a, m := setupFixture(t)
	a.Config.MacOS.BaseImage = ""
	s, err := a.Run(t.Context(), app.RunOptions{Selection: app.Selection{New: true, OS: model.MacOS}, Agent: "codex", Source: t.TempDir(), ApproveSetup: approve})
	u.OK(t, err)
	u.Equal(t, s.OS, model.MacOS)
	u.Equal(t, strings.HasPrefix(s.VM, "agent-macos-"), true)
	u.Equal(t, m.base.clones, 1)
	v, err := a.Store.VM(t.Context(), s.VM)
	u.OK(t, err)
	u.Equal(t, strings.Contains(v.Image, "macos-golden-gate-base@sha256:"), true)
}

func TestLockedGuestKeychainInstructionsPreserveCredentialStore(t *testing.T) {
	e := &app.LoginRequired{Agent: "cursor", VM: "agent-macos", UnlockKeychain: true}
	text := e.Instructions([]string{"agent-vm"})
	u.Equal(t, strings.Contains(text, "security unlock-keychain"), true)
	u.Equal(t, strings.Contains(text, "NO_OPEN_BROWSER=1 agent login"), true)
	u.Equal(t, strings.Contains(text, "'--vm' 'agent-macos'"), true)
}

func TestCursorKeychainRetryDoesNotAskForSetupOrReinstall(t *testing.T) {
	a, m := setupFixture(t)
	a.Config.MacOS.BaseImage = ""
	m.lockedTools = true
	m.base.keychainLocked = true
	m.base.agentVersion = "2026.10.22-27dcf5f"
	_, err := a.Run(t.Context(), app.RunOptions{Selection: app.Selection{OS: model.MacOS}, Agent: "cursor", Source: t.TempDir(), ApproveSetup: approve})
	u.Error(t, err)
	u.Equal(t, strings.Contains(err.Error(), "keychain"), true)
	plan, err := a.PlanSetup(t.Context(), app.Selection{OS: model.MacOS}, "cursor")
	u.OK(t, err)
	u.Equal(t, plan.Needed(), false)
	m.lockedTools = false
	_, err = a.Run(t.Context(), app.RunOptions{Selection: app.Selection{OS: model.MacOS}, Agent: "cursor", Source: t.TempDir()})
	u.Error(t, err)
	u.Equal(t, strings.Contains(err.Error(), "keychain"), true)
	u.Equal(t, len(m.tools), 1)
	m.base.keychainLocked = false
	m.base.authMissing = true
	_, err = a.Run(t.Context(), app.RunOptions{Selection: app.Selection{OS: model.MacOS}, Agent: "cursor", Source: t.TempDir()})
	u.Error(t, err)
	u.Equal(t, strings.Contains(err.Error(), "not authenticated"), true)
	u.Equal(t, len(m.tools), 1)
}

func TestOlderSystemTartOffersPrivateCopyWithoutHostUpgrade(t *testing.T) {
	a, m := setupFixture(t)
	m.tartVersion = "2.40.1"
	p, err := a.PlanSetup(t.Context(), app.Selection{}, "codex")
	u.OK(t, err)
	u.Equal(t, p.InstallTart, true)
	u.Equal(t, m.base.clones, 0)
}
