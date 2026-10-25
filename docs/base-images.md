# Reproducible base images

No public image digest has been live-validated here. `base_image` is deliberately unset by default. You must select an immutable ARM64 image digest or a versioned local prepared image. `agent-vm` never silently pulls `latest`; OCI references without `@sha256:` are rejected. A local image is an operator-managed reference: keep its manifest and never mutate/reuse its versioned name.

Both OS images need an unprivileged SSH account, key authorization, Python >=3.8, Git 2.x, tmux 3.8, the selected agent executables, a normal POSIX shell, tar, and outbound vendor network access. The explicit agent-installation script requires verified stable Node **26.12.1** and npm **12.2.1**; npm's engine range includes Node >=26, while Claude Code requires >=22. System packages must come from your pinned OS image/package snapshot. Record their exact versions. This project does not claim that unpinned apt/Homebrew installs are reproducible.

## Linux ARM64

1. Obtain a Linux ARM64 image or install media from its official distributor and verify its published digest. Tart's [Linux installation guidance](https://tart.run/quick-start/#linux) describes creating an empty Linux VM and attaching installation media. Use a pinned ISO/digest, never a floating example tag.
2. Install the guest OS and tools from a recorded package snapshot. Enable OpenSSH server, authorize only your intended public key, restrict SSH to the guest account, disable password authentication, and verify networking. Do not embed host private keys, vendor tokens or source projects.
3. Obtain/build tmux **3.8** from its [tagged release](https://github.com/tmux/tmux/releases/tag/3.8), recording artifact checksum and build/package dependencies. Install Python, Git and a supported current Node/npm from your pinned package sources.
4. Copy this repository's scripts into the disposable preparation VM. Set `SOURCE_IMAGE_DIGEST` to the verified source provenance and `CURSOR_SHA256` to the checksum independently verified for the **pinned** Linux ARM64 Cursor archive. Run `sh scripts/prepare-linux.sh` **inside that VM**. This explicitly downloads/installs pinned agent versions and writes manifests; it is never run automatically by the host CLI.
5. Remove preparation sources/caches if appropriate, verify there are no private projects/authenticated accounts, stop the image and give it a new immutable local name such as `agent-linux-base-v1`. Preserve a manifest with source digest, OS/package inventory, script commit/hash and vendor artifact checksums. Prefer pushing an explicitly approved credential-free image to your own OCI registry and recording the returned digest.

## macOS ARM64

1. On Apple hardware, obtain a specific Apple restore image under its applicable license and record its verified digest. Use Tart's [macOS preparation guidance](https://tart.run/quick-start/#macos) with that IPSW rather than `--from-ipsw=latest`. Complete installation manually in a fresh guest account.
2. Enable Remote Login for the intended guest user. Install its intended host **public** key in `authorized_keys` (0600), directory 0700. Disable password SSH authentication after verifying key access. Use DHCP networking; no Tart guest-agent service is required.
3. Install pinned development prerequisites from recorded distributions/package definitions: Git 2.x, Python >=3.8, tmux 3.8, Node/npm. Do not unexpectedly modify host Homebrew; all preparation runs **inside the disposable guest**. Preserve the OS build and package inventory.
4. Run `sh scripts/prepare-macos.sh` with explicit source provenance and the checksum of the pinned **Darwin ARM64** Cursor artifact. Do not log in to agents or an Apple personal account during base-image preparation.
5. Stop and seal as `agent-macos-base-v1` or your registry digest. Keep manifests separate from images and verify no personal Keychain/profile credentials were introduced. Observe Apple's VM/license restrictions; this CLI does not bypass them.

`install-agents.sh` pins npm packages `@openai/codex@0.164.0` and `@anthropic-ai/claude-code@2.1.310`. It uses the dated Cursor URL `https://downloads.cursor.com/lab/2026.10.22-27dcf5f/OS/arm64/agent-cli-package.tar.gz` found in the official installer, requires a separately verified checksum, and saves the generated npm package-lock in the image. Cursor does not provide a verified checksum in the inspected installer; no checksum is invented here. For stronger provenance, obtain your approved checksum through a separate trusted channel. These preparation scripts have syntax checks only; package installation is not live-validated.

Clone images get independent disks but may initially share the base's SSH host key. Prefer regenerating host keys in each clone through a trusted console before enrollment (`ssh-keygen -A` after deliberate key removal using the guest's OS procedure). Enroll each clone's verified fingerprint using `agent-vm trust`. A stable per-VM host-key alias avoids IP-reuse collisions. Base images must never contain personal vendor authentication; provision credentials only after cloning.

`agent-vm provision NAME` validates guest OS/ARM64, Python/Git/tmux availability, creates private workspace/process/credential directories, and records provisioning version 1. Fresh runs perform the same checks. It does not install packages or update tools unexpectedly. Agent preflight enforces targeted vendor versions and required auth status where verified.

## Updates

1. Recheck [technology-baseline.md](technology-baseline.md) against official stable releases, major/module migrations, licenses and security advisories. Use `go list -m -versions` for each canonical major and `go list -m -u all`; do not assume @latest finds a new major.
2. Update exact module/tool pins and CI Go/action references together. Run `go mod tidy`, inspect `go list -m all` / `go mod graph`, then `make check`. Dependabot supplies module/action PRs; its green tests do not replace API/baseline review.
3. Review harness launch/resume/auth/discovery behavior at new tagged versions. Update adapters, scripts, tests and the support matrix together. Do not suppress the version contract just to run an unverified tool.
4. Build **new** clean Linux/macOS images from new pinned source digests. Record new package manifests and artifact checksums. Test through [live-validation.md](live-validation.md), seal under new names/digests, then explicitly change configuration for future VMs.
5. Existing persistent VMs retain their disks/tools/history. Changing a base image only affects future creation. Upgrade an existing VM deliberately after backup/export and version review, or create a fresh VM. Never rebuild a personal VM implicitly when resume fails.
