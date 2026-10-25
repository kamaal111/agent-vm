# Technology baseline

Verification date: **2026-10-24**. Stable releases only. All selected releases were rechecked against official upstream metadata before delivery with no newer stable versions found. Resolved graph: 68 modules including the main module, 225 requirement edges. No direct compatibility downgrade was needed. Node/npm preparation targets are pinned separately; npm 12.2.1 supports Node >=26, and Claude Code requires Node >=22. See verification.md for executed checks.

## Go and dependencies

Go **1.27.3**, verified using [official release JSON](https://go.dev/dl/?mode=json), [release notes](https://go.dev/doc/go1.27), and the official archive SHA-256. Module `go 1.27.3`; CI uses the same release. Follow [module guidance](https://go.dev/doc/modules/gomod-ref); use context-aware commands and database operations, wrapped errors, gofmt, vet and race tests.

| Canonical module | Pin | Required Go | Official release/docs |
|---|---|---|---|
| github.com/spf13/cobra | v1.10.2 | 1.15 | [release](https://github.com/spf13/cobra/releases/tag/v1.10.2), [docs](https://github.com/spf13/cobra/tree/v1.10.2) |
| charm.land/bubbletea/v2 | v2.1.0 | 1.26.0 | [release](https://github.com/charmbracelet/bubbletea/releases/tag/v2.1.0), [migration](https://github.com/charmbracelet/bubbletea/blob/v2.1.0/UPGRADE_GUIDE_V2.md) |
| charm.land/bubbles/v2 | v2.2.1 | 1.25.0 | [release](https://github.com/charmbracelet/bubbles/releases/tag/v2.2.1), [v2 changes](https://github.com/charmbracelet/bubbles/releases/tag/v2.0.0) |
| charm.land/lipgloss/v2 | v2.0.6 | 1.25.0 | [release/docs](https://github.com/charmbracelet/lipgloss/tree/v2.0.6) |
| charm.land/huh/v2 | v2.0.3 | 1.25.8 | [release/docs](https://github.com/charmbracelet/huh/tree/v2.0.3), [migration](https://github.com/charmbracelet/huh/blob/v2.0.3/UPGRADE_GUIDE_V2.md) |
| modernc.org/sqlite | v1.61.0 | 1.26.0 | [repository](https://gitlab.com/cznic/sqlite), [API/changelog](https://pkg.go.dev/modernc.org/sqlite@v1.61.0) |
| github.com/pelletier/go-toml/v2 | v2.4.3 | 1.21.0 | [release](https://github.com/pelletier/go-toml/releases/tag/v2.4.3), [docs](https://github.com/pelletier/go-toml/tree/v2.4.3) |

Confirmed each pin and GoVersion with `go list -m -json`. Charm v2 uses charm.land imports, `tea.KeyPressMsg`, and `View() tea.View`. No v1 compatibility layer. TOML uses the stable v2 decoder, not its unstable AST. SQLite uses database/sql, foreign keys, busy timeout, short transactions and WAL on local storage; preserve its selected libc dependency. See [SQLite WAL guidance](https://sqlite.org/wal.html). Go resolves all compatible latest stable direct modules together; no dependency downgrade exception anticipated. Transitive pseudo-versions selected by stable upstream modules are recorded in go.sum, not opted-in direct experimental APIs.

Build tooling: [govulncheck](https://go.dev/security/vuln/) `golang.org/x/vuln/cmd/govulncheck@v1.9.0`, verified via Go module metadata. CI actions verified from official GitHub tag refs: checkout v7.0.1 `3d3c42e5aac5ba805825da76410c181273ba90b1`; setup-go v7.0.0 `b7ad1dad31e06c5925ef5d2fc7ad053ef454303e`.

## External boundaries

| Tool | Verified current stable/target | Local testing |
|---|---|---|
| Tart | 2.41.1 | Missing; fake-command testing only |
| tmux | 3.8 | Installed 3.8; local integration check |
| OpenSSH | upstream 10.6p1 | Apple's installed 10.3p1; retain system OpenSSH |
| Codex | 0.164.0 (`rust-v0.164.0`) | Native CLI/auth/VM integration untested |
| Claude Code | 2.1.310 | Native CLI/auth/VM integration untested |
| Cursor Agent | 2026.10.22-27dcf5f | Native CLI/auth/VM integration untested |
| Node (explicit guest preparation only) | 26.12.1 | Not installed/tested; [official release index](https://nodejs.org/dist/index.json) |
| npm (explicit guest preparation only) | 12.2.1 | Not installed/tested; [official registry](https://registry.npmjs.org/npm/12.2.1) |

Targets are deliberately strict initially: Tart >=2.41.1 within major 2, tmux >=3.8 within major 3, system OpenSSH >=10.3 within major 10; harnesses use the recorded version contract. Later versions require explicit baseline review rather than silent behavioral drift. Native capability checks supplement versions.

Sources: [Tart release](https://github.com/openai/tart/releases/tag/2.41.1), [tagged commands](https://github.com/openai/tart/tree/2.41.1/Sources/tart/Commands), [tmux release/manual](https://github.com/tmux/tmux/tree/3.8), [OpenSSH releases](https://www.openssh.org/releasenotes.html), [ssh_config](https://man.openbsd.org/ssh_config), [Codex release](https://github.com/openai/codex/releases/tag/rust-v0.164.0), [Claude releases](https://github.com/anthropics/claude-code/releases), [Cursor official installer](https://cursor.com/install) (read, not executed; contains immutable dated build URL).

The former cirruslabs/tart repository redirects to openai/tart, rather than being a separately maintained provider. Its README now installs `openai/tools/tart`; tart.run still contains Cirrus image references and permissive SSH/password examples. Use tagged source for flags: clone, set --cpu/--memory/--disk-size, run --no-graphics, stop, delete, list --source local --format json, ip --wait using DHCP. Do not require the optional guest-agent IP resolver. CPU is count, memory MiB, disk GB. Disk growth cannot shrink existing images.

SSH: batch-mode key authentication, dedicated known_hosts, stable per-VM HostKeyAlias, strict verification after explicit enrollment. Never use global StrictHostKeyChecking=no or /dev/null known_hosts. Verify enrollment fingerprint independently in the guest console.

## Images, provisioning and licensing

No image was downloaded or tested. No default floating image reference is supplied. Configure a local prepared image name with a versioned manifest, or OCI `@sha256:` digest. Separate Linux/macOS preparation workflows record source digest, provisioning version and exact agent versions. Local images can be replaced outside this tool, so preserve their manifest and immutable exported digest for reproducible release builds.

[Tart 2.41.1 license](https://github.com/openai/tart/blob/2.41.1/LICENSE) is FSL-1.1-ALv2, with permitted internal use and competing-use restrictions; it is not an unrestricted Apache release today. This project invokes a separately installed Tart and does not redistribute it. macOS images require Apple hardware and applicable Apple software license rights; obtain them yourself. Codex is Apache-2.0; Claude Code and Cursor distributions and account access have vendor terms. Do not redistribute their binaries or preauthenticated images without checking those terms. Go libraries have permissive licenses; preserve notices when distributing the binary. See agent-support.md for per-agent sources and limitations.
