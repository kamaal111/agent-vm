package agent_test

import (
	"testing"

	"agent-vm/internal/agent"
	"agent-vm/internal/model"
	u "agent-vm/internal/testutil"
)

func TestAdapterFreshAndResumeCommands(t *testing.T) {
	s := model.Session{ID: "b7eea41f-bd5b-4351-9667-d4d4aa3c1f83", NativeID: "native-id", Args: []string{"--model", "example"}}
	codex, err := agent.Get("codex")
	u.OK(t, err)
	args, err := codex.Launch(s, "a 'quoted'\nprompt $(true)", false)
	u.OK(t, err)
	u.Equal(t, args, []string{"codex", "--model", "example", "--", "a 'quoted'\nprompt $(true)"})
	args, err = codex.Launch(s, "", true)
	u.OK(t, err)
	u.Equal(t, args, []string{"codex", "resume", "native-id", "--model", "example"})
	claude, err := agent.Get("claude")
	u.OK(t, err)
	args, err = claude.Launch(s, "", false)
	u.OK(t, err)
	u.Equal(t, args, []string{"claude", "--session-id", s.ID, "--model", "example"})
	cursor, err := agent.Get("cursor")
	u.OK(t, err)
	args, err = cursor.Launch(s, "", false)
	u.OK(t, err)
	u.Equal(t, args, []string{"agent", "--resume", "native-id", "--model", "example"})
}
func TestAdapterRejectsMissingHistoryAndCredentialArguments(t *testing.T) {
	a, err := agent.Get("codex")
	u.OK(t, err)
	_, err = a.Launch(model.Session{}, "", true)
	u.Error(t, err)
	u.Error(t, agent.ValidateArgs([]string{"--api-key=secret"}))
	u.Error(t, agent.ValidateArgs([]string{"--resume", "old"}))
	u.Error(t, agent.ValidateArgs([]string{"--workspace=/host"}))
	u.Error(t, a.CheckVersion("codex-cli 0.163.0"))
}
