#!/bin/sh
# Run explicitly INSIDE a credential-free image-preparation VM.
set -eu
umask 077
case "$(uname -m)" in arm64|aarch64) ;; *) echo 'ARM64 guest required' >&2; exit 1;; esac
case "$(uname -s)" in Darwin) platform=darwin;; Linux) platform=linux;; *) exit 1;; esac
for tool in git tmux python3 node npm curl tar; do
  command -v "$tool" >/dev/null || { echo "Install prerequisite: $tool" >&2; exit 1; }
done
test "$(node --version)" = 'v26.12.1' || { echo 'Prepare pinned Node v26.12.1 first' >&2; exit 1; }
test "$(npm --version)" = '12.2.1' || { echo 'Prepare pinned npm 12.2.1 first' >&2; exit 1; }
test "$(tmux -V)" = 'tmux 3.8' || { echo 'Prepare tmux 3.8 first' >&2; exit 1; }
: "${CURSOR_SHA256:?Set the independently verified SHA256 of the pinned Cursor ARM64 archive}"
prefix="$HOME/.local/share/agent-vm-tools/v1"
test ! -e "$prefix" || { echo 'Tool version directory exists; build a new image instead of mutating it' >&2; exit 1; }
mkdir -p "$prefix" "$HOME/.local/bin"
npm install --prefix "$prefix" --save-exact @openai/codex@0.164.0 @anthropic-ai/claude-code@2.1.310
ln -s "$prefix/node_modules/.bin/codex" "$HOME/.local/bin/codex"
ln -s "$prefix/node_modules/.bin/claude" "$HOME/.local/bin/claude"
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT HUP INT TERM
url="https://downloads.cursor.com/lab/2026.10.22-27dcf5f/$platform/arm64/agent-cli-package.tar.gz"
curl --fail --location --proto '=https' "$url" -o "$tmp/cursor.tar.gz"
printf '%s  %s\n' "$CURSOR_SHA256" "$tmp/cursor.tar.gz" > "$tmp/checksum"
if command -v sha256sum >/dev/null; then sha256sum -c "$tmp/checksum"; else shasum -a 256 -c "$tmp/checksum"; fi
mkdir "$prefix/cursor"
tar -xzf "$tmp/cursor.tar.gz" --strip-components=1 -C "$prefix/cursor"
ln -s "$prefix/cursor/agent" "$HOME/.local/bin/agent"
ln -s "$prefix/cursor/agent" "$HOME/.local/bin/cursor-agent"
export PATH="$HOME/.local/bin:$PATH"
codex --version
claude --version
agent --version
printf 'provision_version = "1"\ncodex = "0.164.0"\nclaude = "2.1.310"\ncursor = "2026.10.22-27dcf5f"\n' > "$prefix/manifest.toml"
echo 'Record OS, source image digest, package manifests, Node/npm/Python/Git/tmux versions and Cursor checksum with this image.'
