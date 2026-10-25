#!/usr/bin/env python3
"""Validate release tags and create reproducible agent-vm archives."""

import argparse
import gzip
import io
from pathlib import Path
import re
import tarfile


def validate_tag(tag: str) -> None:
    if not re.fullmatch(r"v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)", tag):
        raise ValueError("Expected a stable vX.Y.Z tag")


def package_archive(binary_path: Path, archive_path: Path) -> None:
    """Normalize metadata so publication retries produce identical bytes."""
    binary = binary_path.read_bytes()
    with archive_path.open("wb") as output:
        with gzip.GzipFile(fileobj=output, mode="wb", filename="", mtime=0) as compressed:
            with tarfile.open(fileobj=compressed, mode="w") as archive:
                info = tarfile.TarInfo("agent-vm")
                info.size = len(binary)
                info.mode = 0o755
                archive.addfile(info, io.BytesIO(binary))


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    commands = parser.add_subparsers(dest="command", required=True)
    validate = commands.add_parser("validate-tag", help="Require a stable vX.Y.Z tag")
    validate.add_argument("tag")
    package = commands.add_parser("package", help="Create a reproducible tar.gz archive")
    package.add_argument("binary", type=Path)
    package.add_argument("archive", type=Path)
    args = parser.parse_args()
    try:
        if args.command == "validate-tag":
            validate_tag(args.tag)
        else:
            package_archive(args.binary, args.archive)
    except (ValueError, OSError) as error:
        parser.exit(1, f"Release helper failed: {error}\n")


if __name__ == "__main__":
    main()
