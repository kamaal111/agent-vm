# Implementation plan

Build a local, agent-agnostic CLI for persistent Tart VMs and guest tmux sessions.

## Scope and decisions

Use the agreed Go/Cobra/Charm v2/SQLite/OpenSSH/tmux/Tart/TOML stack. Host means the Apple Silicon Mac executing the command. No daemon, host mounts, remote controller, automatic change application, or cross-agent conversion. Help and completion must not open the database or contact Tart.

Environment inspected 2026-10-24: empty directory, no Git metadata or applicable ancestor AGENTS.md; Darwin ARM64; no Go or Tart; OpenSSH 10.3p1; tmux 3.8. Download a checksum-verified Go toolchain to /tmp for development. Do not download VM images or install host virtualization software.

## Assumptions

- A1: users prepare credential-free, SSH-ready images and explicitly configure them. No trustworthy immutable image digest was supplied; do not invent one or use floating latest. Setup and VM creation depend on this prerequisite.
- A2: base images provide an unprivileged user, Python 3, Git, tmux and agent binaries. Guest scripts check this contract; preparation instructions cover both operating systems.
- A3: native history formats are version-sensitive. Use verified structured APIs where available; fail explicitly on unrecognized history rather than selecting an unrelated conversation.

## Before and after

| Scenario | Before | After |
|---|---|---|
| VM lifecycle | Absent | Owned persistent VMs created, started, reused, stopped, deleted with confirmation |
| Project | Absent | Independent snapshot with standalone Git metadata; explicit export |
| Agent | Absent | Native interface through SSH, persistent tmux process, separate attach/resume |
| Registry | Absent | SQLite VM/workspace/conversation/process metadata; no secrets |
| Terminal | Absent | Searchable picker exits before SSH handoff; JSON stays clean |

## Phases and files

1. `docs/technology-baseline.md`: verify official releases, canonical imports, migrations, tool commands, harness auth/resume and licenses before dependency selection.
2. `cmd/agent-vm`, `internal/cli`, `internal/config`, `internal/store`: thin Cobra handlers, strict TOML configuration, transactional schema, doctor and command completion. Deliver help/config/doctor first.
3. `internal/host`, `internal/tart`, `internal/app`: cancellable external-command boundary, OS validation, advisory file locks, ownership checks, durable creation intent, detached Tart run, bounded IP/SSH readiness, explicit adoption, shell and deletion session preview.
4. `internal/guest`, `scripts`: SSH key-only transport with per-VM known-host aliases and explicit trust enrollment; idempotent guest checks; credential-free image preparation/update workflows.
5. `internal/project`: snapshot tracked/modified/untracked nonignored files, exclude secrets/dependencies, materialize worktrees without host Git paths, new workspace per conversation, binary Git patch plus archive export without applying it.
6. `internal/agent`, `internal/app`: Codex fresh launch, native history identification, tmux process state/exit code, attach and native resume, duplicate prevention and missing-history errors.
7. Same adapters for Claude Code and Cursor, preserving their different session/auth mechanisms. Record unsupported discovery explicitly.
8. `internal/ui`: Bubbles input/spinner, Bubble Tea lifecycle, Lip Gloss styling; Huh setup/confirmation, narrow-screen/empty/cancel behavior, UI teardown before native SSH.
9. `README.md`, `docs/*`, `.github/*`, tests: walkthroughs, reproducible preparation, opt-in live validation, pinned CI and automated dependency updates. Recheck upstream, run formatting/vet/tests/race/vulnerability scan and CGO-free ARM64 build.

## Decisive examples and validation

- Accepted: default Linux VM absent → clone configured image once, persist ownership, start; second run reuses VM and creates another workspace/conversation.
- Rejected: `run codex --vm experiment --new` or Linux VM with `--os macos` → error before Tart mutation.
- Rejected: unrelated Tart name collision → no adoption or deletion; explicit `adopt` required.
- Accepted: `sessions rename dark-mode new-name` → stable ID unchanged; old name no longer resolves.
- Accepted: live session `resume` → attach only; dead session → verify workspace/history and native resume once under lock.
- Rejected: missing native history → actionable error, no fresh conversation or host synchronization.
- Accepted: Git worktree containing edits, deletion, binary/untracked files → independent guest repository; export retains changes without modifying host.
- Accepted: prompt containing quotes, spaces, newlines and `$()` → exact argv, no evaluation.
- Rejected: existing guest credentials → import conflict, preserve both copies.

Use fake external commands, real temporary SQLite/Git/filesystems, concurrent lock tests and local tmux checks. Real VM checks are opt-in and remain unverified until Tart and prepared images are available. Never run personal login flows during implementation.
