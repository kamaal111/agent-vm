package bootstrap_test

import (
	"agent-vm/internal/bootstrap"
	"agent-vm/internal/model"
	u "agent-vm/internal/testutil"
	"os/exec"
	"strings"
	"testing"
)

func TestManifestHasImmutableImagesAndPlatformChecksums(t *testing.T) {
	m, err := bootstrap.Pins()
	u.OK(t, err)
	u.Equal(t, strings.Contains(m.Images[model.Linux], "@sha256:"), true)
	u.Equal(t, strings.Contains(m.Images[model.MacOS], "macos-golden-gate-base@sha256:"), true)
	u.Equal(t, len(m.NodeSHA[model.Linux]), 64)
	u.Equal(t, len(m.NodeSHA[model.MacOS]), 64)
	u.Equal(t, len(m.CursorSHA[model.Linux]), 64)
	u.Equal(t, len(m.CursorSHA[model.MacOS]), 64)
	u.Equal(t, len(m.TmuxSHA), 64)
	u.Equal(t, len(m.Tart.SHA256), 64)
	u.Equal(t, strings.HasPrefix(m.Tart.URL, "https://github.com/openai/tart/releases/download/"), true)
}
func TestRenderedBootstrapScriptsAreValidPOSIXShell(t *testing.T) {
	for _, osName := range []model.OS{model.Linux, model.MacOS} {
		for _, name := range []string{"codex", "claude", "cursor"} {
			t.Run(string(osName)+"/"+name, func(t *testing.T) {
				script, err := bootstrap.Tools(osName, name)
				u.OK(t, err)
				c := exec.Command("sh", "-n")
				c.Stdin = strings.NewReader(script)
				u.OK(t, c.Run())
				u.Equal(t, strings.Contains(script, "selected='"+name+"'"), true)
			})
		}
	}
	s, err := bootstrap.SSH("admin", "ssh-ed25519 AAAA", model.Linux)
	u.OK(t, err)
	c := exec.Command("sh", "-n")
	c.Stdin = strings.NewReader(s)
	u.OK(t, c.Run())
}
