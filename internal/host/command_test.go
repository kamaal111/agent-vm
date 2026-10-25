package host_test

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"agent-vm/internal/host"
	u "agent-vm/internal/testutil"
)

func TestQuotingPreservesArgumentsWithoutEvaluation(t *testing.T) {
	args := []string{"", "a b", "a'b", "$(touch /tmp/agent-vm-should-not-exist)", "line1\nline2", "; exit 9", "--model=x"}
	b, err := (host.Exec{}).Run(t.Context(), host.Command{Name: "sh", Args: []string{"-c", "printf '%s\\0' " + host.Join(args)}})
	u.OK(t, err)
	u.Equal(t, strings.Split(string(b), "\x00"), append(args, ""))
}
func TestCommandErrorsWithholdArgumentsAndOutput(t *testing.T) {
	_, err := (host.Exec{}).Run(t.Context(), host.Command{Name: "sh", Args: []string{"-c", "printf secret-token; exit 23"}})
	u.Error(t, err)
	u.Equal(t, host.ExitCode(err), 23)
	u.Equal(t, strings.Contains(err.Error(), "secret-token"), false)
}
func TestLockCancellationAndRelease(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lock")
	unlock, err := host.Lock(t.Context(), path)
	u.OK(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = host.Lock(ctx, path)
	u.Equal(t, errors.Is(err, context.Canceled), true)
	unlock()
	next, err := host.Lock(t.Context(), path)
	u.OK(t, err)
	next()
}
func TestCanceledExternalCommandStopsPromptly(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	_, err := (host.Exec{}).Run(ctx, host.Command{Name: "sleep", Args: []string{"20"}})
	u.Equal(t, errors.Is(err, context.DeadlineExceeded), true)
}
func TestVersionCompatibility(t *testing.T) {
	u.OK(t, host.CheckVersion("tart 2.41.1", "2.41.1", 2))
	u.OK(t, host.CheckVersion("OpenSSH_10.3p1", "10.3", 10))
	u.Error(t, host.CheckVersion("tart 2.40.0", "2.41.1", 2))
	u.Error(t, host.CheckVersion("tart 3.0.0", "2.41.1", 2))
	u.Error(t, host.CheckVersion("unknown", "2.41.1", 2))
}
