package tart_test

import (
	"agent-vm/internal/config"
	"agent-vm/internal/host"
	"agent-vm/internal/tart"
	u "agent-vm/internal/testutil"
	"context"
	"fmt"
	"io"
	"strings"
	"testing"
)

func TestExecSendsScriptThroughLocalControlChannelStdin(t *testing.T) {
	var command host.Command
	r := u.Runner{Do: func(_ context.Context, c host.Command) ([]byte, error) { command = c; return []byte("result"), nil }}
	p := tart.Provider{Runner: r}
	b, err := p.Exec(t.Context(), "agent-linux", "echo 'hello'\n", nil, nil)
	u.OK(t, err)
	u.Equal(t, string(b), "result")
	u.Equal(t, command.Args, []string{"exec", "-i", "agent-linux", "/bin/sh", "-s"})
	input, err := io.ReadAll(command.In)
	u.OK(t, err)
	u.Equal(t, string(input), "echo 'hello'\n")
}
func TestMissingGuestAgentHonorsCancellation(t *testing.T) {
	r := u.Runner{Do: func(context.Context, host.Command) ([]byte, error) { return nil, fmt.Errorf("unavailable") }}
	p := tart.Provider{Runner: r}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	u.Error(t, p.AgentReady(ctx, "agent-linux"))
}

func TestRootBootstrapElevatesInsideGuestWithoutShellInterpolation(t *testing.T) {
	var command host.Command
	r := u.Runner{Do: func(_ context.Context, c host.Command) ([]byte, error) { command = c; return nil, nil }}
	p := tart.Provider{Runner: r}
	script := "printf '%s' 'AGENT_VM_BOOTSTRAP'\n"
	_, err := p.ExecRoot(t.Context(), "agent-linux", script, nil, nil)
	u.OK(t, err)
	u.Equal(t, command.Name, "tart")
	u.Equal(t, command.Args, []string{"exec", "-i", "agent-linux", "sudo", "-n", "/bin/sh", "-s"})
	b, err := io.ReadAll(command.In)
	u.OK(t, err)
	u.Equal(t, string(b), script)
}

func TestClonePreservesUnrelatedCachedImages(t *testing.T) {
	var commands []host.Command
	r := u.Runner{Do: func(_ context.Context, c host.Command) ([]byte, error) {
		commands = append(commands, c)
		return nil, nil
	}}
	p := tart.Provider{Runner: r}
	err := p.Clone(t.Context(), "prepared-v1", "agent-linux", config.Guest{CPU: 4, Memory: 8192, Disk: 60}, nil)
	u.OK(t, err)
	u.Equal(t, strings.Contains(strings.Join(commands[0].Env, "\n"), "TART_NO_AUTO_PRUNE=1"), true)
	u.Equal(t, commands[0].Args, []string{"clone", "prepared-v1", "agent-linux"})
}
