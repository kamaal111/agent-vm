# Releases and Homebrew

Release Please opens a release PR from Conventional Commits on `main`.
Merge it to publish a versioned macOS ARM64 archive and checksums.

Versions advance automatically from the last published release: `fix:` commits
bump the patch version and `feat:` commits bump the minor version. Release Please
updates `version.txt`, `.release-please-manifest.json`, and the changelog in its PR.
Keep `release-as` out of the normal configuration. If used to override a release
version temporarily, remove it after that release PR merges; leaving it set makes
subsequent releases reuse the same version and fail when the tag already exists.

## Setup

In both agent-vm and homebrew-tap, enable **Settings → Actions → General →
Workflow permissions → Allow GitHub Actions to create and approve pull requests**.

## Update Homebrew

Once the Release workflow has finished uploading the archive and checksums, run:

```sh
gh workflow run update-agent-vm.yml --repo kamaal111/homebrew-tap -f version=v0.1.0
```

Review and merge the formula PR it opens. You can also run the updater locally;
see the [tap README](https://github.com/kamaal111/homebrew-tap#update-agent-vm).

Install from the tap:

```sh
brew install kamaal111/tap/agent-vm
```

See [First setup](../README.md#first-setup) for Tart and VM configuration.

## Retry a failed upload

```sh
gh workflow run release.yml --repo kamaal111/agent-vm -f release_tag=v0.1.0
```

This retries missing uploads without replacing existing assets. Once it succeeds,
run the tap updater again.

Recovery builds the existing tag; it does not include newer commits on `main`.
Release new changes through the next Release Please PR, preserving published tags
and assets.
