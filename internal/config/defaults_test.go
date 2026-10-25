package config_test

import (
	"agent-vm/internal/config"
	"agent-vm/internal/model"
	u "agent-vm/internal/testutil"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultConfigOmitsOptionalOverridesAndRetainsDefaultsOnReload(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	u.OK(t, config.Write(path, config.Defaults()))
	contents, err := os.ReadFile(path)
	u.OK(t, err)
	u.Equal(t, strings.Contains(string(contents), "base_image ="), false)
	u.Equal(t, strings.Contains(string(contents), "identity_file ="), false)
	u.Equal(t, strings.Contains(string(contents), "\nfile ="), false)
	loaded, err := config.Load(path)
	u.OK(t, err)
	u.Equal(t, loaded.Effective(dir), config.Defaults().Effective(dir))
}

func TestAbsentConfigHasUsableEffectiveDefaultsWithoutWriting(t *testing.T) {
	dir := t.TempDir()
	c, err := config.Load(filepath.Join(dir, "absent.toml"))
	u.OK(t, err)
	e := c.Effective(dir)
	u.Equal(t, c.Builtin(model.Linux), true)
	u.Equal(t, strings.Contains(e.Linux.BaseImage, "ubuntu@sha256:"), true)
	u.Equal(t, strings.Contains(e.MacOS.BaseImage, "macos-golden-gate-base@sha256:"), true)
	u.Equal(t, e.IdentityFile, filepath.Join(dir, "ssh", "id_ed25519"))
}

func TestEffectiveDefaultsPreserveCustomImagesAndIdentity(t *testing.T) {
	c := config.Defaults()
	c.Linux.BaseImage = "custom-v1"
	c.IdentityFile = "/custom/key"
	e := c.Effective(t.TempDir())
	u.Equal(t, e.Linux.BaseImage, "custom-v1")
	u.Equal(t, e.IdentityFile, "/custom/key")
	u.Equal(t, c.Builtin(model.Linux), false)
}
