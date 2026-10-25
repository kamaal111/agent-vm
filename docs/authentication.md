# Authentication and credentials

Configuration contains explicit sources, never secret values. The default source for each agent is **guest**: perform its native login in the persistent VM. No host configuration directory, Keychain entry, shell environment or SSH agent is forwarded into the guest. Managed default-image setup authorizes a dedicated public SSH key through Tart; custom images require manual public-key authorization. Private host SSH keys stay on the host.

All code tests use synthetic data and temporary homes. Development does not inspect personal credential files or run a login flow.

For managed default images, `run` installs the selected agent and prints login instructions if authentication is missing. Complete the login below, exit the shell, and retry using the same `--vm` name. Never repeat `--new` merely to retry login. Explicit config/data paths are included in generated instructions.

## Guest login

```sh
agent-vm shell agent-linux
# In the guest, run only the login for the agent you intend to use:
codex login
codex login status
claude auth login
claude auth status
NO_OPEN_BROWSER=1 agent login
agent status --format json
```

Follow vendor-supported headless instructions. Codex's device-code flow is currently documented as beta and is not automated or required by this release. A supported stable fallback is browser callback forwarding (`ssh -L 1455:localhost:1455` through a connection you explicitly establish) or an explicit file-cache import. Claude can print a login URL/code for SSH use. Cursor supports printing a URL with `NO_OPEN_BROWSER=1`. Actual subscription/account entitlement and Linux/macOS credential-store behavior need user live validation.

### Cursor on headless macOS

The pinned Cursor launcher can refuse even `--version` while the guest login keychain is locked. Managed setup retains the verified archive installation and prints guest-shell instructions to run `security unlock-keychain`, then `NO_OPEN_BROWSER=1 agent login`. Enter the guest keychain password interactively. Retry with the same VM name; no reinstallation or additional setup approval is required. Native version verification completes after the keychain is available. The tool preserves Cursor's native credential-store setting.

## API keys inside guests

An explicitly created `~/.agent-vm/credentials/codex.env`, `claude.env`, or `cursor.env` in the guest is loaded only for that agent's auth check and launch. Files must be regular and mode 0600 or stricter. They are trusted shell input; write assignments only. Parent directories should be 0700. Use an editor or a protected secrets workflow; do not type real keys into command arguments or logs.

- Codex: prefer `codex login --with-api-key` with the key on stdin (for example, from the guest environment using `printenv OPENAI_API_KEY | codex login --with-api-key`). This stores native authentication in the guest. Its interactive API-key behavior is distinct from `CODEX_API_KEY` used for some noninteractive commands.
- Claude: set `ANTHROPIC_API_KEY` in its guest env file. Native approval/billing rules still apply.
- Cursor: set `CURSOR_API_KEY` in its guest env file. The `--api-key` flag is prohibited by this CLI because it exposes secrets in process arguments.

No arbitrary host env-file import is provided. API keys and subscription tokens give the guest and its agents access to that account. The VM is persistent and agents in the same guest account can read that account's credentials and other workspaces. Project copies isolate host edits; they are not isolation between mutually untrusted agents sharing one VM. Use separate VMs/accounts for that boundary.

## Codex file-cache import

[Official Codex authentication guidance](https://learn.chatgpt.com/docs/auth) supports copying an existing file cache for headless access. File-backed login may create `~/.codex/auth.json`; Keychain/keyring-backed login may not. This implementation does not read Keychain or assume a file exists.

If you explicitly choose this method, configure:

```toml
[auth.codex]
source = "file"
file = "/absolute/path/to/explicit/auth.json"
```

Then run `agent-vm auth import-codex --vm agent-linux`. Only that file is read, after validating regular-file status, restrictive permissions, JSON syntax and a 1 MiB limit. Bytes travel over SSH stdin. The guest file is created exclusively with mode 0600; an existing file is a conflict. No token, token hash or copy of the auth cache enters SQLite or command arguments. The code never copies the guest cache back to the host.

Refreshes persist in the VM's native credential store across agent processes and VM stop/start. Never reimport on each launch: doing so could replace refreshed credentials. Multiple sessions in one VM share the harness's native account state and rely on that harness's refresh coordination. Cross-VM imports of the same subscription refresh-token family may conflict at the provider; prefer independent guest logins, and reauthenticate a VM explicitly if refresh fails. No cross-VM credential synchronization or conflict-merging claim is made.

Base images must be prepared in a clean account before login, not sanitized copies of personal development machines. Image preparation checks some common credential paths but cannot prove absence of Keychain or unknown secrets; inspect clean-image provenance and accounts before sealing. Exported workspaces can contain secrets created during development; exports are private files.
