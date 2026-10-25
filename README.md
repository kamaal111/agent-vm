# agent-vm

A Go CLI for persistent Linux ARM64 and macOS ARM64 Tart VMs on an Apple Silicon Mac. Launch Codex CLI, Claude Code or Cursor Agent in independent guest project copies and persistent tmux sessions. The host is the Mac running this command, including when you SSH into a Mac mini from Herdr.

**Development release:** local automated tests and isolated tmux checks have run; real Tart guests and vendor account integrations have not. [Verification and limitations](docs/verification.md) lists the remaining live checks. Cursor history discovery and newer Codex history layouts need explicit binding; see the [support matrix](docs/agent-support.md). No images, personal credentials, or host virtualization packages are installed by building this repository.

## Build and install

Use Go **1.27.3** ([verified versions and sources](docs/technology-baseline.md)). The executable builds without CGO:

```sh
make build
install -m 755 bin/agent-vm /usr/local/bin/agent-vm
agent-vm --help
agent-vm completion zsh > /path/on/your/fpath/_agent-vm
```

Choose an installation directory you can write, such as `~/.local/bin`, if `/usr/local/bin` needs administrator access. Add it to PATH. Go modules and checksums are pinned in go.mod/go.sum; no npm or guest tooling is required to build the host CLI.

## First setup

1. Install Tart 2.41.1 separately, after reviewing its license. Prepare separate credential-free images using [base-images.md](docs/base-images.md). No floating image defaults are provided.
2. Create a dedicated SSH key: `ssh-keygen -t ed25519 -f ~/.ssh/agent-vm`. Authorize its public key in your prepared guest account. Use a key available without a password prompt for batch SSH; a passphrase-protected key may be unlocked in your host SSH agent. Only the specified identity is offered.
3. Run `agent-vm config init`, or `agent-vm config init --defaults` for a noninteractive template. Edit `~/.config/agent-vm/config.toml`; [config.example.toml](config.example.toml) shows all options. Paths must be absolute; no shell or tilde expansion is performed.
4. Run `agent-vm doctor`. macOS/ARM64, Tart, system SSH, Git, images and identity must be configured. Doctor does not perform personal logins or download images.
5. Create, start and trust your first guest:

```sh
agent-vm create agent-linux --os linux
agent-vm start agent-linux
```

Read the guest's Ed25519 SSH host-key fingerprint from its trusted Tart console (`ssh-keygen -lf /etc/ssh/ssh_host_ed25519_key.pub -E sha256` in the guest). Enroll that exact fingerprint:

```sh
agent-vm trust agent-linux --fingerprint 'SHA256:the-fingerprint-you-verified'
agent-vm provision agent-linux
agent-vm shell agent-linux
```

Complete vendor authentication **inside the guest**, then exit its shell. See [authentication.md](docs/authentication.md). New uniquely named VMs also need explicit trust enrollment; an unattended `run --new` will preserve the VM and report the missing trust prerequisite. Use `list` to find its generated name, enroll it and run with `--vm`.

## Conversations and persistence

From the source project's directory:

```sh
agent-vm run codex
agent-vm run claude --os linux --name dark-mode --prompt 'Implement dark mode'
agent-vm run cursor --vm experiment -- --model auto
agent-vm run codex --new --os macos
agent-vm run                         # configured default_agent
agent-vm run codex --detach --json   # native agent runs in guest tmux
```

Explicit `--vm` wins; otherwise explicit `--os` selects that OS's default VM; otherwise configured default OS/default VM wins. A missing default VM is cloned, a stopped VM is booted, and a running VM is reused. `--vm` plus `--new`, or an explicit OS conflicting with an existing VM, is rejected. `--new` creates a uniquely named **persistent** VM and never changes defaults.

Every `run` creates a fresh conversation and a new project snapshot. An agent exiting or the SSH connection disconnecting leaves the VM and saved native history intact. tmux survives SSH disconnects; it does not survive shutting down the VM. When an attached agent exits, the CLI reports its recorded exit code when reachable. SSH status 255 means connection failure, not proof that the agent died. Detach deliberately with tmux's `Ctrl-b d`.

