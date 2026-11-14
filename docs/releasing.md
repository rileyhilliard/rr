# Releasing

This document covers how to release new versions of `rr`.

## Release process

Releases are automated via GitHub Actions (`.github/workflows/release.yml`) and GoReleaser (`.goreleaser.yaml`). When you push a `v*` tag, the workflow:

1. Builds binaries for Linux, macOS, and Windows on amd64/arm64, with the Go version from `go.mod`
2. Packages them as `rr_<os>_<arch>.tar.gz` (`.zip` on Windows) with `README.md`, `LICENSE`, and the committed `completions/` directory, plus `checksums.txt`
3. Creates a GitHub release whose notes GoReleaser generates from commit messages (`docs:`, `test:`, and `chore:` commits are left out)
4. Publishes the Homebrew cask to the tap repository

Completions are not generated during the release. If commands or flags changed, run `make completions` and commit the result before tagging.

### Creating a release

```bash
# Tag the release
git tag v1.2.3
git push origin v1.2.3
```

The rest happens automatically. CHANGELOG.md is maintained by hand in [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) format; add the version's entry through a PR, since `main` only accepts PR merges. Breaking changes also go in [MIGRATION.md](MIGRATION.md).

The `/merge-release` Claude Code skill (`.claude/skills/merge-release/SKILL.md`) adds the changelog entry to the feature PR, merges it, and tags main.

To check the GoReleaser config locally without publishing:

```bash
goreleaser release --snapshot --clean
```

## Required secrets

### HOMEBREW_TAP_TOKEN

GoReleaser pushes the Homebrew cask to `rileyhilliard/homebrew-tap`. This requires a Personal Access Token with write access.

**Setup steps:**

1. Create a GitHub Personal Access Token (classic) at https://github.com/settings/tokens
   - Scopes needed: `repo` (full control of private repositories)
   - If the tap repo is public, `public_repo` scope is sufficient

2. Add the token as a repository secret in the main `rr` repo:
   - Go to Settings > Secrets and variables > Actions
   - Click "New repository secret"
   - Name: `HOMEBREW_TAP_TOKEN`
   - Value: Your personal access token

3. Create the tap repository if it doesn't exist:
   - Repository name must be `homebrew-tap`
   - Can be public or private
   - GoReleaser will create/update the cask file automatically

### Other secrets

| Secret | Purpose |
|--------|---------|
| `GITHUB_TOKEN` | Provided automatically by GitHub Actions. Used for creating releases. |

## Homebrew tap repository

The tap repository (`rileyhilliard/homebrew-tap`) is managed automatically by GoReleaser (the `homebrew_casks` section of `.goreleaser.yaml`). You don't need to manually edit the cask file. On macOS the cask's post-install hook removes the quarantine attribute from the `rr` binary.

After a release, users can install with:

```bash
brew install rileyhilliard/tap/rr
```

## Troubleshooting

### Release failed to push the cask

Check that:
- `HOMEBREW_TAP_TOKEN` secret is set correctly
- The token has `repo` or `public_repo` scope
- The `homebrew-tap` repository exists under the correct owner

### Wrong version reported

`rr version` prints the version, commit, and build date, which GoReleaser sets via ldflags (`main.version`, `main.commit`, `main.date`). A local `make build` reports `rr dev`. If a released binary shows `dev`, check the `ldflags` in `.goreleaser.yaml` against the variables in `cmd/rr/main.go`.

## Version numbering

Follow semantic versioning:
- `v1.0.0` - Major release (breaking changes)
- `v1.1.0` - Minor release (new features, backward compatible)
- `v1.1.1` - Patch release (bug fixes)

rr is still pre-1.0, so breaking changes ship in minor releases (v0.21.0, v0.23.0, and v0.24.0 all had them). Call them out under `### Breaking Changes` in CHANGELOG.md.
