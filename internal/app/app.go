package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"agent-vm/internal/config"
	"agent-vm/internal/guest"
	"agent-vm/internal/host"
	"agent-vm/internal/model"
	"agent-vm/internal/store"
	"agent-vm/internal/tart"
)

const ProvisionVersion = "1"

type App struct {
	Config config.Config
	Store  *store.Store
	Runner host.Runner
	Tart   tart.Provider
	Data   string
}

func Open(c config.Config, data string, r host.Runner) (*App, error) {
	if err := os.MkdirAll(data, 0700); err != nil {
		return nil, err
	}
	if err := os.Chmod(data, 0700); err != nil {
		return nil, err
	}
	s, err := store.Open(filepath.Join(data, "registry.sqlite"))
	if err != nil {
		return nil, err
	}
	return &App{Config: c, Store: s, Runner: r, Tart: tart.Provider{Runner: r, Data: data}, Data: data}, nil
}

type Selection struct {
	VM  string
	OS  model.OS
	New bool
}

func Select(c config.Config, o Selection) (string, model.OS, error) {
	if o.VM != "" && o.New {
		return "", "", fmt.Errorf("--vm and --new cannot be combined")
	}
	os := o.OS
	if os == "" {
		os = c.DefaultOS
	}
	if _, err := model.ParseOS(string(os)); err != nil {
		return "", "", err
	}
	name := o.VM
	if name == "" {
		name = c.Guest(os).DefaultVM
	}
	if o.New {
		name = "agent-" + string(os) + "-" + model.ID()[:8]
	}
	if err := model.ValidName(name); err != nil {
		return "", "", err
	}
	return name, os, nil
}
func (a *App) lock(ctx context.Context) (func(), error) {
	return host.Lock(ctx, filepath.Join(a.Data, "locks", "lifecycle.lock"))
}
func (a *App) Ensure(ctx context.Context, o Selection) (model.VM, error) {
	name, os, err := Select(a.Config, o)
	if err != nil {
		return model.VM{}, err
	}
	unlock, err := a.lock(ctx)
	if err != nil {
		return model.VM{}, err
	}
	defer unlock()
	vs, err := a.Store.VMs(ctx)
	if err != nil {
		return model.VM{}, err
	}
	for _, v := range vs {
		if v.Name == name {
			if o.OS != "" && v.OS != o.OS {
				return v, fmt.Errorf("VM %s is %s, incompatible with --os %s", name, v.OS, o.OS)
			}
			return a.startLocked(ctx, v)
		}
	}
	v, err := a.createLocked(ctx, name, os)
	if err != nil {
		return v, err
	}
	return a.startLocked(ctx, v)
}
func (a *App) Create(ctx context.Context, name string, os model.OS) (model.VM, error) {
	if err := model.ValidName(name); err != nil {
		return model.VM{}, err
	}
	if _, err := model.ParseOS(string(os)); err != nil {
		return model.VM{}, err
	}
	unlock, err := a.lock(ctx)
	if err != nil {
		return model.VM{}, err
	}
	defer unlock()
	return a.createLocked(ctx, name, os)
}
func (a *App) createLocked(ctx context.Context, name string, os model.OS) (model.VM, error) {
	v := model.VM{ID: model.ID(), Name: name, OS: os, Image: a.Config.Guest(os).BaseImage, Provision: ProvisionVersion, State: "creating", Created: time.Now().UTC()}
	if err := model.ValidName(name); err != nil {
		return v, err
	}
	if v.Image == "" {
		return v, fmt.Errorf("configure %s.base_image with a prepared local image or immutable OCI digest first", os)
	}
	if err := a.Tart.Check(ctx); err != nil {
		return v, err
	}
	live, err := a.Tart.List(ctx)
	if err != nil {
		return v, err
	}
	if _, ok := live[name]; ok {
		return v, fmt.Errorf("Tart VM %q already exists; refusing automatic adoption", name)
	}
	if err = a.Store.AddVM(ctx, v); err != nil {
		return v, err
	}
	if err = a.Tart.Clone(ctx, v.Image, name, a.Config.Guest(os)); err != nil {
		return v, fmt.Errorf("creation incomplete; ownership intent retained, use start to recover or delete --yes: %w", err)
	}
	v.State = "stopped"
	return v, a.Store.VMState(ctx, v.ID, v.State)
}
func (a *App) Start(ctx context.Context, name string) (model.VM, error) {
	unlock, err := a.lock(ctx)
	if err != nil {
		return model.VM{}, err
	}
	defer unlock()
	v, err := a.Store.VM(ctx, name)
	if err != nil {
		return v, err
	}
	return a.startLocked(ctx, v)
}
func (a *App) startLocked(ctx context.Context, v model.VM) (model.VM, error) {
	if err := a.Tart.Check(ctx); err != nil {
		return v, err
	}
	live, err := a.Tart.List(ctx)
	if err != nil {
		return v, err
	}
	t, ok := live[v.Name]
	if !ok {
		if v.State != "creating" {
			return v, fmt.Errorf("owned VM %s is missing from Tart; history may be lost; refusing replacement", v.Name)
		}
		if err = a.Tart.Clone(ctx, v.Image, v.Name, a.Config.Guest(v.OS)); err != nil {
			return v, err
		}
	} else if v.State == "creating" && !t.Running {
		if err = a.Tart.Configure(ctx, v.Name, a.Config.Guest(v.OS)); err != nil {
			return v, err
		}
	}
	if !t.Running {
		if err = a.Tart.Start(ctx, v.Name); err != nil {
			return v, err
		}
	}
	bootCtx, cancel := context.WithTimeout(ctx, time.Duration(a.Config.BootSeconds)*time.Second)
	defer cancel()
	if _, err = a.Tart.IP(bootCtx, v.Name, a.Config.BootSeconds); err != nil {
		return v, fmt.Errorf("boot/IP readiness failed; VM preserved; inspect %s: %w", filepath.Join(a.Data, "tart-"+v.Name+".log"), err)
	}
	v.State = "running"
	return v, a.Store.VMState(ctx, v.ID, v.State)
}
func (a *App) Stop(ctx context.Context, name string) error {
	unlock, err := a.lock(ctx)
	if err != nil {
		return err
	}
	defer unlock()
	v, err := a.Store.VM(ctx, name)
	if err != nil {
		return err
	}
	if err = a.Tart.Check(ctx); err != nil {
		return err
	}
	live, err := a.Tart.List(ctx)
	if err != nil {
		return err
	}
	if t, ok := live[v.Name]; ok && t.Running {
		if err = a.Tart.Stop(ctx, v.Name); err != nil {
			return err
		}
	}
	return a.Store.VMState(ctx, v.ID, "stopped")
}
func (a *App) Delete(ctx context.Context, name string) error {
	return a.DeleteReviewed(ctx, name, nil)
}
func (a *App) DeleteReviewed(ctx context.Context, name string, review func(model.VM, []model.Session) error) error {
	unlock, err := a.lock(ctx)
	if err != nil {
		return err
	}
	defer unlock()
	v, err := a.Store.VM(ctx, name)
	if err != nil {
		return err
	}
	if err = a.Tart.Check(ctx); err != nil {
		return err
	}
	live, err := a.Tart.List(ctx)
	if err != nil {
		return err
	}
	if t, ok := live[v.Name]; ok {
		if t.Running {
			return fmt.Errorf("stop VM %s before deletion", v.Name)
		}
	}
	if review != nil {
		all, err := a.Store.Sessions(ctx)
		if err != nil {
			return err
		}
		affected := []model.Session{}
		for _, s := range all {
			if s.VMID == v.ID {
				affected = append(affected, s)
			}
		}
		if err = review(v, affected); err != nil {
			return err
		}
	}
	if _, ok := live[v.Name]; ok {
		if err = a.Tart.Delete(ctx, v.Name); err != nil {
			return err
		}
	}
	return a.Store.DeleteVM(ctx, v.ID)
}

