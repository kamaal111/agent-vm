# agent-vm

A Go CLI for persistent Linux ARM64 and macOS ARM64 Tart VMs on an Apple Silicon Mac. Launch Codex CLI, Claude Code or Cursor Agent in independent guest project copies and persistent tmux sessions. The host is the Mac running this command.

**Development release:** automated checks, isolated tmux checks, and real Linux/macOS 27 guest setup have run; authenticated vendor account integrations remain untested. [Verification and limitations](docs/verification.md) lists the remaining live checks. Cursor history discovery and newer Codex history layouts need explicit binding; see the [support matrix](docs/agent-support.md). No images, personal credentials, or host virtualization packages are installed by building this repository.

## Install

On an Apple Silicon Mac:

```sh
brew install kamaal111/tap/agent-vm
```

Then follow [First setup](#first-setup) to configure Tart and your VMs.

Run `agent-vm run codex` from your project directory. The first run offers to download a pinned upstream VM image, configure SSH, and install Codex. A configuration file is optional.

## Build from source

Use Go **1.27.3** ([verified versions and sources](docs/technology-baseline.md)) and `just` for the repository task commands. Run `just` to list available commands. The executable builds without CGO:

```sh
just build
install -m 755 bin/agent-vm /usr/local/bin/agent-vm
agent-vm --help
agent-vm completion zsh > /path/on/your/fpath/_agent-vm
```

Choose an installation directory you can write, such as `~/.local/bin`, if `/usr/local/bin` needs administrator access. Add it to PATH. Go modules and checksums are pinned in go.mod/go.sum; no npm or guest tooling is required to build the host CLI.

For release publishing, see [releases.md](docs/releases.md).

## First setup

From your project directory on an Apple Silicon Mac:

```sh
agent-vm run codex
```

Review the setup summary and confirm once. The CLI uses Ubuntu 24.04 ARM64 by default. It downloads the pinned upstream Cirrus Labs image, creates a persistent VM, generates a dedicated SSH identity, authorizes its public key, retrieves the guest host key through Tart's local channel, and installs the requested agent and prerequisites inside the guest. Private host keys never enter the guest. If system Tart is missing or too old, the same confirmation covers downloading a checksum-verified Tart release into the private data directory. Homebrew is not required for this path and the system installation is left untouched. A private Tart installation is reused by commands using the same data directory.

Image downloads can be several gigabytes and guest tool installation takes time. Progress and installation diagnostics go to stderr. Canceled or failed setup preserves the owned VM; the error/progress names it so you can retry with `--vm NAME`. Unrelated Tart VMs are never adopted automatically.

If Codex needs authentication, the CLI prints the guest shell and login commands:

```sh
agent-vm shell agent-linux
# Inside the guest:
codex login
exit
# Back on the host:
agent-vm run codex --vm agent-linux
```

For Cursor on macOS, a locked guest login keychain is reported with `security unlock-keychain` followed by the native login command. Enter the guest keychain password at its prompt. Installation is retained and retry does not download it again; credential storage is not changed automatically.

See [authentication.md](docs/authentication.md) for headless login options. No conversation is created and no project files are transferred until the agent's authentication check succeeds. Cursor status is parsed using the pinned version's explicit `isAuthenticated` boolean; a successful status-command exit alone does not mean it is logged in.

For macOS 27 Golden Gate:

```sh
agent-vm run codex --os macos
```

Linux automatic setup requires macOS 14+; the macOS 27 default requires a macOS 27+ host. Both need compatible Apple Silicon hardware, supported Tart/system SSH versions, adequate storage/RAM, and outbound network access. Default image references are embedded immutable digests; upstream floating tags are never resolved during setup. Existing VMs keep their OS, installed tools, credentials, and history when defaults change.

`--new` creates one uniquely named persistent VM from the selected OS image and performs the same setup. If login is needed, retry with the printed `--vm` name, omitting `--new`. Running another agent in a managed VM offers to install that agent without reinstalling agents already prepared.

For unattended use, explicitly authorize setup and detach:

```sh
agent-vm run codex --yes --detach --json
```

`--detach` alone does not approve downloads or installation. Noninteractive and JSON runs never prompt; without `--yes`, missing setup returns an actionable error. Vendor authentication remains a separate guest login.

### Configuration and custom images

Defaults are per host/user; no project config is required or discovered. Inspect effective defaults without creating a config or SSH key:

```sh
agent-vm config show
agent-vm doctor
agent-vm doctor --os macos
```

Doctor checks the selected/default OS and reports missing setup separately from broken prerequisites. It does not download images or prove a guest can boot.

Use `agent-vm config init --defaults` to write an optional template at `~/.config/agent-vm/config.toml`. Omit image settings to use the built-in upstream images; omit the identity setting to generate a dedicated key beneath the data directory during approved setup. The generated template omits these optional keys. Only add them when overriding a default. Edit an existing file rather than initializing it again. Explicit paths must be absolute; no tilde expansion is performed. [config.example.toml](config.example.toml) shows overrides. Use the same `--config` and `--data-dir` on subsequent commands when overriding them.

For a custom prepared image, set its actual local name or immutable OCI digest in `linux.base_image` or `macos.base_image`, set its guest `user`, and configure an authorized `identity_file`. Explicit image overrides use the manual preparation and trust workflow in [base-images.md](docs/base-images.md); the tool does not install packages or alter SSH in custom/existing adopted VMs automatically.

## Conversations and persistence

From the source project's directory (missing default-image setup is offered automatically):

```sh
agent-vm run codex
agent-vm run claude --os linux --name dark-mode --prompt 'Implement dark mode'
agent-vm run cursor --vm experiment -- --model auto
agent-vm run codex --new --os macos
agent-vm run                         # configured default_agent
agent-vm run codex --detach --json   # native agent runs in guest tmux
```

Explicit `--vm` wins; otherwise explicit `--os` selects that OS's default VM; otherwise configured default OS/default VM wins. A missing default VM is prepared after setup approval, a stopped VM is booted, and a running VM is reused. `--vm` plus `--new`, or an explicit OS conflicting with an existing VM, is rejected. `--new` creates a uniquely named **persistent** VM and never changes defaults.

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

## Terminal and SSH workflow

Open a terminal locally or connect over SSH to an Apple Silicon Mac, change into your source repository and run `agent-vm`. Select a conversation or start one with `run`. Open another terminal to run an independent agent/OS combination. Each guest process has its own tmux session and workspace. If a connection disconnects, reconnect to the **same host**, run `agent-vm resume`, and select the conversation. Configuration and the registry are local to that host.

Use `--detach` and explicit arguments for noninteractive launch; use `--json` for lists/status/show/doctor or detached launch. Progress goes to stderr, never JSON stdout. Interactive shell/picker/attach requires a TTY. Delete needs `--yes` without one. `--config` and `--data-dir` isolate configuration and state; keep the data directory on a local disk for SQLite/WAL and advisory locking.

## Development

```sh
just test
just check
AGENT_VM_TMUX_TEST=1 go test ./internal/guest -run TestTmux -v
```

Python release-tool tests use pytest: run `uv run --locked pytest` with [uv](https://docs.astral.sh/uv/) installed.

`just check` runs formatting verification, module verification, vet, tests, race checks, pinned govulncheck and a CGO-free macOS ARM64 build. The optional tmux check uses a temporary home and socket. Real VM checks are [explicitly opt-in](docs/live-validation.md). [Architecture](docs/architecture.md), [implementation plan](docs/implementation-plan.md), [baseline](docs/technology-baseline.md) and [update workflow](docs/base-images.md#updates) explain the boundaries. Dependabot updates modules and CI action major versions; updates must pass compatibility tests and baseline review.
