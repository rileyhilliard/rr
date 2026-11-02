#!/usr/bin/env bash
# PreToolUse (Edit|Write): deny writes that contain something shaped like a real
# credential: cloud and SaaS API keys, private keys, tokens, connection strings
# with embedded passwords.
#
# High-confidence formats only, to keep false positives rare. Placeholder-looking
# values (your-, changeme, example, xxx...) are ignored by the generic rule, and
# a line containing "pragma: allowlist secret" is skipped entirely (the same
# marker the detect-secrets tool uses), for test fixtures that must look real.
set -euo pipefail

hook_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
# shellcheck source=_lib.sh
source "$hook_dir/_lib.sh"

input=$(cat)
file_path=$(hook_get "$input" tool_input.file_path | head -n 1) || exit 0

# .env files (and direnv's .envrc) hold the real values; only the committed
# templates may be written.
case "$(basename "$file_path")" in
.env.example | .env.sample | .env.template) ;;
.env | .env.* | .envrc)
  hook_deny "BLOCKED: $(basename "$file_path") holds real credentials, so agents don't write it.

Put the variable's name in .env.example and ask the user to set the value in their .env.
Never work around this block."
  exit 0
  ;;
esac

# Files where example credentials are expected, or that can't hold one.
case "$file_path" in
*.example | *.sample | *.template | *.md | *.mdx | *.rst | *.txt | *.lock | *.svg | *.png | *.jpg | *.jpeg | *.gif | *.ico | *.webp)
  exit 0
  ;;
esac

# Write carries .content, Edit carries .new_string; the legacy MultiEdit tool
# carries .edits[].new_string.
text=$(
  hook_get "$input" tool_input.content
  hook_get "$input" tool_input.new_string
  hook_get "$input" tool_input.edits.[].new_string
) || exit 0
[ -z "$text" ] && exit 0

# Drop allowlisted lines before scanning.
text=$(grep -v -e 'pragma: allowlist secret' <<<"$text" || true)
[ -z "$text" ] && exit 0

has() { grep -qE -e "$1" <<<"$text"; }
# tokens <ere> [grep flag]: every matched string, one per line.
tokens() { grep -oE ${2:+"$2"} -e "$1" <<<"$text" || true; }
# has_real <ere>: some matched string doesn't look like a placeholder. Only the
# match is tested, so a "TODO" comment beside a real key doesn't excuse it.
has_real() {
  local found
  found=$(tokens "$1")
  [ -n "$found" ] && grep -qviE -e "$placeholder" <<<"$found"
}
# has_real_i <ere>: has_real, ignoring case (apiKey, API_KEY and api_key alike).
has_real_i() {
  local found
  found=$(tokens "$1" -i)
  [ -n "$found" ] && grep -qviE -e "$placeholder" <<<"$found"
}
placeholder='(your[-_]|changeme|replace|example|placeholder|xxxx|fake|dummy|redacted|TODO|FIXME|<[^>]*>)'
# OpenAI-style keys mix upper case and digits in a long tail, which a long
# lowercase slug (sk-learning-module-01-intro) doesn't. Anthropic keys have
# their own rule.
openai_key() {
  tokens 'sk-(proj-|svcacct-)?[A-Za-z0-9_-]{40,}' | grep -v '^sk-ant-' | grep '[A-Z]' | grep '[0-9]' |
    grep -qviE -e "$placeholder"
}

findings=""
add() { findings="${findings}- $1"$'\n'; }

has 'AKIA[0-9A-Z]{16}' && add "AWS access key ID"
has '(aws_secret_access_key|AWS_SECRET_ACCESS_KEY)["'\'']?[[:space:]]*[:=][[:space:]]*["'\'']?[A-Za-z0-9/+=]{40}' && add "AWS secret access key"
has '(ghp|gho|ghs|ghu|ghr)_[A-Za-z0-9]{36}|github_pat_[A-Za-z0-9_]{22,}' && add "GitHub token"
has 'glpat-[A-Za-z0-9_-]{20,}' && add "GitLab token"
has 'xox[baprs]-[A-Za-z0-9-]{10,}|hooks\.slack\.com/services/T[A-Z0-9]+/B[A-Z0-9]+/[A-Za-z0-9]+' && add "Slack token or webhook"
has '(sk|rk)_live_[A-Za-z0-9]{20,}' && add "Stripe live key"
has 'sk_test_[A-Za-z0-9]{20,}' && add "Stripe/Clerk test secret key"
has_real 'sk-ant-[A-Za-z0-9_-]{20,}' && add "Anthropic API key"
openai_key && add "OpenAI-style API key"
has 'AIza[0-9A-Za-z_-]{35}' && add "Google API key"
has 'npm_[A-Za-z0-9]{36}' && add "npm token"
has '-----BEGIN ([A-Z]+ )?PRIVATE KEY-----' && add "Private key (PEM)"
has '(postgres|postgresql|mysql|mongodb(\+srv)?|redis|amqp)://[^:/[:space:]]+:[^@[:space:]]{8,}@' &&
  ! grep -qiE -e '://[^:]+:(password|postgres|changeme|secret|pass|example)@' <<<"$text" &&
  add "Connection string with an embedded password"
has 'eyJ[A-Za-z0-9_-]{10,}\.eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}' && add "JWT"

# Generic: a long high-entropy literal assigned to a secret-looking name.
if has_real_i '(api[_-]?key|api[_-]?secret|secret[_-]?key|client[_-]?secret|access[_-]?token|auth[_-]?token|private[_-]?key)["'\'']?[[:space:]]*[:=][[:space:]]*["'\''][A-Za-z0-9/+=_-]{32,}["'\'']'; then
  add "Long literal assigned to a secret-looking name"
fi

[ -z "$findings" ] && exit 0

reason="BLOCKED: possible secret in the write to $(basename "$file_path").

Detected:
${findings}
Read credentials from environment variables (document them in .env.example) or a secrets manager.
If this is a deliberate fixture, ask the user, then mark the line with a \"pragma: allowlist secret\" comment.
Never work around this block."

hook_deny "$reason"
exit 0
