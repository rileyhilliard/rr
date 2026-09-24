---
name: merge-release
description: Merge the current change to main and tag a release. Puts the changelog entry in the feature PR, squash-merges it, tags main, and watches GoReleaser publish the release.
argument-hint: "[patch|minor|major]"
disable-model-invocation: true
---

# Merge and Release

Ships the current change as a release: the changelog entry goes into the feature PR, the PR is squash-merged, and main is tagged. GoReleaser creates the GitHub release from the tag.

## Arguments

- `$ARGUMENTS`: version bump type (`patch`, `minor`, or `major`). Defaults to `patch`.

## Pre-flight Checks

1. **Working directory state**
   ```bash
   git status --porcelain
   ```
   If there are uncommitted changes that weren't just committed in this conversation, stop and ask the user to commit first.

2. **Branch state**
   ```bash
   git fetch origin
   git branch --show-current
   git log origin/main..HEAD --oneline
   ```
   If there's nothing ahead of `origin/main` and no open PR for this branch, check whether the change is already merged. If it is, use the fallback flow at the end. Otherwise stop: there's nothing to release.

3. **Latest tags**, for the version calculation
   ```bash
   git tag --sort=-v:refname | head -5
   ```

4. **Tests pass** (skip only if the user says so)
   ```bash
   make test
   ```
   If they fail, stop and report the failures.

## Flow

### Step 1: Get onto a feature branch

If you're on main with unpushed commits, move them to a branch and point local main back at `origin/main`. `git branch -f` is safe here because main isn't checked out after the switch:

```bash
BRANCH_NAME=$(git log -1 --format=%s | sed 's/[^a-zA-Z0-9]/-/g' | tr '[:upper:]' '[:lower:]' | cut -c1-50)
git switch -c "release/${BRANCH_NAME}"
git branch -f main origin/main
```

Don't use `git reset --hard`; `.claude/settings.json` denies it.

### Step 2: Calculate the next version

- `patch` (default): v0.4.6 -> v0.4.7
- `minor`: v0.4.6 -> v0.5.0
- `major`: v0.4.6 -> v1.0.0

```bash
LATEST_TAG=$(git tag --sort=-v:refname | head -1)
# Bump per $ARGUMENTS (default patch) to get NEW_TAG, e.g. v0.27.2
```

### Step 3: Write the changelog entry in the feature branch

The tagged commit should include its own changelog entry, so the entry ships in the same PR as the change.

- If `CHANGELOG.md` has an `## [Unreleased]` section, rename it to `## [X.Y.Z] - YYYY-MM-DD` (no `v` in the heading).
- Otherwise, write a new section at the top, under the header, from the commits since the last tag:
  ```bash
  git log $LATEST_TAG..HEAD --format="%s" --reverse
  ```
  Group by conventional commit type, in Keep a Changelog format:
  - `feat:` -> Added
  - `fix:` -> Fixed
  - `perf:` -> Performance
  - `refactor:` -> Changed
  - `docs:` -> skip unless significant
  - `BREAKING CHANGE` -> Breaking Changes section

Breaking changes also need an entry in `docs/MIGRATION.md`.

```bash
git add CHANGELOG.md docs/MIGRATION.md
git commit -m "docs: changelog for $NEW_TAG"
```

### Step 4: Push and open the PR

```bash
git push -u origin HEAD
gh pr create --title "$(git log --reverse --format=%s origin/main..HEAD | head -1)" --body "..."
```

If a PR is already open for the branch, just push. Write a real PR body: what changed and why, not "auto-generated".

### Step 5: Wait for CI and review

```bash
PR_NUMBER=$(gh pr view --json number -q .number)
./scripts/pr-wait.sh "$PR_NUMBER"
```

It waits for every check, then prints unresolved CodeRabbit threads and any "review skipped" notice. If a check failed, stop and report it. If there are unresolved threads, show them to the user and ask before merging. It fails right away if the PR's base isn't `main`, because CI doesn't run there.

### Step 6: Squash-merge and sync main

```bash
gh pr merge "$PR_NUMBER" --squash --delete-branch
git switch main
git pull --ff-only
```

If `git pull --ff-only` refuses, local main has commits that aren't on `origin/main`. Stop and ask the user; don't force it.

### Step 7: Tag main and push the tag

Check that `CHANGELOG.md` on main has the `$NEW_TAG` heading, then:

```bash
git tag -a "$NEW_TAG" -m "$(git log -1 --format=%s)"
git push origin "$NEW_TAG"
```

### Step 8: Confirm the GitHub release

Don't run `gh release create`. Pushing a `v*` tag triggers `.github/workflows/release.yml`, and GoReleaser creates the release, uploads the binaries, and publishes the Homebrew cask. A manual `gh release create` for the same tag collides with it.

GitHub can take a few seconds to register the run, and until then the newest run is the previous release, so wait for the run whose `headBranch` is the new tag:

```bash
RUN_ID=""
for _ in $(seq 1 30); do
  RUN_ID=$(gh run list --workflow=release.yml --limit 10 --json databaseId,headBranch \
    -q ".[] | select(.headBranch == \"$NEW_TAG\") | .databaseId" | head -1)
  [ -n "$RUN_ID" ] && break
  sleep 5
done
gh run watch "$RUN_ID" --exit-status
gh release view "$NEW_TAG" --json url -q .url
```

## Fallback: the change is already merged

Use this only when the change reached main without a changelog entry. Branch protection blocks pushing to main, so the entry goes through its own PR:

```bash
git switch main
git pull --ff-only
git switch -c "docs/changelog-$NEW_TAG"
# Write the entry as in Step 3
git add CHANGELOG.md
git commit -m "docs: update changelog for $NEW_TAG"
git push -u origin HEAD
gh pr create --title "docs: update changelog for $NEW_TAG" --body "Changelog for $NEW_TAG."
./scripts/pr-wait.sh "$(gh pr view --json number -q .number)"
gh pr merge --squash --delete-branch
git switch main
git pull --ff-only
```

Then continue from Step 7.

## Error Handling

- **A check fails or review finds a real problem**: stop before merging and report it.
- **PR merge fails**: report the error and don't tag.
- **Changelog update fails**: nothing is tagged yet; report the error and don't tag.
- **Tag push fails**: report the error and suggest manual intervention.
- **Release workflow fails**: the tag is pushed; report the failed run and suggest re-running it with `gh run rerun`.
- **Any other step fails**: report which step failed and the current state of the branch, PR, and tags.

## Output

On success, report:
- PR URL
- New tag
- Release URL
- The merge commit SHA on main

## Example Usage

```
/merge-release           # patch bump (default)
/merge-release minor
/merge-release major
```
