package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"agent-vm/internal/agent"
	"agent-vm/internal/guest"
	"agent-vm/internal/host"
	"agent-vm/internal/model"
	"agent-vm/internal/project"
)

type RunOptions struct {
	Selection                   Selection
	Agent, Name, Prompt, Source string
	Args                        []string
}
type Process struct {
	Status   string `json:"status"`
	ExitCode *int   `json:"exit_code,omitempty"`
}
type Discovery struct {
	NativeID   string `json:"native_id"`
	History    string `json:"history"`
	Limitation string `json:"limitation"`
}

func (a *App) checkAgent(ctx context.Context, c guest.SSH, adapter agent.Adapter) error {
	var v struct {
		Tmux  string `json:"tmux"`
		Agent string `json:"agent"`
	}
	if err := c.Control(ctx, "check", map[string]string{"executable": adapter.Executable}, &v); err != nil {
		return err
	}
	if err := host.CheckVersion(v.Tmux, "3.8", 3); err != nil {
		return fmt.Errorf("guest tmux: %w", err)
	}
	if err := adapter.CheckVersion(v.Agent); err != nil {
		return err
	}
	var auth struct {
		Authenticated bool `json:"authenticated"`
		Verified      bool `json:"verified"`
	}
	if err := c.Control(ctx, "auth", map[string]string{"agent": adapter.Name}, &auth); err != nil {
		return err
	}
	if auth.Verified && !auth.Authenticated {
		return fmt.Errorf("%s is not authenticated; run agent-vm shell %s and complete guest login (or configure its guest credential env file)", adapter.Name, c.VM.Name)
	}
	return nil
}
func (a *App) Run(ctx context.Context, o RunOptions) (model.Session, error) {
	adapter, err := agent.Get(o.Agent)
	if err != nil {
		return model.Session{}, err
	}
	if err = agent.ValidateArgs(o.Args); err != nil {
		return model.Session{}, err
	}
	if o.Name != "" {
		if err = model.ValidName(o.Name); err != nil {
			return model.Session{}, err
		}
	}
	if _, _, err = Select(a.Config, o.Selection); err != nil {
		return model.Session{}, err
	}
	snapshot, err := project.Prepare(ctx, a.Runner, o.Source)
	if err != nil {
		return model.Session{}, err
	}
	defer os.RemoveAll(snapshot.Dir)
	v, err := a.Ensure(ctx, o.Selection)
	if err != nil {
		return model.Session{}, err
	}
	unlock, err := a.lock(ctx)
	if err != nil {
		return model.Session{}, err
	}
	defer unlock()
	c, err := a.Connection(ctx, v, true)
	if err != nil {
		return model.Session{}, err
	}
	if err = a.provisionGuest(ctx, c, v); err != nil {
		return model.Session{}, err
	}
	if err = a.checkAgent(ctx, c, adapter); err != nil {
		return model.Session{}, err
	}
	var home struct {
		Home string `json:"home"`
	}
	if err = c.Control(ctx, "home", nil, &home); err != nil {
		return model.Session{}, err
	}
	s := model.Session{ID: model.ID(), Name: o.Name, Agent: o.Agent, VMID: v.ID, VM: v.Name, OS: v.OS, Project: snapshot.Project, Source: snapshot.Source, Created: time.Now().UTC(), LastUsed: time.Now().UTC(), Status: "preparing", Args: append([]string(nil), o.Args...)}
	s.Workspace = filepath.ToSlash(filepath.Join(home.Home, ".agent-vm", "workspaces", s.Project, s.ID))
	s.Baseline = snapshot.Baseline
	w := model.Workspace{ID: model.ID(), VMID: v.ID, Project: s.Project, Source: s.Source, Path: s.Workspace, Baseline: s.Baseline}
	if o.Agent == "claude" {
		s.NativeID = s.ID
	}
	if err = a.Store.AddSession(ctx, w, s); err != nil {
		return s, err
	}
	archive, err := snapshot.Archive(ctx)
	if err != nil {
		return s, err
	}
	defer os.Remove(archive.Name())
	defer archive.Close()
	dir := host.Quote(s.Workspace)
	_, err = c.Run(ctx, "umask 077; mkdir -p "+host.Quote(filepath.Dir(s.Workspace))+" && mkdir "+dir+" && tar -xf - -C "+dir, archive, nil, nil, false)
	if err != nil {
		return s, fmt.Errorf("workspace transfer interrupted; session %s retained for inspection, no overwrite attempted: %w", s.ID, err)
	}
	if o.Agent == "cursor" {
		var d Discovery
		if err = c.Control(ctx, "cursor-create", map[string]string{"workspace": s.Workspace, "executable": adapter.Executable}, &d); err != nil {
			return s, err
		}
		s.NativeID = d.NativeID
	}
	argv, err := adapter.Launch(s, o.Prompt, false)
	if err != nil {
		return s, err
	}
	var p Process
	if err = c.Control(ctx, "launch", map[string]any{"id": s.ID, "workspace": s.Workspace, "agent": s.Agent, "argv": argv}, &p); err != nil {
		return s, err
	}
	s.Status = p.Status
	err = a.Store.UpdateSession(ctx, s)
	return s, err
}
func (a *App) discover(ctx context.Context, c guest.SSH, s model.Session) (model.Session, error) {
	if s.NativeID != "" && s.History != "" {
		return s, nil
	}
	var d Discovery
	if err := c.Control(ctx, "discover", map[string]string{"workspace": s.Workspace, "agent": s.Agent}, &d); err != nil {
		return s, err
	}
	if d.NativeID != "" {
		if s.NativeID != "" && s.NativeID != d.NativeID {
			return s, fmt.Errorf("discovered conversation differs from recorded native ID; bind exact history explicitly")
		}
		s.NativeID = d.NativeID
		s.History = d.History
	}
	return s, a.Store.UpdateSession(ctx, s)
}

