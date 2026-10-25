#!/usr/bin/env python3
"""Install pinned agents in a prepared guest using shared bootstrap scripts."""

import json
from pathlib import Path
import platform
import shlex
import subprocess
import sys


def main() -> None:
    root = Path(__file__).resolve().parent.parent / "internal" / "bootstrap"
    pins = json.loads((root / "manifest.json").read_text())
    os_name = {"Linux": "linux", "Darwin": "macos"}[platform.system()]
    prerequisites = [
        (["node", "--version"], "v" + pins["node"]),
        (["npm", "--version"], pins["npm"]),
        (["tmux", "-V"], "tmux " + pins["tmux"]),
    ]
    for command, expected in prerequisites:
        if subprocess.check_output(command, text=True).strip() != expected:
            sys.exit("Prepare pinned prerequisite first: " + expected)

    download = (root / "download.sh").read_text()
    installer = (root / "agent.sh").read_text()
    for selected, version in pins["agents"].items():
        values = {
            "selected": selected,
            "version": version,
            "platform": "darwin" if os_name == "macos" else "linux",
            "cursor_sha": pins["cursor_sha256"][os_name],
        }
        assignments = "".join(
            key + "=" + shlex.quote(value) + "\n"
            for key, value in values.items()
        )
        prefix = (
            "set -eu\n"
            "umask 077\n"
            + assignments
            + 'base="$HOME/.local/share/agent-vm-tools"\n'
            'mkdir -p "$base" "$HOME/.local/bin"\n'
            'export PATH="$HOME/.local/bin:$PATH"\n'
            'tmp=$(mktemp -d)\n'
            'trap \'rm -rf "$tmp"\' EXIT HUP INT TERM\n'
        )
        subprocess.run(
            ["/bin/sh", "-s"],
            input=prefix + download + installer,
            text=True,
            check=True,
        )


if __name__ == "__main__":
    main()
