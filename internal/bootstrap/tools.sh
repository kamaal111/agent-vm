# Runs only in the guest account. All downloaded archives are checked before use.
umask 077
export PATH="$HOME/.local/bin:/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin"
case "$platform:$(uname -s):$(uname -m)" in linux:Linux:aarch64|darwin:Darwin:arm64) ;; *) exit 1;; esac
base="$HOME/.local/share/agent-vm-tools"
mkdir -p "$base" "$HOME/.local/bin"
tmp=$(mktemp -d "$base/install.XXXXXX")
trap 'rm -rf "$tmp"' EXIT HUP INT TERM
if [ ! -f "$base/prerequisites-$revision.complete" ]; then
  if [ "$platform" = linux ]; then
    unicode=--disable-utf8proc
    allocator=--disable-jemalloc
    sudo -n apt-get update
    sudo -n env DEBIAN_FRONTEND=noninteractive apt-get install -y ca-certificates curl python3 git build-essential pkg-config libevent-dev libncurses-dev bison
  else
    test -x /opt/homebrew/bin/brew || { echo 'Default macOS image is missing Homebrew' >&2; exit 1; }
    HOMEBREW_NO_AUTO_UPDATE=1 brew install python git pkgconf libevent ncurses bison utf8proc jemalloc
    unicode=--enable-utf8proc
    allocator=--enable-jemalloc
    export PATH="/opt/homebrew/opt/bison/bin:$PATH"
    export PKG_CONFIG_PATH="/opt/homebrew/opt/libevent/lib/pkgconfig:/opt/homebrew/opt/ncurses/lib/pkgconfig:/opt/homebrew/opt/utf8proc/lib/pkgconfig:/opt/homebrew/opt/jemalloc/lib/pkgconfig"
    export CPPFLAGS="-I/opt/homebrew/include"
    export LDFLAGS="-L/opt/homebrew/lib"
  fi
  node_dir="$base/node-$node_version"
  if [ ! -x "$node_dir/bin/node" ]; then
    fetch "https://nodejs.org/dist/v$node_version/node-v$node_version-$platform-arm64.tar.gz" "$tmp/node.tar.gz" "$node_sha"
    mkdir "$tmp/node"
    tar -xzf "$tmp/node.tar.gz" --strip-components=1 -C "$tmp/node"
    rm -rf "$node_dir.incomplete"
    mv "$tmp/node" "$node_dir.incomplete"
    mv "$node_dir.incomplete" "$node_dir"
  fi
  export PATH="$node_dir/bin:$PATH"
  test "$(node --version)" = "v$node_version"
  # Pin npm independently; Node's bundled npm may be a different supported release.
  if [ "$(npm --version)" != "$npm_version" ]; then npm install --global "npm@$npm_version"; fi
  test "$(npm --version)" = "$npm_version"
  for tool in node npm npx; do ln -sfn "$node_dir/bin/$tool" "$HOME/.local/bin/$tool"; done
  tmux_dir="$base/tmux-$tmux_version"
  if [ ! -x "$tmux_dir/bin/tmux" ]; then
    fetch "https://github.com/tmux/tmux/releases/download/$tmux_version/tmux-$tmux_version.tar.gz" "$tmp/tmux.tar.gz" "$tmux_sha"
    tar -xzf "$tmp/tmux.tar.gz" -C "$tmp"
    (cd "$tmp/tmux-$tmux_version"; ./configure --prefix="$tmux_dir" "$unicode" "$allocator"; make -j2; make install DESTDIR="$tmp/stage")
    mv "$tmp/stage$tmux_dir" "$tmux_dir"
  fi
  ln -sfn "$tmux_dir/bin/tmux" "$HOME/.local/bin/tmux"
  test "$(tmux -V)" = "tmux $tmux_version"
fi
# Adding another agent reuses existing prerequisites without package upgrades.
test "$(node --version)" = "v$node_version"
test "$(npm --version)" = "$npm_version"
test "$(tmux -V)" = "tmux $tmux_version"
command -v python3 >/dev/null
command -v git >/dev/null
test -f "$base/prerequisites-$revision.complete" || touch "$base/prerequisites-$revision.complete"
