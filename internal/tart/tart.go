package tart

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"agent-vm/internal/config"
	"agent-vm/internal/host"
)

type Provider struct {
	Runner host.Runner
	Data   string
	// Host allows capability detection to be replaced at the external boundary in tests.
	Host func() (string, string)
}
type VM struct {
	Name    string
	Running bool
	State   string
}

func (p Provider) Check(ctx context.Context) error {
	osName, arch := runtime.GOOS, runtime.GOARCH
	if p.Host != nil {
		osName, arch = p.Host()
	}
	if osName != "darwin" || arch != "arm64" {
		return fmt.Errorf("Tart requires an Apple Silicon Mac; current host is %s/%s", osName, arch)
	}
	b, err := p.Runner.Run(ctx, host.Command{Name: "tart", Args: []string{"--version"}})
	if err != nil {
		return fmt.Errorf("Tart not available: install verified Tart 2.41.1; %w", err)
	}
	return host.CheckVersion(string(b), "2.41.1", 2)
}
func (p Provider) List(ctx context.Context) (map[string]VM, error) {
	b, err := p.Runner.Run(ctx, host.Command{Name: "tart", Args: []string{"list", "--source", "local", "--format", "json"}})
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
func (p Provider) Clone(ctx context.Context, image, name string, g config.Guest) error {
	if _, err := p.Runner.Run(ctx, host.Command{Name: "tart", Args: []string{"clone", image, name}}); err != nil {
		return fmt.Errorf("clone %s (check configured image availability): %w", image, err)
	}
	return p.Configure(ctx, name, g)
}
func (p Provider) Configure(ctx context.Context, name string, g config.Guest) error {
	_, err := p.Runner.Run(ctx, host.Command{Name: "tart", Args: []string{"set", name, "--cpu", strconv.Itoa(g.CPU), "--memory", strconv.Itoa(g.Memory), "--disk-size", strconv.Itoa(g.Disk)}})
	return err
}
func (p Provider) Start(ctx context.Context, name string) error {
	return p.Runner.Detach(ctx, host.Command{Name: "tart", Args: []string{"run", "--no-graphics", name}}, filepath.Join(p.Data, "tart-"+name+".log"))
}
func (p Provider) Stop(ctx context.Context, name string) error {
	_, err := p.Runner.Run(ctx, host.Command{Name: "tart", Args: []string{"stop", name}})
	return err
}
func (p Provider) Delete(ctx context.Context, name string) error {
	_, err := p.Runner.Run(ctx, host.Command{Name: "tart", Args: []string{"delete", name}})
	return err
}
func (p Provider) IP(ctx context.Context, name string, seconds int) (string, error) {
	b, err := p.Runner.Run(ctx, host.Command{Name: "tart", Args: []string{"ip", name, "--wait", strconv.Itoa(seconds)}})
	if err != nil {
		return "", err
	}
	ip := strings.TrimSpace(string(b))
	if net.ParseIP(ip) == nil {
		return "", fmt.Errorf("Tart returned an invalid IP address")
	}
	return ip, nil
}
