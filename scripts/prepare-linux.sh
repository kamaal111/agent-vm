#!/bin/sh
set -eu
test "$(uname -s)" = Linux || { echo 'Run inside Linux guest' >&2; exit 1; }
test -n "${SOURCE_IMAGE_DIGEST:-}" || { echo 'Set SOURCE_IMAGE_DIGEST to immutable source provenance' >&2; exit 1; }
for tool in ssh-keygen python3 git tmux; do command -v "$tool" >/dev/null; done
test ! -e "$HOME/.codex/auth.json" || { echo 'Use a clean image without personal authentication' >&2; exit 1; }
test ! -e "$HOME/.claude/.credentials.json" || { echo 'Use a clean image without personal authentication' >&2; exit 1; }
mkdir -p "$HOME/.ssh"
chmod 700 "$HOME/.ssh"
printf '%s\n' 'Install ONLY the intended host public SSH key in ~/.ssh/authorized_keys (0600).'
printf '%s\n' 'Enable sshd, disable password authentication and verify access before sealing.'
"$(dirname "$0")/install-agents.sh"
printf 'source_image = "%s"\nprovision_version = "1"\n' "$SOURCE_IMAGE_DIGEST" > "$HOME/.local/share/agent-vm-tools/image.toml"
