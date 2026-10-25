fetch() {
  curl --disable --fail --location --retry 3 --connect-timeout 20 --proto '=https' --proto-redir '=https' "$1" -o "$2"
  printf '%s  %s\n' "$3" "$2" > "$tmp/checksum"
  if command -v sha256sum >/dev/null; then sha256sum -c "$tmp/checksum"; else shasum -a 256 -c "$tmp/checksum"; fi
}
