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
