import os
from pathlib import Path
import subprocess
import sys
import tarfile

import pytest

import release


@pytest.fixture
def binary(tmp_path):
    path = tmp_path / "binary"
    path.write_bytes(b"agent-vm binary\x00fixture")
    return path


def test_archive_contains_only_executable_with_original_bytes(binary, tmp_path):
    destination = tmp_path / "release.tar.gz"
    release.package_archive(binary, destination)
    with tarfile.open(destination) as archive:
        assert archive.getnames() == ["agent-vm"]
        assert archive.getmember("agent-vm").mode == 0o755
        assert archive.extractfile("agent-vm").read() == binary.read_bytes()


def test_archive_ignores_input_metadata_and_output_filename(binary, tmp_path):
    first = tmp_path / "first.tar.gz"
    second = tmp_path / "second.tar.gz"
    release.package_archive(binary, first)
    os.utime(binary, (1234567890, 1234567890))
    binary.chmod(0o600)
    release.package_archive(binary, second)
    assert first.read_bytes() == second.read_bytes()


def test_cli_packages_archive(binary, tmp_path):
    destination = tmp_path / "cli.tar.gz"
    result = subprocess.run(
        [
            sys.executable,
            str(Path(release.__file__)),
            "package",
            str(binary),
            str(destination),
        ],
        capture_output=True,
        text=True,
    )
    assert result.returncode == 0, result.stderr
    with tarfile.open(destination) as archive:
        assert archive.getnames() == ["agent-vm"]


def test_cli_accepts_stable_tag():
    result = subprocess.run(
        [sys.executable, str(Path(release.__file__)), "validate-tag", "v0.1.0"],
        capture_output=True,
        text=True,
    )
    assert result.returncode == 0, result.stderr


def test_cli_rejects_prerelease_even_with_python_optimization():
    result = subprocess.run(
        [
            sys.executable,
            "-O",
            str(Path(release.__file__)),
            "validate-tag",
            "v0.1.0-rc.1",
        ],
        capture_output=True,
        text=True,
    )
    assert result.returncode == 1
    assert "Expected a stable vX.Y.Z tag" in result.stderr


def test_rejects_leading_zero_in_tag():
    with pytest.raises(ValueError):
        release.validate_tag("v01.0.0")
