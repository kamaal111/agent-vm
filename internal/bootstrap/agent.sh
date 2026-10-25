target="$base/$selected-$version"
if [ ! -f "$target/complete" ]; then
  rm -rf "$target.incomplete"
  mkdir "$target.incomplete"
  case "$selected" in
    codex) npm install --prefix "$target.incomplete" --save-exact "@openai/codex@$version"; executable=codex; binary=node_modules/.bin/codex;;
    claude)
      npm install --prefix "$target.incomplete" --save-exact "@anthropic-ai/claude-code@$version"
      # npm 12 requires explicit approval of lifecycle scripts. Run only this
      # pinned vendor installer, rather than enabling all dependency scripts.
      node "$target.incomplete/node_modules/@anthropic-ai/claude-code/install.cjs"
      executable=claude; binary=node_modules/.bin/claude;;
    cursor)
      fetch "https://downloads.cursor.com/lab/$version/$platform/arm64/agent-cli-package.tar.gz" "$tmp/cursor.tar.gz" "$cursor_sha"
      tar -xzf "$tmp/cursor.tar.gz" --strip-components=1 -C "$target.incomplete"
      executable=agent; binary=cursor-agent;;
    *) exit 1;;
  esac
  touch "$target.incomplete/complete"
  mv "$target.incomplete" "$target"
else
  case "$selected" in codex|claude) executable=$selected; binary=node_modules/.bin/$selected;; cursor) executable=agent; binary=cursor-agent;; esac
fi
ln -sfn "$target/$binary" "$HOME/.local/bin/$executable"
if [ "$selected" = cursor ]; then ln -sfn "$target/cursor-agent" "$HOME/.local/bin/cursor-agent"; fi
# Installation is retained even when the guest credential store needs user
# interaction. The host records verified tools only after native preflight.
if [ "$selected:$platform" = cursor:darwin ]; then
  if "$target/$binary" --version > "$tmp/cursor-version" 2>&1; then
    cat "$tmp/cursor-version"
  else
    if grep -q 'Your macOS login keychain is locked' "$tmp/cursor-version"; then
      printf '%s\n' 'Guest login keychain is locked; unlock it in the guest shell, then log in to Cursor.' >&2
      exit 42
    fi
    cat "$tmp/cursor-version" >&2
    exit 1
  fi
else
  "$target/$binary" --version
fi
{
  uname -a
  python3 --version
  git --version
  tmux -V
  node --version
  npm --version
  "$executable" --version
} > "$base/$selected-$version-inventory.txt"
if [ "$platform" = linux ]; then
  cat /etc/os-release >> "$base/$selected-$version-inventory.txt"
  dpkg-query -W >> "$base/$selected-$version-inventory.txt"
else
  sw_vers >> "$base/$selected-$version-inventory.txt"
  brew list --versions >> "$base/$selected-$version-inventory.txt"
fi
