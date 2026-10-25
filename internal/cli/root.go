package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"agent-vm/internal/app"
	"agent-vm/internal/config"
	"agent-vm/internal/host"
	"agent-vm/internal/model"
	"agent-vm/internal/ui"
	"github.com/spf13/cobra"
)

type options struct {
	config, data string
	json         bool
}

func (o *options) with(cmd *cobra.Command, fn func(*app.App) error) error {
	c, err := config.Load(o.config)
	if err != nil {
		return err
	}
	a, err := app.Open(c, o.data, host.Exec{})
	if err != nil {
		return err
	}
	defer a.Store.Close()
	return fn(a)
}
func (o *options) print(cmd *cobra.Command, v any) error {
	if o.json {
		return json.NewEncoder(cmd.OutOrStdout()).Encode(v)
	}
	switch x := v.(type) {
	case model.VM:
		fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\n", x.Name, x.OS, x.State)
	case []model.VM:
		for _, v := range x {
			fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\n", v.Name, v.OS, v.State)
		}
	case model.Session:
		fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\t%s\t%s\n", x.ID, x.Label(), x.Agent, x.VM, x.Status)
	case []model.Session:
		for _, s := range x {
			fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\t%s/%s\t%s\t%s\t%s\n", s.ID, s.Label(), s.Agent, s.VM, s.OS, s.Project, s.Status, s.LastUsed.Local().Format("2006-01-02 15:04"))
		}
	default:
		b, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			return err
		}
		fmt.Fprintln(cmd.OutOrStdout(), string(b))
	}
	return nil
}

// Version is overridden by the release build using -ldflags.
var Version = "0.1.0-dev"

func New() *cobra.Command {
	cp, data, err := config.Paths()
	if err != nil {
		cp = "config.toml"
		data = ".agent-vm"
	}
	o := &options{config: cp, data: data}
	root := &cobra.Command{Use: "agent-vm", Short: "Persistent development VMs and native coding-agent sessions", Version: Version, SilenceUsage: true, SilenceErrors: true, Args: cobra.NoArgs}
	root.PersistentFlags().StringVar(&o.config, "config", cp, "TOML configuration file")
	root.PersistentFlags().StringVar(&o.data, "data-dir", data, "Private local registry and SSH state directory")
	root.PersistentFlags().BoolVar(&o.json, "json", false, "Machine-readable output (list/status/show/doctor and detached run)")
	root.RunE = func(cmd *cobra.Command, _ []string) error {
		return o.with(cmd, func(a *app.App) error { return browse(cmd.Context(), a, o.json) })
	}
	root.AddCommand(configCommand(o), doctorCommand(o), runCommand(o), sessionCommands(o), handoffCommand(o, "attach", false), handoffCommand(o, "resume", true), exportCommand(o), authCommand(o))
	addVMCommands(root, o)
	return root
}
func configCommand(o *options) *cobra.Command {
	c := &cobra.Command{Use: "config", Short: "Inspect configuration or create it explicitly"}
	c.AddCommand(&cobra.Command{Use: "show", Short: "Show resolved configuration (contains no credentials)", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		v, err := config.Load(o.config)
		if err != nil {
			return err
		}
		return o.print(cmd, v.Effective(o.data))
	}})
	var defaults bool
	init := &cobra.Command{Use: "init", Short: "Create configuration using setup prompts", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		v := config.Defaults()
		var err error
		if !defaults {
			v, err = ui.Setup(cmd.Context(), v)
			if err != nil {
				return err
			}
		}
		return config.Write(o.config, v)
	}}
	init.Flags().BoolVar(&defaults, "defaults", false, "Write defaults without prompting; run guides image and SSH setup")
	c.AddCommand(init)
	return c
}
func doctorCommand(o *options) *cobra.Command {
	var osName string
	c := &cobra.Command{Use: "doctor", Short: "Check prerequisites and setup readiness without installing or downloading", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		return o.with(cmd, func(a *app.App) error {
			selected := a.Config.DefaultOS
			if osName != "" {
				var err error
				selected, err = model.ParseOS(osName)
				if err != nil {
					return err
				}
			}
			checks := a.DoctorOS(cmd.Context(), selected)
			if err := o.print(cmd, checks); err != nil {
				return err
			}
			for _, check := range checks {
				if !check.OK {
					return fmt.Errorf("doctor found missing or unsupported prerequisites")
				}
			}
			return nil
		})
	}}
	c.Flags().StringVar(&osName, "os", "", "Check linux or macos (defaults to configured OS)")
	return c
}

