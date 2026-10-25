package tart

import (
	"agent-vm/internal/bootstrap"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"agent-vm/internal/config"
	"agent-vm/internal/host"
)

type Provider struct {
	Runner host.Runner
	Data   string
	// Host allows capability detection to be replaced at the external boundary in tests.
	Host func() (string, string)
}

// UnsupportedVersion distinguishes an incompatible installation from host or
// execution failures, allowing managed setup to offer a private supported copy.
type UnsupportedVersion struct{ Cause error }

func (e *UnsupportedVersion) Error() string {
	return "Tart: " + e.Cause.Error() + "; use a supported Tart release"
}
func (e *UnsupportedVersion) Unwrap() error { return e.Cause }

type VM struct {
	Name    string
	Running bool
	State   string
}

// Executable prefers the checksum-verified private installation, when present.
func (p Provider) Executable() string {
	if p.Data == "" {
		return "tart"
	}
	pins, err := bootstrap.Pins()
	if err == nil {
		path := filepath.Join(p.Data, "tools", "tart-"+pins.Tart.Version, "tart.app", "Contents", "MacOS", "tart")
		if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() {
			return path
		}
	}
	return "tart"
}
func (p Provider) Check(ctx context.Context) error {
	osName, arch := runtime.GOOS, runtime.GOARCH
	if p.Host != nil {
		osName, arch = p.Host()
	}
	if osName != "darwin" || arch != "arm64" {
		return fmt.Errorf("Tart requires an Apple Silicon Mac; current host is %s/%s", osName, arch)
	}
	b, err := p.Runner.Run(ctx, host.Command{Name: p.Executable(), Args: []string{"--version"}})
	if err != nil {
		return fmt.Errorf("Tart not available: install verified Tart 2.41.1; %w", err)
	}
	if err := host.CheckVersion(string(b), "2.41.1", 2); err != nil {
		return &UnsupportedVersion{Cause: err}
	}
	return nil
}
func (p Provider) List(ctx context.Context) (map[string]VM, error) {
	b, err := p.Runner.Run(ctx, host.Command{Name: p.Executable(), Args: []string{"list", "--source", "local", "--format", "json"}})
	if err != nil {
		return nil, err
	}
	var vs []VM
	if err = json.Unmarshal(b, &vs); err != nil {
		return nil, fmt.Errorf("decode Tart list: %w", err)
	}
	m := map[string]VM{}
	for _, v := range vs {
		m[v.Name] = v
	}
	return m, nil
}
func (p Provider) Clone(ctx context.Context, image, name string, g config.Guest, progress io.Writer) error {
	if _, err := p.Runner.Run(ctx, host.Command{Name: p.Executable(), Args: []string{"clone", image, name}, Env: append(os.Environ(), "TART_NO_AUTO_PRUNE=1"), Out: progress, Err: progress}); err != nil {
		return fmt.Errorf("clone %s (check configured image availability): %w", image, err)
	}
	return p.Configure(ctx, name, g)
}
func (p Provider) Configure(ctx context.Context, name string, g config.Guest) error {
	_, err := p.Runner.Run(ctx, host.Command{Name: p.Executable(), Args: []string{"set", name, "--cpu", strconv.Itoa(g.CPU), "--memory", strconv.Itoa(g.Memory), "--disk-size", strconv.Itoa(g.Disk)}})
	return err
}
func (p Provider) Start(ctx context.Context, name string) error {
	return p.Runner.Detach(ctx, host.Command{Name: p.Executable(), Args: []string{"run", "--no-graphics", name}}, filepath.Join(p.Data, "tart-"+name+".log"))
}
func (p Provider) Stop(ctx context.Context, name string) error {
	_, err := p.Runner.Run(ctx, host.Command{Name: p.Executable(), Args: []string{"stop", name}})
	return err
}
func (p Provider) Delete(ctx context.Context, name string) error {
	_, err := p.Runner.Run(ctx, host.Command{Name: p.Executable(), Args: []string{"delete", name}})
	return err
}
func (p Provider) IP(ctx context.Context, name string, seconds int) (string, error) {
	b, err := p.Runner.Run(ctx, host.Command{Name: p.Executable(), Args: []string{"ip", name, "--wait", strconv.Itoa(seconds)}})
	if err != nil {
		return "", err
	}
	ip := strings.TrimSpace(string(b))
	if net.ParseIP(ip) == nil {
		return "", fmt.Errorf("Tart returned an invalid IP address")
	}
	return ip, nil
}

// Exec uses the owned VM's local control socket, not network SSH trust.
func (p Provider) Exec(ctx context.Context, name, script string, out, stderr io.Writer) ([]byte, error) {
	return p.exec(ctx, name, []string{"/bin/sh", "-s"}, script, out, stderr)
}

// ExecRoot elevates only inside the selected guest, through its local channel.
func (p Provider) ExecRoot(ctx context.Context, name, script string, out, stderr io.Writer) ([]byte, error) {
	return p.exec(ctx, name, []string{"sudo", "-n", "/bin/sh", "-s"}, script, out, stderr)
}
func (p Provider) exec(ctx context.Context, name string, command []string, script string, out, stderr io.Writer) ([]byte, error) {
	return p.Runner.Run(ctx, host.Command{Name: p.Executable(), Args: append([]string{"exec", "-i", name}, command...), In: strings.NewReader(script), Out: out, Err: stderr})
}

func (p Provider) AgentReady(ctx context.Context, name string) error {
	for {
		if _, err := p.Exec(ctx, name, "true\n", nil, nil); err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("Tart guest agent is not ready in %s: %w; VM preserved", name, ctx.Err())
		case <-time.After(time.Second):
		}
	}
}