func (a *App) Provision(ctx context.Context, name string) error {
	v, err := a.Start(ctx, name)
	if err != nil {
		return err
	}
	unlock, err := a.lock(ctx)
	if err != nil {
		return err
	}
	defer unlock()
	c, err := a.Connection(ctx, v, true)
	if err != nil {
		return err
	}
	return a.provisionGuest(ctx, c, v)
}
func (a *App) provisionGuest(ctx context.Context, c guest.SSH, v model.VM) error {
	var result struct {
		Tmux string `json:"tmux"`
	}
	if err := c.Control(ctx, "provision", map[string]string{"os": string(v.OS), "version": ProvisionVersion, "image": v.Image}, &result); err != nil {
		return err
	}
	if err := host.CheckVersion(result.Tmux, "3.8", 3); err != nil {
		return err
	}
	return a.Store.VMProvision(ctx, v.ID, ProvisionVersion)
}
func (a *App) Adopt(ctx context.Context, name string, os model.OS, image string) (model.VM, error) {
	v := model.VM{ID: model.ID(), Name: name, OS: os, Image: image, Provision: "adopted", State: "stopped", Created: time.Now().UTC()}
	if err := model.ValidName(name); err != nil {
		return v, err
	}
	if _, err := model.ParseOS(string(os)); err != nil {
		return v, err
	}
	unlock, err := a.lock(ctx)
	if err != nil {
		return v, err
	}
	defer unlock()
	if err = a.Tart.Check(ctx); err != nil {
		return v, err
	}
	live, err := a.Tart.List(ctx)
	if err != nil {
		return v, err
	}
	t, ok := live[name]
	if !ok {
		return v, fmt.Errorf("Tart VM %s does not exist", name)
	}
	if t.Running {
		v.State = "running"
	}
	return v, a.Store.AddVM(ctx, v)
}
func (a *App) Connection(ctx context.Context, v model.VM, ready bool) (guest.SSH, error) {
	if err := guest.CheckSSH(ctx, a.Runner); err != nil {
		return guest.SSH{}, err
	}
	ip, err := a.Tart.IP(ctx, v.Name, 0)
	if err != nil {
		return guest.SSH{}, err
	}
	s := guest.SSH{Runner: a.Runner, Data: a.Data, Identity: a.Config.IdentityFile, User: a.Config.Guest(v.OS).User, IP: ip, VM: v}
	if ready {
		c, cancel := context.WithTimeout(ctx, time.Duration(a.Config.BootSeconds)*time.Second)
		defer cancel()
		err = s.Ready(c)
	}
	return s, err
}
func (a *App) List(ctx context.Context) ([]model.VM, error) {
	vs, err := a.Store.VMs(ctx)
	if err != nil {
		return nil, err
	}
	if err = a.Tart.Check(ctx); err != nil {
		return nil, err
	}
	live, err := a.Tart.List(ctx)
	if err != nil {
		return nil, err
	}
	for i, v := range vs {
		t, ok := live[v.Name]
		vs[i].State = "missing"
		if ok {
			vs[i].State = strings.ToLower(t.State)
			if t.Running {
				vs[i].State = "running"
			} else if t.State == "" {
				vs[i].State = "stopped"
			}
		}
	}
	return vs, nil
}