func browse(ctx context.Context, a *app.App, jsonMode bool) error {
	if jsonMode {
		return fmt.Errorf("picker does not support --json; use sessions list --json or an explicit session ID")
	}
	ss, err := a.Store.Sessions(ctx)
	if err != nil {
		return err
	}
	id, err := ui.Pick(ctx, ss)
	if err != nil || id == "" {
		return err
	}
	return handoff(ctx, a, id, true)
}
func handoff(ctx context.Context, a *app.App, id string, resume bool) error {
	if err := ui.Require(); err != nil {
		return err
	}
	var s model.Session
	var attach func() error
	err := ui.Progress(ctx, "Preparing session", true, func(c context.Context) error {
		v, connection, p, err := a.PrepareSession(c, id, resume)
		s = v
		if err != nil {
			return err
		}
		if p.Status != "running" {
			return fmt.Errorf("agent process is %s", p.Status)
		}
		attach = func() error { return connection.Attach(ctx, s.Tmux()) }
		return nil
	})
	if err != nil {
		return err
	}
	connectionErr := attach()
	observeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3e9)
	defer cancel()
	_, p, err := a.Observe(observeCtx, s.ID)
	if err == nil && p.ExitCode != nil {
		if *p.ExitCode != 0 {
			return &host.ExitError{Tool: s.Agent, Code: *p.ExitCode}
		}
		if ctx.Err() == nil && (connectionErr == nil || host.ExitCode(connectionErr) != 255) {
			return nil
		}
	}
	if connectionErr != nil {
		return connectionErr
	}
	return nil
}
func handoffCommand(o *options, name string, resume bool) *cobra.Command {
	c := &cobra.Command{Use: name + " [session]", Short: "Attach to a live process"}
	if resume {
		c.Short = "Attach if live, otherwise resume the original native conversation"
		c.Args = cobra.MaximumNArgs(1)
	} else {
		c.Args = cobra.ExactArgs(1)
	}
	c.RunE = func(cmd *cobra.Command, args []string) error {
		return o.with(cmd, func(a *app.App) error {
			if len(args) == 0 {
				return browse(cmd.Context(), a, o.json)
			}
			if o.json {
				return fmt.Errorf("interactive handoff does not support --json")
			}
			return handoff(cmd.Context(), a, args[0], resume)
		})
	}
	return c
}
func runCommand(o *options) *cobra.Command {
	var vm, osName, name, prompt, source string
	var fresh, detach, yes bool
	c := &cobra.Command{Use: "run [codex|claude|cursor] [flags] [-- harness-args...]", Short: "Start a fresh conversation in an isolated guest project", Args: cobra.ArbitraryArgs}
	c.Flags().StringVar(&vm, "vm", "", "Owned VM name")
	c.Flags().StringVar(&osName, "os", "", "linux or macos")
	c.Flags().BoolVar(&fresh, "new", false, "Create a uniquely named persistent VM")
	c.Flags().StringVar(&name, "name", "", "Unique session name")
	c.Flags().StringVar(&prompt, "prompt", "", "Initial prompt (never shell-evaluated)")
	c.Flags().StringVar(&source, "project", ".", "Source project directory")
	c.Flags().BoolVar(&detach, "detach", false, "Launch without handing off the terminal")
	c.Flags().BoolVar(&yes, "yes", false, "Approve required image downloads, guest setup, and missing Tart installation")
	c.RunE = func(cmd *cobra.Command, args []string) error {
		dash := cmd.ArgsLenAtDash()
		if len(args) > 1 && dash != 1 && dash != 0 {
			return fmt.Errorf("put harness arguments after --")
		}
		if !detach {
			if o.json {
				return fmt.Errorf("--json run requires --detach")
			}
			if err := ui.Require(); err != nil {
				return err
			}
		}
		return o.with(cmd, func(a *app.App) error {
			selected := a.Config.DefaultAgent
			extra := []string(nil)
			if dash == 0 {
				extra = args
			} else if len(args) > 0 {
				selected = args[0]
				extra = args[1:]
			}
			approve := func(p app.SetupPlan) error {
				fmt.Fprintln(cmd.ErrOrStderr(), p.Summary())
				if yes {
					return nil
				}
				if o.json || !ui.Interactive() {
					return fmt.Errorf("setup needed; rerun with --yes to approve these actions")
				}
				return ui.ConfirmSetup(cmd.Context())
			}
			s, err := a.Run(cmd.Context(), app.RunOptions{Selection: app.Selection{VM: vm, OS: model.OS(osName), New: fresh}, Agent: selected, Name: name, Prompt: prompt, Source: source, Args: extra, ApproveSetup: approve, Progress: cmd.ErrOrStderr()})
			if err != nil {
				var login *app.LoginRequired
				if errors.As(err, &login) {
					prefix := []string{"agent-vm"}
					if cmd.Flags().Changed("config") {
						prefix = append(prefix, "--config", o.config)
					}
					if cmd.Flags().Changed("data-dir") {
						prefix = append(prefix, "--data-dir", o.data)
					}
					return errors.New(login.Instructions(prefix))
				}
				return err
			}
			if detach {
				return o.print(cmd, s)
			}
			if s.Status == "exited" {
				_, p, err := a.Observe(cmd.Context(), s.ID)
				if err != nil {
					return err
				}
				if p.ExitCode != nil && *p.ExitCode != 0 {
					return &host.ExitError{Tool: s.Agent, Code: *p.ExitCode}
				}
				return nil
			}
			return handoff(cmd.Context(), a, s.ID, false)
		})
	}
	return c
}
func sessionCommands(o *options) *cobra.Command {
	c := &cobra.Command{Use: "sessions", Short: "Inspect and name conversation metadata"}
	c.AddCommand(&cobra.Command{Use: "list", Short: "List recorded sessions (status is last observed)", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		return o.with(cmd, func(a *app.App) error {
			ss, err := a.Store.Sessions(cmd.Context())
			if err != nil {
				return err
			}
			return o.print(cmd, ss)
		})
	}})
	var live bool
	show := &cobra.Command{Use: "show session", Short: "Show metadata; optionally reconcile running guest state", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		return o.with(cmd, func(a *app.App) error {
			if live {
				s, p, err := a.Observe(cmd.Context(), args[0])
				if err != nil {
					return err
				}
				return o.print(cmd, struct {
					Session model.Session `json:"session"`
					Process app.Process   `json:"process"`
				}{s, p})
			}
			s, err := a.Store.Session(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return o.print(cmd, s)
		})
	}}
	show.Flags().BoolVar(&live, "live", false, "Check tmux and discover history without booting the VM")
	c.AddCommand(show)
	c.AddCommand(&cobra.Command{Use: "rename session new-name", Short: "Rename without changing the stable ID", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		return o.with(cmd, func(a *app.App) error { return a.Store.Rename(cmd.Context(), args[0], args[1]) })
	}})
	var native, history string
	bind := &cobra.Command{Use: "bind session", Short: "Explicitly record a native conversation ID and existing guest history file", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		return o.with(cmd, func(a *app.App) error { return a.Bind(cmd.Context(), args[0], native, history) })
	}}
	bind.Flags().StringVar(&native, "native-id", "", "Exact native conversation ID")
	bind.Flags().StringVar(&history, "history", "", "Absolute guest path to saved native history")
	c.AddCommand(bind)
	return c
}
func exportCommand(o *options) *cobra.Command {
	var output string
	c := &cobra.Command{Use: "export session", Short: "Export changes and workspace archive for review; never applies changes", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if output == "" {
			return fmt.Errorf("--output is required (new .tar file)")
		}
		return o.with(cmd, func(a *app.App) error {
			return ui.Progress(cmd.Context(), "Exporting guest changes", !o.json, func(ctx context.Context) error { return a.Export(ctx, args[0], output) })
		})
	}}
	c.Flags().StringVar(&output, "output", "", "New local export archive path")
	return c
}
func authCommand(o *options) *cobra.Command {
	c := &cobra.Command{Use: "auth", Short: "Explicit authentication setup; credentials stay out of metadata"}
	var vm string
	importCmd := &cobra.Command{Use: "import-codex", Short: "Import only the explicitly configured Codex auth file; never overwrite", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		if vm == "" {
			return fmt.Errorf("--vm is required")
		}
		return o.with(cmd, func(a *app.App) error { return a.ImportCodex(cmd.Context(), vm) })
	}}
	importCmd.Flags().StringVar(&vm, "vm", "", "Owned target VM")
	c.AddCommand(importCmd)
	return c
}

