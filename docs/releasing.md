# Releasing

This document covers how to release new versions of `rr`.

## Release process

Releases are automated via GitHub Actions and GoReleaser. When you push a tag, the CI:

1. Builds binaries for all platforms (Linux, macOS, Windows on amd64/arm64)
2. Creates a GitHub release with changelogs
3. Publishes the Homebrew formula to the tap repository
4. Generates shell completions and includes them in archives

### Creating a release

```bash
# Tag the release
git tag v1.2.3
git push origin v1.2.3
```

The rest happens automatically.

## Required secrets

### HOMEBREW_TAP_TOKEN

GoReleaser pushes the Homebrew formula to `rileyhilliard/homebrew-tap`. This requires a Personal Access Token with write access.

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
   - GoReleaser will create/update the formula file automatically

### Other secrets

| Secret | Purpose |
|--------|---------|
| `GITHUB_TOKEN` | Provided automatically by GitHub Actions. Used for creating releases. |

## Homebrew tap repository

The tap repository (`rileyhilliard/homebrew-tap`) is managed automatically by GoReleaser. You don't need to manually edit the formula file.

After a release, users can install with:

```bash
brew install rileyhilliard/tap/rr
```

## Troubleshooting

### Release failed to push formula

Check that:
- `HOMEBREW_TAP_TOKEN` secret is set correctly
- The token has `repo` or `public_repo` scope
- The `homebrew-tap` repository exists under the correct owner

### Formula test failed

The Homebrew formula runs `rr --version` as a test. If this fails:
- Check that the binary runs without errors
- Verify version information is being set correctly via ldflags

## Version numbering

Follow semantic versioning:
- `v1.0.0` - Major release (breaking changes)
- `v1.1.0` - Minor release (new features, backward compatible)
- `v1.1.1` - Patch release (bug fixes)
