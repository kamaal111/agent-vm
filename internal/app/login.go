package app

import (
	"fmt"

	"agent-vm/internal/host"
)

type LoginRequired struct {
	Agent, VM      string
	UnlockKeychain bool
}

func (e *LoginRequired) Error() string {
	if e.UnlockKeychain {
		return fmt.Sprintf("%s requires an unlocked guest login keychain in %s", e.Agent, e.VM)
	}
	return fmt.Sprintf("%s is not authenticated in %s", e.Agent, e.VM)
}
func (e *LoginRequired) Instructions(prefix []string) string {
	login := "codex login"
	switch e.Agent {
	case "claude":
		login = "claude auth login"
	case "cursor":
		login = "NO_OPEN_BROWSER=1 agent login"
	}
	if e.UnlockKeychain {
		login = "security unlock-keychain\n  " + login
	}
	shell := host.Join(append(append([]string{}, prefix...), "shell", e.VM))
	retry := host.Join(append(append([]string{}, prefix...), "run", e.Agent, "--vm", e.VM))
	return fmt.Sprintf("%s.\nRun:\n  %s\nThen inside the guest:\n  %s\nAfter login, rerun your original command with --vm %s instead of --new, for example:\n  %s", e.Error(), shell, login, e.VM, retry)
}
