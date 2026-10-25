#!/bin/sh
# Explicit manual-image installer; shared pins and installation with managed setup.
set -eu
case "$(uname -m)" in arm64|aarch64) ;; *) echo 'ARM64 guest required' >&2; exit 1;; esac
for tool in git tmux python3 node npm curl tar; do
  command -v "$tool" >/dev/null || { echo "Install prerequisite: $tool" >&2; exit 1; }
done
exec python3 "$(dirname "$0")/install_agents.py"
