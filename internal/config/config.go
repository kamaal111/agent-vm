package config

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"agent-vm/internal/bootstrap"
	"agent-vm/internal/model"
	"github.com/pelletier/go-toml/v2"
)

type Guest struct {
	DefaultVM string `toml:"default_vm"`
	BaseImage string `toml:"base_image,omitempty"`
	User      string `toml:"user"`
	CPU       int    `toml:"cpu"`
	Memory    int    `toml:"memory_mib"`
	Disk      int    `toml:"disk_gb"`
}
type Auth struct {
	Source string `toml:"source"`
	File   string `toml:"file,omitempty"`
}
type Config struct {
	DefaultOS    model.OS        `toml:"default_os"`
	DefaultAgent string          `toml:"default_agent"`
	IdentityFile string          `toml:"identity_file,omitempty"`
	BootSeconds  int             `toml:"boot_timeout_seconds"`
	Linux        Guest           `toml:"linux"`
	MacOS        Guest           `toml:"macos"`
	Auth         map[string]Auth `toml:"auth"`
}

func Defaults() Config {
	return Config{DefaultOS: model.Linux, DefaultAgent: "codex", BootSeconds: 180,
		Linux: Guest{DefaultVM: "agent-linux", User: "admin", CPU: 4, Memory: 8192, Disk: 60},
		MacOS: Guest{DefaultVM: "agent-macos", User: "admin", CPU: 4, Memory: 8192, Disk: 80},
		Auth:  map[string]Auth{"codex": {Source: "guest"}, "claude": {Source: "guest"}, "cursor": {Source: "guest"}}}
}
func Paths() (string, string, error) {
	h, err := os.UserHomeDir()
	if err != nil {
		return "", "", err
	}
	return filepath.Join(h, ".config", "agent-vm", "config.toml"), filepath.Join(h, ".local", "share", "agent-vm"), nil
}
func Load(path string) (Config, error) {
	c := Defaults()
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	d := toml.NewDecoder(bytes.NewReader(b)).DisallowUnknownFields()
	if err = d.Decode(&c); err != nil {
		return c, fmt.Errorf("decode config: %w", err)
	}
	return c, c.Validate()
}
func (c Config) Builtin(o model.OS) bool {
	if o == model.MacOS {
		return c.MacOS.BaseImage == ""
	}
	return c.Linux.BaseImage == ""
}
func (c Config) Guest(o model.OS) Guest {
	g := c.Linux
	if o == model.MacOS {
		g = c.MacOS
	}
	if g.BaseImage == "" {
		g.BaseImage = bootstrap.Image(o)
	}
	return g
}
func (c Config) Effective(data string) Config {
	c.Linux = c.Guest(model.Linux)
	c.MacOS = c.Guest(model.MacOS)
	if c.IdentityFile == "" {
		c.IdentityFile = filepath.Join(data, "ssh", "id_ed25519")
	}
	return c
}
func (c Config) Validate() error {
	if c.IdentityFile != "" && !filepath.IsAbs(c.IdentityFile) {
		return fmt.Errorf("identity_file must be an absolute path")
	}
	if _, err := model.ParseOS(string(c.DefaultOS)); err != nil {
		return err
	}
	if c.DefaultAgent != "codex" && c.DefaultAgent != "claude" && c.DefaultAgent != "cursor" {
		return fmt.Errorf("invalid default_agent")
	}
	if c.BootSeconds < 1 || c.BootSeconds > 3600 {
		return fmt.Errorf("boot_timeout_seconds must be 1–3600")
	}
	for _, g := range []Guest{c.Linux, c.MacOS} {
		if err := model.ValidName(g.DefaultVM); err != nil {
			return err
		}
		if err := model.ValidName(g.User); err != nil {
			return fmt.Errorf("SSH user: %w", err)
		}
		if g.CPU < 1 || g.Memory < 1024 || g.Disk < 20 {
			return fmt.Errorf("resources require cpu>=1, memory_mib>=1024 and disk_gb>=20")
		}
		if g.BaseImage != "" {
			if err := ValidateImage(g.BaseImage); err != nil {
				return err
			}
		}
	}
	for agent, a := range c.Auth {
		if a.File != "" && !filepath.IsAbs(a.File) {
			return fmt.Errorf("auth.%s.file must be an absolute path", agent)
		}
		if agent != "codex" && agent != "claude" && agent != "cursor" {
			return fmt.Errorf("unknown auth agent %q", agent)
		}
		if a.Source != "guest" && !(agent == "codex" && a.Source == "file" && a.File != "") {
			return fmt.Errorf("auth.%s: use guest, or codex file with an explicit file path", agent)
		}
	}
	return nil
}
func ValidateImage(s string) error {
	if strings.Contains(s, "/") {
		parts := strings.Split(s, "@sha256:")
		if len(parts) != 2 || len(parts[1]) != 64 || strings.Trim(parts[1], "0123456789abcdef") != "" {
			return fmt.Errorf("OCI image must use an immutable @sha256 digest: %q", s)
		}
		return nil
	}
	return model.ValidName(s)
}
func Write(path string, c Config) error {
	if err := c.Validate(); err != nil {
		return err
	}
	b, err := toml.Marshal(c)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	_, err = f.Write(b)
	return errorsClose(f, err)
}
func errorsClose(f *os.File, err error) error {
	e := f.Close()
	if err != nil {
		return err
	}
	return e
}
