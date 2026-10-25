package app

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"strings"

	"agent-vm/internal/host"
)

type Check struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail"`
}

func (a *App) Doctor(ctx context.Context) []Check {
	r := []Check{{"host", runtime.GOOS == "darwin" && runtime.GOARCH == "arm64", runtime.GOOS + "/" + runtime.GOARCH}}
	for _, tool := range []struct {
		name, flag, min string
		major           int
	}{{"tart", "--version", "2.41.1", 2}, {"ssh", "-V", "10.3", 10}, {"git", "--version", "2.0", 2}} {
		b, err := a.Runner.Run(ctx, host.Command{Name: tool.name, Args: []string{tool.flag}, Err: nil})
		// OpenSSH writes its version to stderr.
		if tool.name == "ssh" {
			var out strings.Builder
			_, err = a.Runner.Run(ctx, host.Command{Name: "ssh", Args: []string{"-V"}, Err: &out})
			b = []byte(out.String())
		}
		if err == nil {
			err = host.CheckVersion(string(b), tool.min, tool.major)
		}
		detail := strings.TrimSpace(string(b))
		if err != nil {
			detail = err.Error()
		}
		r = append(r, Check{tool.name, err == nil, detail})
	}
	_, err := os.Stat(a.Config.IdentityFile)
	r = append(r, Check{"ssh_identity", err == nil, "configure identity_file; public key must be authorized in both guest images"})
	for _, o := range []string{"linux", "macos"} {
		image := a.Config.Linux.BaseImage
		if o == "macos" {
			image = a.Config.MacOS.BaseImage
		}
		r = append(r, Check{o + "_image", image != "", fmt.Sprintf("configured image: %s; live provisioning not checked by doctor", image)})
	}
	return r
}
