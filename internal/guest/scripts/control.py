"""Short-lived SSH helper. No service, terminal scraping, or credential output."""
import contextlib
import fcntl
import glob
import json
import os
import platform
from pathlib import Path
import re
import shlex
import shutil
import subprocess
import sys
import stat

ROOT = Path.home() / ".agent-vm"

def run(args, **kw):
    return subprocess.run(args, check=True, stdout=subprocess.PIPE, stderr=subprocess.DEVNULL, **kw).stdout.decode().strip()

def private_write(path, value):
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
    with os.fdopen(fd, "w") as f:
        f.write(value)

def session_paths(r):
    sid = r["id"]
    if not re.fullmatch(r"[0-9a-f-]{36}", sid):
        raise ValueError("invalid managed session ID")
    d = ROOT / "processes" / sid
    d.mkdir(parents=True, exist_ok=True, mode=0o700)
    return d, "avm-" + sid

def credential_prefix(agent):
    if agent not in ("codex", "claude", "cursor"):
        raise ValueError("unknown credential source")
    path = ROOT / "credentials" / (agent + ".env")
    if path.exists() or path.is_symlink():
        mode = path.lstat().st_mode
        if not stat.S_ISREG(mode) or mode & 0o077:
            raise ValueError("guest credential env file must be regular with mode 0600 or stricter")
        return "set -a; . " + shlex.quote(str(path)) + "; set +a; "
    return ""

def process_state(r):
    d, name = session_paths(r)
    if (d / "exit").exists():
        return {"status": "exited", "exit_code": int((d / "exit").read_text())}
    p = subprocess.run(["tmux", "-L", "agent-vm", "list-panes", "-t", name, "-F", "#{pane_dead}"], stdout=subprocess.PIPE, stderr=subprocess.DEVNULL)
    if p.returncode == 0 and p.stdout.strip() == b"0":
        return {"status": "running"}
    return {"status": "missing"}

def discover(r):
    workspace = str(Path(r["workspace"]).resolve())
    candidates = set()
    if r["agent"] == "codex":
        files = (Path.home() / ".codex" / "sessions").rglob("*.jsonl")
    elif r["agent"] == "claude":
        files = (Path.home() / ".claude" / "projects").rglob("*.jsonl")
    else:
        return {"native_id": "", "history": "", "limitation": "Cursor has no verified structured local-history lookup; use sessions bind with its native ID and an existing history file"}
    for file in files:
        try:
            with file.open() as f:
                for _ in range(64):
                    line = f.readline(1024 * 1024)
                    if not line:
                        break
                    v = json.loads(line)
                    if r["agent"] == "codex" and v.get("type") == "session_meta":
                        m = v.get("payload", {})
                        if m.get("cwd") == workspace and m.get("source") in ("cli", "vscode", None):
                            candidates.add((m["id"], str(file)))
                    if r["agent"] == "claude" and v.get("cwd") == workspace and v.get("sessionId"):
                        candidates.add((v["sessionId"], str(file)))
        except (OSError, ValueError, KeyError):
            continue
    if len(candidates) != 1:
        return {"native_id": "", "history": "", "limitation": "Native history missing, ambiguous, or in an unsupported layout; bind the exact native ID and history file explicitly"}
    native, history = candidates.pop()
    return {"native_id": native, "history": history}

