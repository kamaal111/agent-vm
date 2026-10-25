package guest

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"agent-vm/internal/host"
	"agent-vm/internal/model"
	u "agent-vm/internal/testutil"
)

func helper(t *testing.T, home, op string, r any) []byte {
	t.Helper()
	b, err := json.Marshal(r)
	u.OK(t, err)
	c := exec.CommandContext(t.Context(), "python3", "-c", control, op)
	c.Env = append(os.Environ(), "HOME="+home, "TMUX_TMPDIR="+home)
	c.Stdin = bytes.NewReader(b)
	out, err := c.CombinedOutput()
	if err != nil {
		t.Fatalf("helper failed: %v: %s", err, out)
	}
	return out
}
func TestStructuredHistoryDiscoveryMatchesWorkspaceAndRejectsAmbiguity(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, ".codex", "sessions", "2026", "one.jsonl")
	u.Write(t, path, `{"type":"session_meta","payload":{"id":"native-one","cwd":"/work/project","source":"cli"}}`+"\n")
	var got map[string]string
	u.OK(t, json.Unmarshal(helper(t, home, "discover", map[string]string{"agent": "codex", "workspace": "/work/project"}), &got))
	u.Equal(t, got["native_id"], "native-one")
	u.Write(t, filepath.Join(home, ".codex", "sessions", "2026", "two.jsonl"), `{"type":"session_meta","payload":{"id":"native-two","cwd":"/work/project","source":"cli"}}`+"\n")
	u.OK(t, json.Unmarshal(helper(t, home, "discover", map[string]string{"agent": "codex", "workspace": "/work/project"}), &got))
	u.Equal(t, got["native_id"], "")
}
func TestGuestHistoryCheckDoesNotCreateMissingWorkspace(t *testing.T) {
	home := t.TempDir()
	b := helper(t, home, "history", map[string]string{"workspace": filepath.Join(home, "missing"), "native_id": "id", "history": "/missing"})
	var got map[string]any
	u.OK(t, json.Unmarshal(b, &got))
	u.Equal(t, strings.Contains(got["error"].(string), "workspace is missing"), true)
	_, err := os.Stat(filepath.Join(home, "missing"))
	u.Equal(t, os.IsNotExist(err), true)
}
func TestSSHUsesStrictPerVMHostKeysAndSafeRemoteCommand(t *testing.T) {
	var captured host.Command
	r := u.Runner{Do: func(_ context.Context, c host.Command) ([]byte, error) { captured = c; return nil, nil }}
	s := SSH{Runner: r, Data: "/state", Identity: "/key with spaces", User: "admin", IP: "192.0.2.2", VM: model.VM{ID: "vm-id"}}
	_, err := s.Run(t.Context(), "printf '%s' 'hello'", nil, nil, nil, false)
	u.OK(t, err)
	u.Equal(t, strings.Contains(strings.Join(captured.Args, "\n"), "StrictHostKeyChecking=yes"), true)
	u.Equal(t, strings.Contains(strings.Join(captured.Args, "\n"), "HostKeyAlias=agent-vm-vm-id"), true)
	u.Equal(t, captured.Args[len(captured.Args)-2], "admin@192.0.2.2")
}
func TestTrustRejectsMismatchedFingerprintsWithoutWritingKnownHosts(t *testing.T) {
	dir := t.TempDir()
	r := u.Runner{Do: func(_ context.Context, c host.Command) ([]byte, error) {
		if c.Name == "ssh-keyscan" {
			return []byte("192.0.2.2 ssh-ed25519 AAAA\n"), nil
		}
		return []byte("256 SHA256:actual host (ED25519)\n"), nil
	}}
	s := SSH{Runner: r, Data: dir, VM: model.VM{ID: "vm"}}
	u.Error(t, s.Trust(t.Context(), "SHA256:expected"))
	_, err := os.Stat(filepath.Join(dir, "known_hosts"))
	u.Equal(t, os.IsNotExist(err), true)
}
func TestTmuxPreservesPromptAndExitStatus(t *testing.T) {
	if os.Getenv("AGENT_VM_TMUX_TEST") != "1" {
		t.Skip("opt-in isolated local tmux integration")
	}
	home, err := os.MkdirTemp("/tmp", "avm-tmux-")
	u.OK(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(home) })
	socket := filepath.Join(home, fmt.Sprintf("tmux-%d", os.Getuid()), "agent-vm")
	t.Cleanup(func() { _ = exec.Command("tmux", "-S", socket, "kill-server").Run() })
	workspace := filepath.Join(home, "workspace")
	u.OK(t, os.Mkdir(workspace, 0700))
	output := filepath.Join(home, "argv.json")
	script := `import json,sys;json.dump(sys.argv[2:],open(sys.argv[1],'w'));sys.exit(42)`
	sid := model.ID()
	args := []string{"a b", "a'b", "line1\nline2", "$(touch " + filepath.Join(home, "unsafe") + ")", ""}
	argv := append([]string{"python3", "-c", script, output}, args...)
	b := helper(t, home, "launch", map[string]any{"id": sid, "agent": "codex", "workspace": workspace, "argv": argv})
	var response map[string]any
	u.OK(t, json.Unmarshal(b, &response))
	u.Equal(t, response["error"], nil)
	exit := filepath.Join(home, ".agent-vm", "processes", sid, "exit")
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	u.OK(t, waitFile(ctx, exit))
	var got []string
	data, err := os.ReadFile(output)
	u.OK(t, err)
	u.OK(t, json.Unmarshal(data, &got))
	u.Equal(t, got, args)
	data, err = os.ReadFile(exit)
	u.OK(t, err)
	u.Equal(t, strings.TrimSpace(string(data)), "42")
	_, err = os.Stat(filepath.Join(home, "unsafe"))
	u.Equal(t, os.IsNotExist(err), true)
}
func waitFile(ctx context.Context, path string) error {
	for {
		if _, err := os.Stat(path); err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func TestGuestChannelEnrollmentIsIdempotentAndRefusesReplacement(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first")
	second := filepath.Join(dir, "second")
	_, err := (host.Exec{}).Run(t.Context(), host.Command{Name: "ssh-keygen", Args: []string{"-q", "-t", "ed25519", "-N", "", "-f", first}})
	u.OK(t, err)
	_, err = (host.Exec{}).Run(t.Context(), host.Command{Name: "ssh-keygen", Args: []string{"-q", "-t", "ed25519", "-N", "", "-f", second}})
	u.OK(t, err)
	one, err := os.ReadFile(first + ".pub")
	u.OK(t, err)
	two, err := os.ReadFile(second + ".pub")
	u.OK(t, err)
	s := SSH{Data: dir, VM: model.VM{ID: "test-vm"}}
	u.OK(t, s.EnrollPublicKey(t.Context(), string(one), true))
	before, err := os.ReadFile(filepath.Join(dir, "known_hosts"))
	u.OK(t, err)
	u.OK(t, s.EnrollPublicKey(t.Context(), string(one), true))
	u.Error(t, s.EnrollPublicKey(t.Context(), string(two), true))
	after, err := os.ReadFile(filepath.Join(dir, "known_hosts"))
	u.OK(t, err)
	u.Equal(t, after, before)
	u.Equal(t, strings.Count(string(after), "agent-vm-test-vm"), 1)
}
func TestGuestChannelRejectsMalformedPublicKeysWithoutEnrollment(t *testing.T) {
	dir := t.TempDir()
	s := SSH{Data: dir, VM: model.VM{ID: "test-vm"}}
	u.Error(t, s.EnrollPublicKey(t.Context(), "ssh-ed25519 AAAA", true))
	_, err := os.Stat(filepath.Join(dir, "known_hosts"))
	u.Equal(t, os.IsNotExist(err), true)
}

func TestCursorAuthUsesExplicitBooleanInsteadOfSuccessfulExit(t *testing.T) {
	home := t.TempDir()
	bin := filepath.Join(home, "bin")
	u.OK(t, os.Mkdir(bin, 0700))
	path := filepath.Join(bin, "agent")
	u.OK(t, os.WriteFile(path, []byte("#!/bin/sh\nprintf '%s\\n' '{\"status\":\"unauthenticated\",\"isAuthenticated\":false}'\n"), 0700))
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))
	var got map[string]bool
	u.OK(t, json.Unmarshal(helper(t, home, "auth", map[string]string{"agent": "cursor"}), &got))
	u.Equal(t, got["verified"], true)
	u.Equal(t, got["authenticated"], false)
}
func TestCursorAuthRejectsUnsupportedStatusWithoutCredentialOutput(t *testing.T) {
	home := t.TempDir()
	bin := filepath.Join(home, "bin")
	u.OK(t, os.Mkdir(bin, 0700))
	u.OK(t, os.WriteFile(filepath.Join(bin, "agent"), []byte("#!/bin/sh\nprintf '%s\\n' '{\"accessToken\":\"synthetic-private-token\"}'\n"), 0700))
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))
	b := helper(t, home, "auth", map[string]string{"agent": "cursor"})
	var got map[string]string
	u.OK(t, json.Unmarshal(b, &got))
	u.Equal(t, strings.Contains(got["error"], "unsupported schema"), true)
	u.Equal(t, strings.Contains(string(b), "synthetic-private-token"), false)
}
