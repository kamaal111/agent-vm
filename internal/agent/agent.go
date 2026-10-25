package agent

import (
	"fmt"
	"strings"

	"agent-vm/internal/bootstrap"
	"agent-vm/internal/model"
)

type Adapter struct{ Name, Executable, Version string }

func Get(name string) (Adapter, error) {
	pins, err := bootstrap.Pins()
	if err != nil {
		return Adapter{}, err
	}
	executable := name
	switch name {
	case "codex", "claude":
	case "cursor":
		executable = "agent"
	default:
		return Adapter{}, fmt.Errorf("unsupported agent %q: use codex, claude or cursor", name)
	}
	version := pins.Agents[name]
	if version == "" {
		return Adapter{}, fmt.Errorf("missing version contract for %s", name)
	}
	return Adapter{Name: name, Executable: executable, Version: version}, nil
}
func (a Adapter) CheckVersion(s string) error {
	match := false
	for _, field := range strings.Fields(s) {
		if strings.TrimPrefix(field, "v") == a.Version {
			match = true
		}
	}
	if !match {
		return fmt.Errorf("%s version contract requires %s; found an unsupported version (rebuild image or review technology baseline)", a.Name, a.Version)
	}
	return nil
}
func ValidateArgs(args []string) error {
	for _, arg := range args {
		key, _, _ := strings.Cut(arg, "=")
		if len(key) > 2 && !strings.HasPrefix(key, "--") && strings.ContainsAny(key[1:2], "rcCawp") {
			return fmt.Errorf("attached or clustered managed short flags are unsupported; use explicit long harness options")
		}
		switch key {
		case "resume", "fork", "login", "logout", "exec", "-c", "--continue", "--resume", "-r", "--session-id", "--fork-session", "--no-session-persistence", "--ephemeral", "--worktree", "-w", "--cd", "-C", "--workspace", "--api-key", "-a", "--with-api-key", "--with-access-token", "--print", "-p", "--background", "--bg", "--remote", "--remote-auth-token-env":
			return fmt.Errorf("harness argument %q conflicts with managed conversation, workspace, interactive mode, or credential handling", key)
		}
		lower := strings.ToLower(arg)
		if strings.Contains(lower, "api_key") || strings.Contains(lower, "api-key") || strings.Contains(lower, "token=") {
			return fmt.Errorf("credential arguments are prohibited; configure guest authentication")
		}
	}
	return nil
}
func (a Adapter) Launch(s model.Session, prompt string, resume bool) ([]string, error) {
	if err := ValidateArgs(s.Args); err != nil {
		return nil, err
	}
	args := []string{a.Executable}
	if resume {
		if s.NativeID == "" {
			return nil, fmt.Errorf("native conversation ID missing; bind it explicitly before resume")
		}
		switch a.Name {
		case "codex":
			args = append(args, "resume", s.NativeID)
		case "claude", "cursor":
			args = append(args, "--resume", s.NativeID)
		}
	} else {
		switch a.Name {
		case "claude":
			args = append(args, "--session-id", s.ID)
		case "cursor":
			if s.NativeID == "" {
				return nil, fmt.Errorf("Cursor create-chat did not provide a native ID")
			}
			args = append(args, "--resume", s.NativeID)
		}
	}
	args = append(args, s.Args...)
	if prompt != "" {
		args = append(args, "--", prompt)
	}
	return args, nil
}