def main(op, r):
    ROOT.mkdir(parents=True, exist_ok=True, mode=0o700)
    if op == "check":
        for executable in ["git", "tmux", "python3", r["executable"]]:
            if not shutil.which(executable):
                raise ValueError("missing executable " + executable + "; prepare/rebuild the configured base image")
        if r["executable"] == "agent" and platform.system() == "Darwin":
            version = subprocess.run(["agent", "--version"], stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=30)
            if version.returncode != 0:
                if b"Your macOS login keychain is locked" in version.stdout + version.stderr:
                    return {"keychain_locked": True}
                raise ValueError("Cursor version check failed; use the guest shell to inspect login")
            agent_version = version.stdout.decode().strip()
        else:
            agent_version = run([r["executable"], "--version"])
        return {"tmux": run(["tmux", "-V"]), "agent": agent_version}
    if op == "auth":
        commands = {"codex": ["codex", "login", "status"], "claude": ["claude", "auth", "status"], "cursor": ["agent", "status", "--format", "json"]}
        command = credential_prefix(r["agent"]) + "exec " + shlex.join(commands[r["agent"]])
        result = subprocess.run(["sh", "-c", command], stdout=subprocess.PIPE if r["agent"] == "cursor" else subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=30)
        if r["agent"] == "cursor":
            try:
                status = json.loads(result.stdout)
            except (ValueError, TypeError):
                raise ValueError("Cursor auth status was not valid JSON; use the guest shell to inspect login")
            authenticated = status.get("isAuthenticated") if isinstance(status, dict) else None
            if not isinstance(authenticated, bool):
                raise ValueError("Cursor auth status has an unsupported schema; use the guest shell to inspect login")
            return {"authenticated": authenticated, "verified": True}
        return {"authenticated": result.returncode == 0, "verified": True}
    if op == "home":
        return {"home": str(Path.home())}
    if op == "provision":
        expected = {"linux": "Linux", "macos": "Darwin"}[r["os"]]
        if platform.system() != expected or platform.machine() not in ("arm64", "aarch64"):
            raise ValueError("guest OS/architecture differs from the configured ARM64 image")
        for tool in ("git", "tmux", "python3"):
            if not shutil.which(tool):
                raise ValueError("missing " + tool + "; run the image preparation workflow")
        for part in ("workspaces", "processes", "credentials"):
            (ROOT / part).mkdir(exist_ok=True, mode=0o700)
        tmux = run(["tmux", "-V"])
        private_write(ROOT / "provision.json", json.dumps({"version": r["version"], "image": r["image"], "os": r["os"]}))
        return {"tmux": tmux}
    if op == "state":
        return process_state(r)
    if op == "discover":
        return discover(r)
    if op == "history":
        if not Path(r["workspace"]).is_dir() or not (Path(r["workspace"]) / ".git").is_dir():
            raise ValueError("guest workspace is missing; resume will not copy host files")
        if not r.get("native_id") or not r.get("history") or not Path(r["history"]).is_file():
            raise ValueError("native conversation history is missing; refusing to start a fresh conversation")
        return {"ok": True}
    if op == "cursor-create":
        command = credential_prefix("cursor") + "exec " + shlex.join([r["executable"], "create-chat"])
        native = run(["sh", "-c", command], cwd=r["workspace"])
        if not re.fullmatch(r"[a-zA-Z0-9_-]{8,100}", native):
            raise ValueError("unrecognized Cursor create-chat ID response")
        return {"native_id": native}
    if op == "launch":
        d, name = session_paths(r)
        with (d / "lock").open("a") as lock:
            fcntl.flock(lock, fcntl.LOCK_EX)
            if process_state(r)["status"] == "running":
                return {"status": "running"}
            workspace = Path(r["workspace"])
            if not workspace.is_dir():
                raise ValueError("guest workspace missing")
            subprocess.run(["tmux", "-L", "agent-vm", "kill-session", "-t", name], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
            with contextlib.suppress(FileNotFoundError):
                (d / "exit").unlink()
            command = credential_prefix(r["agent"]) + shlex.join(r["argv"])
            script = "#!/bin/sh\numask 077\n" + command + "\ncode=$?\nprintf '%s\\n' \"$code\" > " + shlex.quote(str(d / "exit.tmp")) + "\nmv " + shlex.quote(str(d / "exit.tmp")) + " " + shlex.quote(str(d / "exit")) + "\nexit \"$code\"\n"
            private_write(d / "run.sh", script)
            run(["tmux", "-L", "agent-vm", "-f", "/dev/null", "new-session", "-d", "-s", name, "-c", str(workspace), "sh", str(d / "run.sh")])
            return process_state(r)
    raise ValueError("unknown guest operation")

try:
    os.umask(0o077)
    print(json.dumps(main(sys.argv[1], json.load(sys.stdin))))
except Exception as exc:
    if isinstance(exc, subprocess.TimeoutExpired):
        print(json.dumps({"error": "guest command timed out; use shell to inspect installation/authentication"}))
    elif isinstance(exc, subprocess.CalledProcessError):
        print(json.dumps({"error": "guest command failed with status " + str(exc.returncode) + "; use shell to inspect installation/authentication"}))
    else:
        print(json.dumps({"error": str(exc)}))