// PrepareSession resolves live state under a process lock, then returns before terminal handoff.
func (a *App) PrepareSession(ctx context.Context, id string, resume bool) (model.Session, guest.SSH, Process, error) {
	s, err := a.Store.Session(ctx, id)
	if err != nil {
		return s, guest.SSH{}, Process{}, err
	}
	unlock, err := host.Lock(ctx, filepath.Join(a.Data, "locks", "session-"+s.ID+".lock"))
	if err != nil {
		return s, guest.SSH{}, Process{}, err
	}
	defer unlock()
	v, err := a.Start(ctx, s.VM)
	if err != nil {
		return s, guest.SSH{}, Process{}, err
	}
	life, err := a.lock(ctx)
	if err != nil {
		return s, guest.SSH{}, Process{}, err
	}
	defer life()
	s, err = a.Store.Session(ctx, s.ID)
	if err != nil {
		return s, guest.SSH{}, Process{}, err
	}
	c, err := a.Connection(ctx, v, true)
	if err != nil {
		return s, c, Process{}, err
	}
	var p Process
	if err = c.Control(ctx, "state", map[string]string{"id": s.ID}, &p); err != nil {
		return s, c, p, err
	}
	if resume && p.Status == "running" {
		s, err = a.discover(ctx, c, s)
		if err != nil {
			return s, c, p, err
		}
		if err = c.Control(ctx, "history", map[string]string{"workspace": s.Workspace, "native_id": s.NativeID, "history": s.History}, nil); err != nil {
			return s, c, p, err
		}
	}
	if p.Status != "running" {
		if !resume {
			if p.ExitCode != nil && *p.ExitCode != 0 {
				return s, c, p, &host.ExitError{Tool: s.Agent, Code: *p.ExitCode}
			}
			return s, c, p, fmt.Errorf("agent process is %s; use agent-vm resume %s to resume native history", p.Status, s.Label())
		}
		s, err = a.discover(ctx, c, s)
		if err != nil {
			return s, c, p, err
		}
		if err = c.Control(ctx, "history", map[string]string{"workspace": s.Workspace, "native_id": s.NativeID, "history": s.History}, nil); err != nil {
			return s, c, p, err
		}
		adapter, err := agent.Get(s.Agent)
		if err != nil {
			return s, c, p, err
		}
		if err = a.checkAgent(ctx, c, adapter); err != nil {
			return s, c, p, err
		}
		argv, err := adapter.Launch(s, "", true)
		if err != nil {
			return s, c, p, err
		}
		if err = c.Control(ctx, "launch", map[string]any{"id": s.ID, "workspace": s.Workspace, "agent": s.Agent, "argv": argv}, &p); err != nil {
			return s, c, p, err
		}
	}
	s.Status = p.Status
	s.LastUsed = time.Now().UTC()
	err = a.Store.UpdateSession(ctx, s)
	return s, c, p, err
}
func (a *App) Observe(ctx context.Context, id string) (model.Session, Process, error) {
	s, err := a.Store.Session(ctx, id)
	if err != nil {
		return s, Process{}, err
	}
	v, err := a.Store.VM(ctx, s.VMID)
	if err != nil {
		return s, Process{}, err
	}
	c, err := a.Connection(ctx, v, false)
	if err != nil {
		return s, Process{}, err
	}
	var p Process
	if err = c.Control(ctx, "state", map[string]string{"id": s.ID}, &p); err != nil {
		return s, p, err
	}
	s.Status = p.Status
	s, err = a.discover(ctx, c, s)
	if err != nil {
		return s, p, err
	}
	err = a.Store.UpdateSession(ctx, s)
	return s, p, err
}
func (a *App) Bind(ctx context.Context, id, native, history string) error {
	if native == "" || strings.HasPrefix(native, "-") || strings.ContainsAny(native, "\n\r\x00") {
		return fmt.Errorf("invalid native ID")
	}
	if !filepath.IsAbs(history) {
		return fmt.Errorf("--history must be an absolute guest history file path")
	}
	s, err := a.Store.Session(ctx, id)
	if err != nil {
		return err
	}
	unlock, err := host.Lock(ctx, filepath.Join(a.Data, "locks", "session-"+s.ID+".lock"))
	if err != nil {
		return err
	}
	defer unlock()
	v, err := a.Start(ctx, s.VM)
	if err != nil {
		return err
	}
	c, err := a.Connection(ctx, v, true)
	if err != nil {
		return err
	}
	if s.NativeID != "" && s.NativeID != native {
		return fmt.Errorf("native ID already recorded; refusing reassignment")
	}
	s.NativeID = native
	s.History = history
	if err = c.Control(ctx, "history", map[string]string{"workspace": s.Workspace, "native_id": native, "history": history}, nil); err != nil {
		return err
	}
	return a.Store.UpdateSession(ctx, s)
}
func (a *App) Export(ctx context.Context, id, output string) error {
	s, err := a.Store.Session(ctx, id)
	if err != nil {
		return err
	}
	v, err := a.Start(ctx, s.VM)
	if err != nil {
		return err
	}
	c, err := a.Connection(ctx, v, true)
	if err != nil {
		return err
	}
	var p Process
	if err = c.Control(ctx, "state", map[string]string{"id": s.ID}, &p); err != nil {
		return err
	}
	if p.Status == "running" {
		return fmt.Errorf("exit the agent before exporting a stable snapshot")
	}
	f, err := os.OpenFile(output, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	script := `set -eu; umask 077; cd ` + host.Quote(s.Workspace) + `; tmp=$(mktemp -d); trap 'rm -rf "$tmp"' EXIT; git diff --binary --no-ext-diff --no-textconv ` + host.Quote(s.Baseline) + ` > "$tmp/changes.patch"; git status --porcelain=v1 -z > "$tmp/status.bin"; printf '%s\n' ` + host.Quote(s.Baseline) + ` > "$tmp/baseline.txt"; tar --exclude=./.git -cf "$tmp/workspace.tar" .; tar -cf - -C "$tmp" changes.patch status.bin baseline.txt workspace.tar`
	_, err = c.Run(ctx, script, nil, f, nil, false)
	closeErr := f.Close()
	if err != nil {
		os.Remove(output)
		return err
	}
	return closeErr
}
func (a *App) ImportCodex(ctx context.Context, name string) error {
	source, ok := a.Config.Auth["codex"]
	if !ok || source.Source != "file" || source.File == "" {
		return fmt.Errorf("configure auth.codex.source=file and its explicit file path first; guest login is also supported")
	}
	info, err := os.Lstat(source.File)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() > 1024*1024 {
		return fmt.Errorf("auth source must be a regular file <=1 MiB with permissions 0600 or stricter")
	}
	b, err := os.ReadFile(source.File)
	if err != nil {
		return err
	}
	if !json.Valid(b) {
		return fmt.Errorf("auth source is not JSON")
	}
	v, err := a.Start(ctx, name)
	if err != nil {
		return err
	}
	c, err := a.Connection(ctx, v, true)
	if err != nil {
		return err
	}
	script := `import json,os,sys
from pathlib import Path
p=Path.home()/'.codex'; p.mkdir(mode=0o700,exist_ok=True)
b=sys.stdin.buffer.read(); json.loads(b)
try:
 fd=os.open(p/'auth.json',os.O_WRONLY|os.O_CREAT|os.O_EXCL,0o600)
except FileExistsError:
 sys.exit(73)
with os.fdopen(fd,'wb') as f: f.write(b)
`
	_, err = c.Run(ctx, host.Join([]string{"python3", "-c", script}), strings.NewReader(string(b)), nil, nil, false)
	var exit *host.ExitError
	if errors.As(err, &exit) && exit.Code == 73 {
		return fmt.Errorf("guest auth.json already exists; preserve refreshed guest credentials; no overwrite or host copy-back performed")
	}
	return err
}
