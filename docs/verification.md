# Verification results

Implementation date: 2026-10-24. Host: Apple Silicon macOS, Darwin 27.0.0/ARM64. System OpenSSH 10.3p1; tmux 3.8. Go 1.27.3 was downloaded into `/tmp` from go.dev and verified against its published SHA-256. No host Go/Tart installation, VM image download, vendor login or personal credential inspection was performed.

## Automated checks

The repository's aggregate check is `sh scripts/check.sh` (or `just check`). It passed gofmt verification, module checksum verification, `go vet ./...`, `go test ./...`, `go test -race ./...`, govulncheck v1.9.0 (**No vulnerabilities found**), a CGO-disabled Darwin ARM64 build, Python syntax, and shell syntax. There are 40 named tests, including one opt-in local tmux integration test run separately. The resulting `bin/agent-vm` is a Mach-O ARM64 executable.

Official `go list -m all` and `go mod graph` were inspected (68 modules including main, 225 edges). Initial sandboxed module-metadata lookups failed due network DNS restrictions; the same official-tool queries succeeded with network escalation. Stable releases and exact npm package engine requirements were rechecked before delivery; pins remained current. Exact direct imports and pins are in technology-baseline.md/go.mod/go.sum.

CLI smoke checks passed: help/completion, default config generation, clean empty session-list JSON, and doctor returning a nonzero exit with valid JSON for missing Tart/configuration. A PTY exercise with synthetic metadata passed picker rendering, no-match filtering, Escape exit and ordinary terminal input afterward. All terminal flags restored exactly after canonical input consumed macOS's transient PENDIN bit; this bit is documented as pending-input state in the installed SDK termios header. No extra terminal-state workaround was introduced.

Meaningful tests cover selection precedence, conflicting OS/flags, ownership/collision safeguards, creation/start/reuse/stop, concurrent lifecycle and resume calls, partial creation recovery, missing-VM refusal, session uniqueness/rename/transactions, live attachment versus native resume, missing histories, exact prompt argv, shell quoting/cancellation/exit codes, strict SSH trust, structured history ambiguity, independent Git worktree snapshots, export of committed/binary/deleted/untracked changes, credential import conflict/no argument leakage, JSON cleanliness, lightweight help, and picker filtering/narrow/empty/cancel states.

The additional opt-in local tmux test ran with an isolated socket and temporary HOME using installed tmux 3.8. It passed quote/newline/empty-argument preservation, no shell evaluation and agent exit status 42. A sandboxed attempt could not start tmux; the same isolated test passed with sandbox escalation. It never connected to personal tmux sessions or vendor accounts.

## Remaining live validation

- Tart is absent. Real clone/configure/boot/stop/delete/IP, VM resources and SSH connectivity have not run.
- No Linux/macOS images were downloaded or prepared. Guest installer scripts have syntax checks only; exact vendor package install and image manifests need validation. Cursor's dated URL is verified from its official installer, but artifact checksum/availability is not verified here.
- Native Codex/Claude/Cursor UIs, auth/token refresh, subscription versus API-key access, vendor argument parsing, SSH terminal resize/signal handling and all six harness/OS combinations remain untested live.
- Codex JSONL discovery is a pinned internal-format reader. New paginated Codex history and Cursor automatic saved-history discovery are unsupported; explicit binding is required. No experimental app-server API is substituted.
- Native Cursor auth-status schema/exit semantics are unverified, so preflight does not claim authenticated status; the native interface decides.
- Project snapshots intentionally omit original Git history/remotes/staging metadata and reject submodules/escaping symlinks; no atomic host filesystem snapshot or LFS hydration is promised.
- Multiple guest sessions share native account credentials. Cross-VM refresh-token coordination and credential synchronization are not implemented; independent guest login is preferred.
- UI model tests and local tmux cover parts of terminal behavior; end-to-end Herdr/SSH handoff still requires manual checks.

Follow [live-validation.md](live-validation.md) before relying on this development release for production work. The implementation is testable and builds locally, but this document does not certify untested integrations.
