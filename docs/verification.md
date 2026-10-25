# Verification results

Original implementation verification: 2026-10-24. This paragraph and the original check notes below describe that initial run; current first-run validation is recorded separately. Host tooling and personal credentials were not modified during the later validation.

Implementation date: 2026-10-24. Host: Apple Silicon macOS, Darwin 27.0.0/ARM64. System OpenSSH 10.3p1; tmux 3.8. Go 1.27.3 was downloaded into `/tmp` from go.dev and verified against its published SHA-256. No host Go/Tart installation, VM image download, vendor login or personal credential inspection was performed.

## Automated checks

The repository's aggregate check is `sh scripts/check.sh` (or `just check`). It passed gofmt verification, module checksum verification, `go vet ./...`, `go test ./...`, `go test -race ./...`, govulncheck v1.9.0 (**No vulnerabilities found**), a CGO-disabled Darwin ARM64 build, Python syntax, and shell syntax. There are 40 named tests, including one opt-in local tmux integration test run separately. The resulting `bin/agent-vm` is a Mach-O ARM64 executable.

Official `go list -m all` and `go mod graph` were inspected (68 modules including main, 225 edges). Initial sandboxed module-metadata lookups failed due network DNS restrictions; the same official-tool queries succeeded with network escalation. Stable releases and exact npm package engine requirements were rechecked before delivery; pins remained current. Exact direct imports and pins are in technology-baseline.md/go.mod/go.sum.

CLI smoke checks passed: help/completion, default config generation, clean empty session-list JSON, and doctor returning a nonzero exit with valid JSON for missing Tart/configuration. A PTY exercise with synthetic metadata passed picker rendering, no-match filtering, Escape exit and ordinary terminal input afterward. All terminal flags restored exactly after canonical input consumed macOS's transient PENDIN bit; this bit is documented as pending-input state in the installed SDK termios header. No extra terminal-state workaround was introduced.

Meaningful tests cover selection precedence, conflicting OS/flags, ownership/collision safeguards, creation/start/reuse/stop, concurrent lifecycle and resume calls, partial creation recovery, missing-VM refusal, session uniqueness/rename/transactions, live attachment versus native resume, missing histories, exact prompt argv, shell quoting/cancellation/exit codes, strict SSH trust, structured history ambiguity, independent Git worktree snapshots, export of committed/binary/deleted/untracked changes, credential import conflict/no argument leakage, JSON cleanliness, lightweight help, and picker filtering/narrow/empty/cancel states.

The additional opt-in local tmux test ran with an isolated socket and temporary HOME using installed tmux 3.8. It passed quote/newline/empty-argument preservation, no shell evaluation and agent exit status 42. A sandboxed attempt could not start tmux; the same isolated test passed with sandbox escalation. It never connected to personal tmux sessions or vendor accounts.

## Managed first-run validation (2026-10-25)

The default setup was exercised using an isolated temporary `TART_HOME`, config/data paths, and synthetic projects. Tart 2.41.1 was downloaded to temporary storage and verified against its release asset SHA-256. The system's existing Tart installation and personal credentials were left untouched. The private Tart installation path also passed with Tart and Homebrew absent from PATH.

Ubuntu 24.04 ARM64 setup passed against `ghcr.io/cirruslabs/ubuntu@sha256:3acc11bebedcbcb813fb17dd0a4671a8c2e6baed537f3caa7922e170ed8a04ed`: clone/configure/boot/IP, guest-channel SSH authorization, unique host-key generation/enrollment, strict SSH, provisioning, and exact Codex 0.164.0, Claude Code 2.1.310, Cursor 2026.10.22-27dcf5f, Node 26.12.1, npm 12.2.1, and tmux 3.8 installation. All three agents returned actionable missing-login instructions. Stop/start and retry reused the existing installation without confirmation. A real `--new` produced one persistent VM, a different host key, and retry commands naming that VM. The shared manual installer also passed against the prepared Linux guest.

macOS 27.0 (26A428), ARM64, passed clone/configure/boot/IP, local guest-agent readiness, automatic SSH authorization and host-key enrollment, and native Codex/Claude installation/version/missing-auth checks using `ghcr.io/cirruslabs/macos-golden-gate-base@sha256:5302a9ea6cd68c059d00fa253fe75c8ebdf4e0dc8c45d4e136ecc6e7a4c02b70`. tmux 3.8 builds with Homebrew utf8proc and jemalloc; Node/npm pins and inventories were verified. Clone disks are independent of the cached base. Stop/start and no-approval retry passed. The exact `run codex --new --os macos` workflow passed through missing-login instructions naming one new VM. Across both OSes, four owned guests had four distinct enrolled host keys, and no macOS conversation was created before login.

Cursor's macOS archive checksum and extraction passed, but the native launcher requires the guest login keychain to be unlocked. Setup now preserves the installed files and emits explicit unlock/login instructions. A second run without `--yes` repeated the instructions without installation or confirmation. Native macOS Cursor version/auth verification remains pending that manual prerequisite; no credential-store setting was weakened to bypass it.

Live testing exposed and corrected Ubuntu's SSH runtime-directory/service startup, npm 12's blocked Claude postinstall, Cursor's actual `cursor-agent` archive entry point, Cursor's zero exit status while unauthenticated, and tmux's macOS build options. The pinned Cursor JSON `isAuthenticated` boolean is now checked explicitly; unsupported schemas fail without exposing returned credential data.

A PTY exercise confirmed that Cancel restores the terminal and produces no VM/key or mutating Tart command. A noninteractive detached JSON run without `--yes` refused setup with empty stdout and no VM/key. Automated coverage additionally exercises interrupted clone/boot/SSH/tool setup, concurrency, checksum rejection before installation, custom-image preservation, and prerequisite reuse without package upgrades. Run the aggregate check described above for the complete current suite; the original count of 40 tests predates these additions.

## Remaining live validation

- Authenticated model requests, vendor login/token refresh, subscriptions versus API-key access, full native UIs, vendor argument parsing, and end-to-end SSH resize/signal handling still require explicit vendor test accounts. No personal login was performed.
- Codex JSONL discovery is a pinned internal-format reader. New paginated Codex history and Cursor automatic saved-history discovery are unsupported; explicit binding is required. No experimental app-server API is substituted.
- Project snapshots intentionally omit original Git history/remotes/staging metadata and reject submodules/escaping symlinks; no atomic host filesystem snapshot or LFS hydration is promised.
- Multiple guest sessions share native account credentials. Cross-VM refresh-token coordination and credential synchronization are not implemented; independent guest login is preferred.
- UI model tests and local tmux cover parts of terminal behavior; end-to-end terminal/SSH handoff still requires manual checks.

Follow [live-validation.md](live-validation.md) before relying on this development release for production work. The implementation is testable and builds locally, but this document does not certify untested integrations.
