#!/bin/sh
set -eu
test "$(uname -s)" = Darwin || { echo 'Run inside macOS guest' >&2; exit 1; }
test "$(uname -m)" = arm64 || exit 1
test -n "${SOURCE_IMAGE_DIGEST:-}" || { echo 'Set SOURCE_IMAGE_DIGEST to immutable source provenance' >&2; exit 1; }
for tool in ssh-keygen python3 git tmux; do command -v "$tool" >/dev/null; done
test ! -e "$HOME/.codex/auth.json" || { echo 'Use a clean image without personal authentication' >&2; exit 1; }
mkdir -p "$HOME/.ssh"
chmod 700 "$HOME/.ssh"
printf '%s\n' 'Enable Remote Login for the guest account, install the intended host public key, and disable password SSH authentication.'
"$(dirname "$0")/install-agents.sh"
printf 'source_image = "%s"\nprovision_version = "1"\n' "$SOURCE_IMAGE_DIGEST" > "$HOME/.local/share/agent-vm-tools/image.toml"
