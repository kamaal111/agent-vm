package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"agent-vm/internal/config"
	u "agent-vm/internal/testutil"
)

func TestConfigRoundTripAndNoOverwrite(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.toml")
	c := config.Defaults()
	c.Linux.BaseImage = "linux-base-v1"
	u.OK(t, config.Write(p, c))
	got, err := config.Load(p)
	u.OK(t, err)
	u.Equal(t, got, c)
	u.Error(t, config.Write(p, c))
	info, err := os.Stat(p)
	u.OK(t, err)
	u.Equal(t, info.Mode().Perm(), os.FileMode(0600))
}
func TestConfigRejectsUnknownKeysAndFloatingImages(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.toml")
	u.Write(t, p, "default_oss = 'linux'\n")
	_, err := config.Load(p)
	u.Error(t, err)
	u.Error(t, config.ValidateImage("ghcr.io/cirruslabs/ubuntu:latest"))
	u.OK(t, config.ValidateImage("ghcr.io/example/linux@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"))
}