```sh
agent-vm sessions list
agent-vm sessions show dark-mode --live
agent-vm sessions rename dark-mode new-name
agent-vm attach new-name       # requires live process
agent-vm resume new-name       # attach if live; native resume if exited
agent-vm resume                # searchable picker
agent-vm                       # same picker
```

Type to filter; arrows move; Enter selects; Escape returns. The picker shows name, agent, project, VM/OS, last-known state and last-used time. Status is advisory until reconciled with `sessions show --live` or attach/resume. UI exits before SSH takes the terminal. Stable UUIDs keep working after a rename.

Resume never copies host files over the guest. Missing workspace/history is an error; it never silently starts a new conversation. Native conversation IDs/history stay in the VM; the host registry is metadata only. A manual binding is available when automatic discovery is unsupported:

```sh
agent-vm sessions bind new-name --native-id 'exact-native-id' --history '/absolute/guest/path/to/saved/history'
```

Binding asserts the native ID belongs to that file; the tool checks file/workspace existence and refuses to change an already recorded native ID. It cannot reconstruct deleted history. Changing agents creates a separate conversation.

## VM lifecycle and export

```sh
agent-vm create experiment --os linux
agent-vm start experiment
agent-vm list --json
agent-vm status experiment --json
agent-vm shell experiment
agent-vm stop experiment
agent-vm delete experiment --yes
agent-vm adopt existing-tart-vm --os macos --source-image prepared-v1 --yes
```

Only registry-owned or explicitly adopted VMs are managed. Adoption requires you to assert the actual guest OS; provisioning verifies it before launching agents. Stop preserves disk/history. Delete requires a stopped VM and confirmation, shows affected sessions under the lifecycle lock, then destroys history and metadata. No recovery is promised after deletion. Unrelated Tart VMs are never automatically adopted or removed.

```sh
agent-vm export new-name --output ./new-name-export.tar
```

Exit the agent before export. The archive contains a binary-capable Git patch against the **initial copied snapshot**, a NUL-delimited status listing, the baseline hash, and a complete current workspace archive (including untracked/ignored guest files, excluding `.git`). It includes committed agent changes and deletion records. Review/extract separately; the tool never applies, merges, pushes or overwrites an existing export. Archives may contain secrets the agent created: handle them as private artifacts. [Project isolation details](docs/project-isolation.md).

## Mac mini / Herdr workflow

Open a Herdr terminal tab, SSH to your Mac mini, change into your source repository and run `agent-vm`. Select a conversation or start one with `run`. Open another tab to run an independent agent/OS combination. Each guest process has its own tmux session and workspace. If a tab disconnects, reconnect to the **same Mac**, run `agent-vm resume`, and select the conversation. You can also use the CLI directly in Terminal on another Apple Silicon Mac; its configuration and registry are local to that host.

Use `--detach` and explicit arguments for noninteractive launch; use `--json` for lists/status/show/doctor or detached launch. Progress goes to stderr, never JSON stdout. Interactive shell/picker/attach requires a TTY. Delete needs `--yes` without one. `--config` and `--data-dir` isolate configuration and state; keep the data directory on a local disk for SQLite/WAL and advisory locking.

## Development

```sh
make test
make check
AGENT_VM_TMUX_TEST=1 go test ./internal/guest -run TestTmux -v
```

`make check` runs formatting verification, module verification, vet, tests, race checks, pinned govulncheck and a CGO-free macOS ARM64 build. The optional tmux check uses a temporary home and socket. Real VM checks are [explicitly opt-in](docs/live-validation.md). [Architecture](docs/architecture.md), [implementation plan](docs/implementation-plan.md), [baseline](docs/technology-baseline.md) and [update workflow](docs/base-images.md#updates) explain the boundaries. Dependabot updates modules and immutable CI action references; updates must pass compatibility tests and baseline review.
