package app_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"agent-vm/internal/app"
	"agent-vm/internal/config"
	"agent-vm/internal/host"
	"agent-vm/internal/model"
	"agent-vm/internal/tart"
	u "agent-vm/internal/testutil"
)

type machine struct {
	mu                       sync.Mutex
	vms                      map[string]tart.VM
	clones, starts, launches int
	failSet                  bool
	authMissing              bool
	agentVersion             string
	keychainLocked           bool
	process                  string
	history                  bool
	lastArgv                 []string
	lastScript               string
}

func (m *machine) Run(ctx context.Context, c host.Command) ([]byte, error) {
	if c.Name == "git" {
		return (host.Exec{}).Run(ctx, c)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if c.Name == "tart" {
		switch c.Args[0] {
		case "--version":
			return []byte("2.41.1"), nil
		case "list":
			vs := []tart.VM{}
			for _, v := range m.vms {
				vs = append(vs, v)
			}
			return json.Marshal(vs)
		case "clone":
			m.clones++
			m.vms[c.Args[2]] = tart.VM{Name: c.Args[2], State: "stopped"}
			return nil, nil
		case "set":
			if m.failSet {
				return nil, fmt.Errorf("configuration failure")
			}
			return nil, nil
		case "ip":
			return []byte("192.0.2.10"), nil
		case "stop":
			v := m.vms[c.Args[1]]
			v.Running = false
			m.vms[c.Args[1]] = v
			return nil, nil
		case "delete":
			delete(m.vms, c.Args[1])
			return nil, nil
		}
	}
	if c.Name == "ssh" {
		if len(c.Args) == 1 && c.Args[0] == "-V" {
			return []byte("OpenSSH_10.3p1"), nil
		}
		script := c.Args[len(c.Args)-1]
		m.lastScript = script
		script = strings.TrimSuffix(strings.ReplaceAll(script, "'\"'\"'", "'"), "'")
		if strings.Contains(script, "'python3' '-c'") && c.In != nil {
			var r map[string]any
			if err := json.NewDecoder(c.In).Decode(&r); err != nil {
				return nil, err
			}
			switch {
			case strings.HasSuffix(script, "'provision'"):
				return []byte(`{"tmux":"tmux 3.8"}`), nil
			case strings.HasSuffix(script, "'check'"):
				if m.keychainLocked {
					return []byte(`{"keychain_locked":true}`), nil
				}
				version := m.agentVersion
				if version == "" {
					version = "0.164.0"
				}
				return json.Marshal(map[string]string{"tmux": "tmux 3.8", "agent": version})
			case strings.HasSuffix(script, "'auth'"):
				return json.Marshal(map[string]bool{"authenticated": !m.authMissing, "verified": true})
			case strings.HasSuffix(script, "'home'"):
				return []byte(`{"home":"/home/admin"}`), nil
			case strings.HasSuffix(script, "'state'"):
				return json.Marshal(map[string]string{"status": m.process})
			case strings.HasSuffix(script, "'launch'"):
				m.launches++
				m.process = "running"
				m.lastArgv = nil
				for _, arg := range r["argv"].([]any) {
					m.lastArgv = append(m.lastArgv, arg.(string))
				}
				return []byte(`{"status":"running"}`), nil
			case strings.HasSuffix(script, "'discover'"):
				if m.history {
					return []byte(`{"native_id":"native-id","history":"/home/admin/.codex/sessions/saved.jsonl"}`), nil
				}
				return []byte(`{"native_id":"","history":""}`), nil
			case strings.HasSuffix(script, "'history'"):
				if m.history && r["native_id"] != "" {
					return []byte(`{"ok":true}`), nil
				}
				return []byte(`{"error":"native conversation history is missing"}`), nil
			}
		}
		if c.In != nil {
			_, err := io.Copy(io.Discard, c.In)
			return nil, err
		}
		return nil, nil
	}
	return nil, fmt.Errorf("unexpected external command %s", c.Name)
}
func (m *machine) Detach(_ context.Context, c host.Command, _ string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.starts++
	name := c.Args[len(c.Args)-1]
	v := m.vms[name]
	v.Running = true
	m.vms[name] = v
	return nil
}
func fixture(t *testing.T) (*app.App, *machine) {
	t.Helper()
	m := &machine{vms: map[string]tart.VM{}, process: "missing", history: true}
	c := config.Defaults()
	c.Linux.BaseImage = "linux-v1"
	c.MacOS.BaseImage = "macos-v1"
	c.IdentityFile = "/test/key"
	a, err := app.Open(c, t.TempDir(), m)
	u.OK(t, err)
	a.Tart.Host = func() (string, string) { return "darwin", "arm64" }
	t.Cleanup(func() { u.OK(t, a.Store.Close()) })
	return a, m
}
func session(t *testing.T, a *app.App, v model.VM) model.Session {
	t.Helper()
	s := model.Session{ID: model.ID(), Name: "dark-mode", Agent: "codex", VMID: v.ID, VM: v.Name, Created: time.Now().UTC(), LastUsed: time.Now().UTC(), Status: "running"}
	w := model.Workspace{ID: model.ID(), VMID: v.ID, Project: "project", Source: "/host/project", Path: "/home/admin/work", Baseline: "baseline-hash"}
	u.OK(t, a.Store.AddSession(t.Context(), w, s))
	return s
}
func TestSelectionPrecedenceAndFlagConflicts(t *testing.T) {
	c := config.Defaults()
	name, os, err := app.Select(c, app.Selection{})
	u.OK(t, err)
	u.Equal(t, name, "agent-linux")
	u.Equal(t, os, model.Linux)
	name, os, err = app.Select(c, app.Selection{OS: model.MacOS})
	u.OK(t, err)
	u.Equal(t, name, "agent-macos")
	u.Equal(t, os, model.MacOS)
	name, _, err = app.Select(c, app.Selection{VM: "experiment"})
	u.OK(t, err)
	u.Equal(t, name, "experiment")
	_, _, err = app.Select(c, app.Selection{VM: "experiment", New: true})
	u.Error(t, err)
}
func TestEnsureCreatesStartsAndReusesPersistentVM(t *testing.T) {
	a, m := fixture(t)
	first, err := a.Ensure(t.Context(), app.Selection{})
	u.OK(t, err)
	second, err := a.Ensure(t.Context(), app.Selection{})
	u.OK(t, err)
	u.Equal(t, first.ID, second.ID)
	u.Equal(t, m.clones, 1)
	u.Equal(t, m.starts, 1)
	u.OK(t, a.Stop(t.Context(), first.Name))
	u.Equal(t, len(m.vms), 1)
	_, err = a.Start(t.Context(), first.Name)
	u.OK(t, err)
	u.Equal(t, m.starts, 2)
}
func TestConcurrentEnsureClonesAndStartsOnce(t *testing.T) {
	a, m := fixture(t)
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for range 8 {
		wg.Go(func() { _, err := a.Ensure(t.Context(), app.Selection{}); errs <- err })
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		u.OK(t, err)
	}
	u.Equal(t, m.clones, 1)
	u.Equal(t, m.starts, 1)
}
func TestUnownedCollisionCannotBeCreatedStoppedOrDeleted(t *testing.T) {
	a, m := fixture(t)
	m.vms["unrelated"] = tart.VM{Name: "unrelated"}
	_, err := a.Create(t.Context(), "unrelated", model.Linux)
	u.Error(t, err)
	u.Error(t, a.Stop(t.Context(), "unrelated"))
	u.Error(t, a.Delete(t.Context(), "unrelated"))
	u.Equal(t, len(m.vms), 1)
	u.Equal(t, m.clones, 0)
}
func TestContradictoryOSDoesNotStartExistingVM(t *testing.T) {
	a, m := fixture(t)
	_, err := a.Create(t.Context(), "experiment", model.Linux)
	u.OK(t, err)
	_, err = a.Ensure(t.Context(), app.Selection{VM: "experiment", OS: model.MacOS})
	u.Error(t, err)
	u.Equal(t, m.starts, 0)
}
func TestPartialConfigurationFailureRetainsOwnershipAndCanRecover(t *testing.T) {
	a, m := fixture(t)
	m.failSet = true
	v, err := a.Create(t.Context(), "experiment", model.Linux)
	u.Error(t, err)
	got, err := a.Store.VM(t.Context(), v.Name)
	u.OK(t, err)
	u.Equal(t, got.State, "creating")
	m.failSet = false
	_, err = a.Start(t.Context(), v.Name)
	u.OK(t, err)
	u.Equal(t, m.clones, 1)
	u.Equal(t, m.starts, 1)
}
func TestFreshVMDoesNotReplaceDefaultAndMissingVMIsNotRecreated(t *testing.T) {
	a, m := fixture(t)
	fresh, err := a.Ensure(t.Context(), app.Selection{New: true, OS: model.MacOS})
	u.OK(t, err)
	u.Equal(t, strings.HasPrefix(fresh.Name, "agent-macos-"), true)
	u.Equal(t, a.Config.MacOS.DefaultVM, "agent-macos")
	u.OK(t, a.Stop(t.Context(), fresh.Name))
	delete(m.vms, fresh.Name)
	_, err = a.Start(t.Context(), fresh.Name)
	u.Error(t, err)
	u.Equal(t, m.clones, 1)
}
func TestLiveResumeAttachesWithoutDuplicateLaunch(t *testing.T) {
	a, m := fixture(t)
	v, err := a.Ensure(t.Context(), app.Selection{})
	u.OK(t, err)
	s := session(t, a, v)
	m.process = "running"
	got, _, p, err := a.PrepareSession(t.Context(), s.ID, true)
	u.OK(t, err)
	u.Equal(t, got.ID, s.ID)
	u.Equal(t, p.Status, "running")
	u.Equal(t, m.launches, 0)
}
func TestResumeUsesNativeIDAndMissingHistoryNeverStartsFresh(t *testing.T) {
	a, m := fixture(t)
	v, err := a.Ensure(t.Context(), app.Selection{})
	u.OK(t, err)
	s := session(t, a, v)
	_, _, _, err = a.PrepareSession(t.Context(), s.ID, true)
	u.OK(t, err)
	u.Equal(t, m.lastArgv, []string{"codex", "resume", "native-id"})
	u.Equal(t, m.launches, 1)
	m.process = "exited"
	m.history = false
	_, _, _, err = a.PrepareSession(t.Context(), s.ID, true)
	u.Error(t, err)
	u.Equal(t, m.launches, 1)
}
func TestConcurrentResumeLaunchesOneProcess(t *testing.T) {
	a, m := fixture(t)
	v, err := a.Ensure(t.Context(), app.Selection{})
	u.OK(t, err)
	s := session(t, a, v)
	var wg sync.WaitGroup
	errs := make(chan error, 5)
	for range 5 {
		wg.Go(func() { _, _, _, err := a.PrepareSession(t.Context(), s.ID, true); errs <- err })
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		u.OK(t, err)
	}
	u.Equal(t, m.launches, 1)
}
func TestAttachDeadProcessReportsResumeWithoutLaunching(t *testing.T) {
	a, m := fixture(t)
	v, err := a.Ensure(t.Context(), app.Selection{})
	u.OK(t, err)
	s := session(t, a, v)
	_, _, _, err = a.PrepareSession(t.Context(), s.ID, false)
	u.Error(t, err)
	u.Equal(t, m.launches, 0)
}
func TestFreshRunCreatesSeparateWorkspacesAndPreservesPromptArgv(t *testing.T) {
	a, m := fixture(t)
	source := t.TempDir()
	u.Write(t, filepath.Join(source, "hello.txt"), "hello")
	first, err := a.Run(t.Context(), app.RunOptions{Agent: "codex", Source: source, Prompt: "Implement 'dark mode'\n$(false)", Args: []string{"--model", "example"}})
	u.OK(t, err)
	u.Equal(t, m.lastArgv, []string{"codex", "--model", "example", "--", "Implement 'dark mode'\n$(false)"})
	second, err := a.Run(t.Context(), app.RunOptions{Agent: "codex", Source: source})
	u.OK(t, err)
	u.Equal(t, first.VMID, second.VMID)
	u.Equal(t, first.Workspace == second.Workspace, false)
	u.Equal(t, first.Project, second.Project)
	u.Equal(t, m.launches, 2)
}
