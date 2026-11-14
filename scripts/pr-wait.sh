#!/usr/bin/env bash
# The single-quoted GraphQL and jq programs below use $var syntax that is
# theirs, not the shell's.
# shellcheck disable=SC2016
#
# Waits for a PR's checks (CI and CodeRabbit) to finish, then prints a short
# digest of unresolved CodeRabbit review threads and any CodeRabbit notice
# that the review was skipped. Read-only: it never posts to GitHub.
#
# Usage:
#   ./scripts/pr-wait.sh <pr-number>
#
# Exit status is that of `gh pr checks --watch`: 0 when every check passed,
# non-zero when one failed. It exits 1 without waiting when the PR has no CI
# checks (a stacked PR, or one whose base isn't main).

set -euo pipefail

usage() {
    echo "usage: $0 <pr-number>" >&2
    exit 2
}

pr="${1:-}"
[[ "$pr" =~ ^[0-9]+$ ]] || usage

for cmd in gh jq; do
    if ! command -v "$cmd" >/dev/null 2>&1; then
        echo "error: $cmd not found on PATH" >&2
        exit 1
    fi
done

repo=$(gh repo view --json nameWithOwner --jq .nameWithOwner)
owner="${repo%/*}"
name="${repo#*/}"
base=$(gh pr view "$pr" --json baseRefName --jq .baseRefName)

# CodeRabbit posts its own check even on PRs that CI ignores, so "has checks"
# isn't enough: count the checks that aren't CodeRabbit.
ci_check_count() {
    gh pr checks "$pr" --json name --jq '[.[] | select(.name != "CodeRabbit")] | length' 2>/dev/null || echo 0
}

no_ci() {
    echo "error: PR #$pr has no CI checks (base: $base)." >&2
    echo "CI only runs for PRs based on main. Retarget with 'gh pr edit $pr --base main'," >&2
    echo "then push a commit: retargeting alone doesn't start CI." >&2
    exit 1
}

[[ "$base" == "main" ]] || no_ci

# Checks can take a few seconds to register after a push.
count=$(ci_check_count)
for _ in 1 2 3 4 5 6; do
    [[ "$count" -gt 0 ]] && break
    sleep 10
    count=$(ci_check_count)
done
[[ "$count" -gt 0 ]] || no_ci

# Off a terminal, --watch reprints the whole table on every refresh, so wait
# quietly and print the final state once.
status=0
if [[ -t 1 ]]; then
    gh pr checks "$pr" --watch || status=$?
else
    gh pr checks "$pr" --watch >/dev/null 2>&1 || status=$?
    gh pr checks "$pr" || true
fi

echo
echo "== Unresolved CodeRabbit threads =="

# REST doesn't expose thread resolution, so this goes through GraphQL. Each
# thread prints as path:line, the severity line, the title, and the first
# paragraph of the body, with <details> blocks and HTML comments dropped.
query='
query($owner: String!, $repo: String!, $pr: Int!) {
  repository(owner: $owner, name: $repo) {
    pullRequest(number: $pr) {
      reviewThreads(first: 100) {
        nodes {
          isResolved
          comments(first: 1) {
            nodes { path line originalLine body author { login } }
          }
        }
      }
    }
  }
}'

# Splits a comment body into paragraphs, dropping HTML comments and
# <details> blocks (CodeRabbit nests them and can put one before the title).
# This runs in jq, not gh's --jq: gh's jq uses Go regexps, which have no
# lookahead.
paragraphs='
  def paragraphs:
    gsub("<!--[\\s\\S]*?-->"; "")
    | reduce range(10) as $_ (.; gsub("<details>(?:(?!<details>)[\\s\\S])*?</details>"; ""))
    | split("<details>")[0]
    | split("\n\n")
    | map(gsub("^\\s+|\\s+$"; ""))
    | map(select(length > 0));'

gh api graphql -F owner="$owner" -F repo="$name" -F pr="$pr" -f query="$query" |
    jq -r "$paragraphs"'
      [.data.repository.pullRequest.reviewThreads.nodes[]
        | select(.isResolved | not)
        | .comments.nodes[0]
        | select(.author.login == "coderabbitai")]
      | if length == 0 then "(none)" else
          .[] | "\(.path):\(.line // .originalLine)\n  \(.body | paragraphs | .[0:3] | join("\n  "))\n"
        end'

# CodeRabbit edits one issue comment in place when it skips a review (base
# branch not main, rate limit), and a skip never turns into a review. Show
# the latest one if it's a skip notice.
notice=$(gh api --paginate "repos/$repo/issues/$pr/comments" \
    --jq '.[] | select(.user.login == "coderabbitai[bot]") | {updated_at, body}' |
    jq -rs "$paragraphs"'
      sort_by(.updated_at) | last // empty | .body
      | select(test("auto-generated comment: (skip review|rate limited) by coderabbit"))
      | gsub("(?m)^> ?"; "")
      | gsub("\\[!(WARNING|IMPORTANT|NOTE)\\]"; "")
      | paragraphs
      | .[0:3]
      | join("\n")')
if [[ -n "$notice" ]]; then
    echo
    echo "== CodeRabbit did not review =="
    echo "$notice"
fi

exit "$status"
