# Opt-in real-VM validation

These checks **have not run** in this environment because Tart and prepared images are unavailable. They may download large images only when you explicitly choose an OCI base. Use disposable test VMs and synthetic projects. Do not run personal logins during automated development tests.

## Prerequisites

- Apple Silicon Mac with verified Tart 2.41.1, system OpenSSH >=10.3 (major 10), Git 2.x, adequate storage/CPU/RAM.
- Separate pinned Linux/macOS ARM64 prepared images and manifests from base-images.md, with tmux 3.8 and the exact supported harness versions.
- Dedicated SSH identity/public authorization, independently verified per-clone host-key fingerprints.
- A temporary config/data directory; explicit vendor test account/API key if you choose to validate actual model calls. Credential checks can first be exercised as missing-auth errors without an account.

## Per-OS lifecycle

Set `--config /tmp/your-config.toml --data-dir /tmp/your-private-state` on every invocation (or use the default host registry deliberately).

1. Create `integration-linux --os linux`; inspect Tart JSON and registry identity/source/provision fields. Start twice; verify there is one VM, no extra clone and no second Tart run process.
2. Read the guest host fingerprint through its console and run `trust` with the exact fingerprint. Incorrect fingerprint must fail without changing known_hosts. Run `provision`, then `shell`; verify OS and ARM64.
3. Stop; inspect that disk still exists. Start again and verify guest files persist. Test too-small disk/resource settings and missing configured image: failures must retain actionable creation state without deleting unrelated VMs.
4. Repeat for `integration-macos --os macos`. Try Linux VM with `--os macos`, `--vm` with `--new`, and a collision with an unrelated Tart VM; all must fail before unintended mutations.
5. Run concurrent starts from two terminals and cancel a boot; verify locks release, one start succeeds, and VM disk is preserved. Remove a test VM manually only to test missing metadata reconciliation; the tool must refuse replacement of a previously complete VM.

## Per-harness × per-OS conversations

For **all six combinations**, record exact OS build, image digest, harness `--version`, tmux version and SSH version in the results.

1. Check native help for the pinned version, authenticate explicitly with a disposable account **inside that VM**, and verify auth status. Verify subscription login separately from API-key mode; do not infer one from the other.
2. Create a synthetic Git repo/worktree with modified/deleted/tracked binary/untracked/ignored files and a fake `.env` secret. `run AGENT --vm NAME --name unique-name --prompt 'A prompt with spaces, quotes and newlines' -- --model VALID_MODEL` must preserve argv and show the native UI directly. Check that the prompt delimiter `--` is accepted by the exact vendor parser.
3. Inspect the guest project copy, `.git` directory and baseline; `.git` must not point to the host. Confirm ignored/secret exclusions and host nonmodification. Open a second session/project in the same VM and verify different directories.
4. Disconnect SSH/tab while the agent is live. `sessions show --live` must reconcile state and discover the correct history only when its supported layout exists. `attach` must use the same process. Confirm terminal resizing and Ctrl-C behavior, tmux `Ctrl-b d`, and clean terminal state after return.
5. Finish the native conversation. Record native ID/history location. If automated discovery is unsupported (Cursor or Codex paginated layout), bind the exact ID and existing guest history file explicitly. `resume` must use that ID, continue the saved chat and preserve existing guest edits despite changes to host files.
6. Try two simultaneous resumes; verify only one process launches. Remove/rename a disposable workspace/history file; resume must fail without a new conversation or host file transfer. Restore it explicitly.
7. Verify native success/nonzero exit status, normal tmux detach, SSH status 255/network loss, canceled provisioning, invalid auth and missing executable/version errors remain distinct and actionable.
8. Rename the tool session; its UUID must still resolve. Picker filtering/arrows/Enter/Escape must work in narrow Herdr tabs, with no dashboard wrapping agent output.

## Export, auth conflicts and cleanup

Make committed and uncommitted text/binary/deletion/untracked changes in the guest. Exit the managed process and export to a new archive. Extract into a separate review directory and verify all changes/status/baseline while the source host stays untouched. Reusing an output path must fail. An active process must block export.

For Codex file transfer, use synthetic auth JSON first to verify exclusive guest creation and an existing-file conflict. With a disposable real account, explicitly observe native token refresh persistence inside the VM and concurrent sessions. No host copy-back occurs. Test missing host auth.json/Keychain-only sources as an actionable limitation; never copy an entire config directory.

Stop each test VM. Delete must list affected sessions and require confirmation or `--yes`; running VMs must be stopped first. Delete only the named owned test VMs. Verify unrelated Tart VMs/images remain and deleted histories cannot be resumed. Preserve the results and explicit failures; do not mark this matrix tested based on fake-command or local tmux tests.
