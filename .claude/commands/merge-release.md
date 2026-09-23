# Merge and Release

Automates the full release workflow: PR creation, merge, changelog update, and tagging. GoReleaser creates the GitHub release from the tag.

## Arguments
- `$ARGUMENTS`: Optional version bump type (patch|minor|major). Defaults to "patch".

## Pre-flight Checks

Before starting, verify:

1. **Working directory state**: Check for uncommitted changes
   ```bash
   git status --porcelain
   ```
   - If changes exist and no recent commit in this conversation, STOP and ask user to commit first
   - If we just made a commit in this conversation, proceed

2. **Branch state**: Determine current branch and unpushed commits
   ```bash
   git branch --show-current
   git log origin/main..HEAD --oneline 2>/dev/null || git log origin/$(git branch --show-current)..HEAD --oneline
   ```
   - If no unpushed commits, STOP and inform user there's nothing to release

3. **Get latest tags for version calculation**
   ```bash
   git tag --sort=-v:refname | head -5
   ```

4. **Verify tests pass** (skip if user explicitly says to skip)
   ```bash
   make test
   ```
   - If tests fail, STOP and report failures

## Execution Flow

### Step 1: Create and Push PR

If on main with unpushed commits:
```bash
# Create feature branch from commit message
BRANCH_NAME=$(git log -1 --format=%s | sed 's/[^a-zA-Z0-9]/-/g' | tr '[:upper:]' '[:lower:]' | cut -c1-50)
git checkout -b "release/${BRANCH_NAME}"
git push -u origin "release/${BRANCH_NAME}"
```

Create PR:
```bash
# Get commit message for PR title
TITLE=$(git log -1 --format=%s)
gh pr create --title "$TITLE" --body "$(cat <<'EOF'
## Summary
Auto-generated release PR.

## Changes
See commit history for details.
EOF
)"
```

### Step 2: Merge PR

```bash
# Get PR number from current branch
PR_NUMBER=$(gh pr view --json number -q .number)
gh pr merge $PR_NUMBER --squash --delete-branch
```

If merge fails, try rebase:
```bash
gh pr merge $PR_NUMBER --rebase --delete-branch
```

### Step 3: Sync Local Main

```bash
git checkout main
git fetch origin
git reset --hard origin/main
```

### Step 4: Calculate Next Version

Parse current version and bump accordingly:
- `patch` (default): v0.4.6 -> v0.4.7
- `minor`: v0.4.6 -> v0.5.0
- `major`: v0.4.6 -> v1.0.0

```bash
LATEST_TAG=$(git tag --sort=-v:refname | head -1)
# Parse and increment based on $ARGUMENTS or default to patch
```

### Step 5: Update CHANGELOG.md

Write the changelog before tagging, so the tagged commit includes its own entry.

Read existing changelog and insert new version entry at the top (after header). If an `## [Unreleased]` section exists, rename it to the new version instead of writing the entry from scratch.

Get changes for this version:
```bash
git log $PREVIOUS_TAG..HEAD --format="%s" --reverse
```

Categorize commits by conventional commit type:
- `feat:` -> Added
- `fix:` -> Fixed
- `docs:` -> Documentation (skip unless significant)
- `perf:` -> Performance
- `refactor:` -> Changed
- `BREAKING CHANGE` -> include breaking change notice

Insert new section following Keep a Changelog format:
```markdown
## [$VERSION] - $DATE

### Added
- New features...

### Fixed
- Bug fixes...

### Changed
- Other changes...
```

Breaking changes also need an entry in `docs/MIGRATION.md`.

### Step 6: Commit and Merge Changelog

**Important**: Most repos have branch protection rules. Always create a PR for the changelog instead of pushing directly to main.

```bash
# Create changelog branch
git checkout -b "docs/changelog-$NEW_TAG"
git add CHANGELOG.md
git commit -m "docs: update changelog for $NEW_TAG"
git push -u origin "docs/changelog-$NEW_TAG"

# Create and merge PR
gh pr create --title "docs: update changelog for $NEW_TAG" --body "Update CHANGELOG.md for $NEW_TAG release."
PR_NUMBER=$(gh pr view --json number -q .number)
gh pr merge $PR_NUMBER --squash --delete-branch

# Sync local main
git checkout main
git fetch origin
git reset --hard origin/main
```

### Step 7: Create and Push Tag

Tag main after the changelog PR has merged:

```bash
COMMIT_MSG=$(git log -1 --format=%s)
git tag -a $NEW_TAG -m "$COMMIT_MSG"
git push origin $NEW_TAG
```

### Step 8: Confirm the GitHub Release

Don't run `gh release create`. Pushing a `v*` tag triggers `.github/workflows/release.yml`, and GoReleaser creates the GitHub release, uploads the binaries, and publishes the Homebrew cask. A manual `gh release create` for the same tag collides with it.

Watch the workflow and get the release URL once it finishes:
```bash
gh run list --workflow=release.yml --limit 1
gh run watch $(gh run list --workflow=release.yml --limit 1 --json databaseId -q '.[0].databaseId')
gh release view $NEW_TAG --json url -q .url
```

## Error Handling

- **PR merge fails**: Report error, don't proceed with tagging
- **Tag push fails**: Report error, suggest manual intervention
- **Changelog update fails**: Nothing is tagged yet; report the error and don't tag
- **Release workflow fails**: The tag is pushed; report the failed run and suggest re-running it with `gh run rerun`
- **Any step fails**: Report which step failed and current state

## Output

On success, report:
- PR URL
- New tag version
- Release URL
- Changelog commit SHA

## Example Usage

```
/merge-release           # patch bump (default)
/merge-release patch     # explicit patch bump
/merge-release minor     # minor version bump
/merge-release major     # major version bump
```
