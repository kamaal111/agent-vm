package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"agent-vm/internal/cli"
	u "agent-vm/internal/testutil"
)

func TestHelpDoesNotCreateDataOrLoadBrokenConfig(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "broken.toml")
	u.Write(t, cfg, "[[[invalid")
	cmd := cli.New()
	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetArgs([]string{"--config", cfg, "--data-dir", filepath.Join(dir, "data"), "--help"})
	u.OK(t, cmd.Execute())
	u.Equal(t, output.Len() > 0, true)
	_, err := os.Stat(filepath.Join(dir, "data"))
	u.Equal(t, os.IsNotExist(err), true)
}
func TestEmptySessionListIsCleanJSONWithoutTart(t *testing.T) {
	dir := t.TempDir()
	cmd := cli.New()
	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetArgs([]string{"--config", filepath.Join(dir, "absent.toml"), "--data-dir", filepath.Join(dir, "data"), "sessions", "list", "--json"})
	u.OK(t, cmd.Execute())
	var value []any
	u.OK(t, json.Unmarshal(output.Bytes(), &value))
	u.Equal(t, len(value), 0)
	u.Equal(t, output.String(), "[]\n")
}
func TestDeletionRequiresExplicitNoninteractiveConfirmation(t *testing.T) {
	dir := t.TempDir()
	cmd := cli.New()
	cmd.SetArgs([]string{"--config", filepath.Join(dir, "absent.toml"), "--data-dir", filepath.Join(dir, "data"), "delete", "unrelated"})
	u.Error(t, cmd.Execute())
}

func TestVersionDoesNotCreateDataOrLoadBrokenConfig(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "broken.toml")
	u.Write(t, cfg, "[[[invalid")
	cmd := cli.New()
	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetArgs([]string{"--config", cfg, "--data-dir", filepath.Join(dir, "data"), "--version"})
	u.OK(t, cmd.Execute())
	u.Equal(t, output.String(), "agent-vm version "+cli.Version+"\n")
	_, err := os.Stat(filepath.Join(dir, "data"))
	u.Equal(t, os.IsNotExist(err), true)
}

func TestConfigShowExposesUsableDefaultsWithoutCreatingState(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "absent.toml")
	statePath := filepath.Join(dir, "state")
	cmd := cli.New()
	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetArgs([]string{"--config", configPath, "--data-dir", statePath, "config", "show", "--json"})
	u.OK(t, cmd.Execute())
	var resolved struct {
		IdentityFile string
		Linux        struct{ BaseImage string }
		MacOS        struct{ BaseImage string }
	}
	u.OK(t, json.Unmarshal(output.Bytes(), &resolved))
	u.Equal(t, strings.Contains(resolved.Linux.BaseImage, "ubuntu@sha256:"), true)
	u.Equal(t, strings.Contains(resolved.MacOS.BaseImage, "macos-golden-gate-base@sha256:"), true)
	u.Equal(t, resolved.IdentityFile, filepath.Join(statePath, "ssh", "id_ed25519"))
	_, err := os.Stat(configPath)
	u.Equal(t, os.IsNotExist(err), true)
	_, err = os.Stat(statePath)
	u.Equal(t, os.IsNotExist(err), true)
}
