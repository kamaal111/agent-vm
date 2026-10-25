package app

import (
	"context"
	"os"
	"runtime"
	"strings"

	"agent-vm/internal/host"
	"agent-vm/internal/model"
)

type Check struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail"`
}

func (a *App) Doctor(ctx context.Context) []Check { return a.DoctorOS(ctx, a.Config.DefaultOS) }
func (a *App) DoctorOS(ctx context.Context, selected model.OS) []Check {
	r := []Check{{"host", runtime.GOOS == "darwin" && runtime.GOARCH == "arm64", runtime.GOOS + "/" + runtime.GOARCH}}
	tartErr := a.Tart.Check(ctx)
	detail := "supported Tart: " + a.Tart.Executable()
	if tartErr != nil {
		detail = tartErr.Error()
	}
	r = append(r, Check{"tart", tartErr == nil, detail})
	for _, tool := range []struct {
		name, flag, min string
		major           int
	}{{"ssh", "-V", "10.3", 10}, {"git", "--version", "2.0", 2}} {
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
	identity := a.Config.Effective(a.Data).IdentityFile
	_, err := os.Stat(identity)
	detail = "SSH identity: " + identity
	if os.IsNotExist(err) && a.Config.IdentityFile == "" {
		detail = "setup needed: run agent-vm run " + a.Config.DefaultAgent + " to generate a dedicated identity"
	}
	r = append(r, Check{"ssh_identity", err == nil, detail})
	p, err := a.PlanSetup(ctx, Selection{OS: selected}, a.Config.DefaultAgent)
	if err != nil {
		r = append(r, Check{"setup", false, err.Error()})
	} else {
		detail := "configured image: " + a.Config.Guest(selected).BaseImage + "; live guest not checked"
		if p.Needed() {
			detail = "setup needed: run agent-vm run " + a.Config.DefaultAgent + " --os " + string(selected)
		}
		r = append(r, Check{string(selected) + "_image", !p.Needed(), detail})
	}
	return r
}
