#!/bin/sh
# Package a checked-out stable release; optionally publish missing assets.
set -eu
cd "$(dirname "$0")/.."
tag=${1:?Usage: sh scripts/release.sh v0.1.0 [--publish]}
mode=${2:-}
test "$mode" = '' || test "$mode" = --publish || { echo 'Unknown option' >&2; exit 1; }
python3 scripts/release.py validate-tag "$tag"
version=${tag#v}
test "$(cat version.txt)" = "$version" || { echo 'Tag must match version.txt' >&2; exit 1; }
test "$(uname -s)/$(uname -m)" = Darwin/arm64 || { echo 'Packaging requires Apple Silicon macOS' >&2; exit 1; }
if test "$mode" = --publish; then
  test "$(git rev-parse HEAD)" = "$(git rev-parse "refs/tags/$tag^{commit}")" || { echo 'Check out the release tag before publishing' >&2; exit 1; }
  test -z "$(git status --porcelain --untracked-files=normal)" || { echo 'Publishing requires a clean checkout' >&2; exit 1; }
  gh release view "$tag" --json isDraft,isPrerelease --jq 'if .isDraft or .isPrerelease then error("Expected published stable release") else . end' >/dev/null
fi
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT HUP INT TERM
CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -trimpath -ldflags "-X agent-vm/internal/cli.Version=$version" -o "$work/agent-vm" ./cmd/agent-vm
test "$("$work/agent-vm" --version)" = "agent-vm version $version"
"$work/agent-vm" --help >/dev/null
mkdir -p bin/release
archive="agent-vm_${version}_darwin_arm64.tar.gz"
python3 scripts/release.py package "$work/agent-vm" "bin/release/$archive"
(cd bin/release && shasum -a 256 "$archive" > checksums.txt)
if test "$mode" = --publish; then
  gh release view "$tag" --json assets --jq '.assets[].name' > "$work/assets"
  # Check every existing asset before uploading anything; never clobber bytes.
  for asset in "$archive" checksums.txt; do
    if grep -Fxq "$asset" "$work/assets"; then
      gh release download "$tag" --pattern "$asset" --dir "$work"
      cmp "bin/release/$asset" "$work/$asset" || { echo "Existing $asset differs; refusing overwrite" >&2; exit 1; }
    fi
  done
  for asset in "$archive" checksums.txt; do
    if ! grep -Fxq "$asset" "$work/assets"; then
      gh release upload "$tag" "bin/release/$asset"
    fi
  done
fi
printf 'Packaged %s\n' "bin/release/$archive"
