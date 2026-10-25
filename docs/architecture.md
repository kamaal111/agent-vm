# Architecture

`cmd/agent-vm` owns signal cancellation and exit-code reporting. `internal/cli` contains Cobra handlers; `internal/ui` owns Bubble Tea/Bubbles/Lip Gloss presentation and Huh forms. Help/completion bypass configuration, SQLite and Tart. Interactive programs finish before SSH receives the terminal.

`internal/config` decodes strict TOML and validates known options. `internal/app` sequences operations. `internal/tart` is the sole virtualization provider; `internal/host.Runner` is the meaningful command/process boundary used in tests. Commands receive argv slices; SSH's single remote command and the guest launcher quote each argument independently. Command errors intentionally omit argv/output to avoid prompt/credential leakage.

`internal/guest` uses system OpenSSH with key-only batch connections, an explicit identity, dedicated known_hosts and stable per-VM host-key aliases. Its embedded Python helper is a short-lived SSH command, not a service. It uses only Python's standard library for atomic credential-independent process launch, tmux state and structured history reads. POSIX scripts prepare images explicitly. `internal/agent` owns per-harness launch/resume/version rules. Authentication is explicit guest login or deliberate Codex file-cache import; no host directory mirroring exists.

The domain keeps separate identities:

1. Configured base image/provisioning version: creation input, never a session.
2. VM UUID/name: persistent Tart disk and guest account.
3. Workspace UUID/project/source/path/baseline: one immutable initial transfer destination for a conversation.
4. Session UUID/optional unique name/harness/native ID/history/timestamps: host metadata pointing to guest-owned history.
5. Process: guest `avm-UUID` tmux session plus private launch/exit records; its status is queried rather than trusted from SQLite.

SQLite uses database/sql/modernc, a versioned schema, foreign keys, unique constraints, local WAL and busy timeout. Workspace+session insertion and metadata deletion use short transactions. There are no transactions during boot/SSH/interactive use. Metadata is private under the data directory; SQLite never stores credentials or initial prompts. Harness extra arguments are recorded to reproduce launch behavior, so known credential flags are rejected.

OS advisory locks serialize lifecycle mutations per registry. A creation intent is persisted before cloning, so partial clone/configuration failure remains visible and explicitly recoverable with `start`. Missing VMs whose creation had completed are never silently replaced. Deletion reviews affected sessions while holding the lifecycle lock. Session locks serialize resume preparation; guest `flock` locks also guard launches. Locks are released on process death, with no stale lock-file cleanup race. Long operations are context-cancellable. No transaction stays open during them.

Reconciliation queries live Tart JSON and tmux format fields. A process exit marker preserves the agent status separately from SSH detach/network exit. Interactive attachment releases host locks, allowing other sessions to operate. Stopping a VM kills live processes but preserves native saved history. External manual Tart edits/name replacements cannot be proven to preserve VM identity: do not rename/replace/delete managed VMs behind the registry; use lifecycle commands. Ownership is scoped to a private local registry, not a host-wide daemon.

The registry is not shared across hosts/data directories. Two independent registries do not coordinate adoption of the same Tart VM; use one registry per host. Advisory locks coordinate cooperating CLI commands, not arbitrary manual Tart or guest shell operations. Linux hosts are unsupported; Linux is a guest choice. Standard tests replace the host capability detector at the Tart boundary so they can run on other development platforms.