func addVMCommands(root *cobra.Command, o *options) {
	var osName string
	create := &cobra.Command{Use: "create name", Short: "Clone a prepared base image into a persistent owned VM", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		return o.with(cmd, func(a *app.App) error {
			selected := a.Config.DefaultOS
			if osName != "" {
				selected = model.OS(osName)
			}
			var v model.VM
			err := ui.Progress(cmd.Context(), "Creating persistent VM", !o.json, func(ctx context.Context) error { var err error; v, err = a.Create(ctx, args[0], selected); return err })
			if err != nil {
				return err
			}
			return o.print(cmd, v)
		})
	}}
	create.Flags().StringVar(&osName, "os", "", "linux or macos")
	root.AddCommand(create)
	root.AddCommand(&cobra.Command{Use: "start name", Short: "Boot an owned VM; preserve it after this command", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		return o.with(cmd, func(a *app.App) error {
			var v model.VM
			err := ui.Progress(cmd.Context(), "Booting VM", !o.json, func(ctx context.Context) error { var err error; v, err = a.Start(ctx, args[0]); return err })
			if err != nil {
				return err
			}
			return o.print(cmd, v)
		})
	}})
	root.AddCommand(&cobra.Command{Use: "stop name", Short: "Stop an owned VM, preserving disk and history", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		return o.with(cmd, func(a *app.App) error { return a.Stop(cmd.Context(), args[0]) })
	}})
	var yes bool
	del := &cobra.Command{Use: "delete name", Short: "Delete a stopped owned VM and its session metadata permanently", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		return o.with(cmd, func(a *app.App) error {
			return a.DeleteReviewed(cmd.Context(), args[0], func(v model.VM, ss []model.Session) error {
				fmt.Fprintln(cmd.ErrOrStderr(), "Permanently deleting VM", v.Name, "and its native history. Affected sessions:")
				for _, s := range ss {
					if s.VMID == v.ID {
						fmt.Fprintf(cmd.ErrOrStderr(), "  %s (%s)\n", s.Label(), s.ID)
					}
				}
				if !yes {
					if err := ui.Confirm(cmd.Context(), "Delete "+v.Name+" and all its history?"); err != nil {
						return err
					}
				}
				return nil
			})
		})
	}}
	del.Flags().BoolVar(&yes, "yes", false, "Confirm permanent deletion without prompting")
	root.AddCommand(del)
	root.AddCommand(&cobra.Command{Use: "list", Short: "List only owned VMs reconciled with live Tart state", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		return o.with(cmd, func(a *app.App) error {
			vs, err := a.List(cmd.Context())
			if err != nil {
				return err
			}
			return o.print(cmd, vs)
		})
	}})
	root.AddCommand(&cobra.Command{Use: "status name", Short: "Show owned VM status", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		return o.with(cmd, func(a *app.App) error {
			v, err := a.Store.VM(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			vs, err := a.List(cmd.Context())
			if err != nil {
				return err
			}
			for _, live := range vs {
				if live.ID == v.ID {
					return o.print(cmd, live)
				}
			}
			return fmt.Errorf("VM not found")
		})
	}})
	root.AddCommand(&cobra.Command{Use: "shell name", Short: "Open a native SSH shell (boots owned VM if needed)", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if err := ui.Require(); err != nil {
			return err
		}
		return o.with(cmd, func(a *app.App) error {
			v, err := a.Start(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			s, err := a.Connection(cmd.Context(), v, true)
			if err != nil {
				return err
			}
			return s.Shell(cmd.Context())
		})
	}})
	var fingerprint string
	trust := &cobra.Command{Use: "trust name", Short: "Enroll a guest host key after independent console fingerprint verification", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		return o.with(cmd, func(a *app.App) error {
			v, err := a.Start(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			s, err := a.Connection(cmd.Context(), v, false)
			if err != nil {
				return err
			}
			return s.Trust(cmd.Context(), fingerprint)
		})
	}}
	trust.Flags().StringVar(&fingerprint, "fingerprint", "", "SHA256 fingerprint read from the guest console")
	root.AddCommand(trust)
	var adoptedOS, image string
	var adoptYes bool
	adopt := &cobra.Command{Use: "adopt name", Short: "Explicitly take ownership of an existing Tart VM (assert its OS)", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if !adoptYes {
			return fmt.Errorf("adoption grants lifecycle/delete authority; pass --yes and --os explicitly")
		}
		if adoptedOS == "" {
			return fmt.Errorf("--os required")
		}
		return o.with(cmd, func(a *app.App) error {
			v, err := a.Adopt(cmd.Context(), args[0], model.OS(adoptedOS), image)
			if err != nil {
				return err
			}
			return o.print(cmd, v)
		})
	}}
	adopt.Flags().StringVar(&adoptedOS, "os", "", "Assert actual guest OS")
	adopt.Flags().StringVar(&image, "source-image", "unknown-adopted", "Provenance label")
	adopt.Flags().BoolVar(&adoptYes, "yes", false, "Explicitly take ownership")
	root.AddCommand(adopt)
	root.AddCommand(&cobra.Command{Use: "provision name", Short: "Verify prepared guest prerequisites and record provisioning version", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		return o.with(cmd, func(a *app.App) error {
			return ui.Progress(cmd.Context(), "Provisioning managed guest directories", !o.json, func(ctx context.Context) error { return a.Provision(ctx, args[0]) })
		})
	}})
}
