# shellcheck shell=bash
# Destructive-command patterns, sourced by block-destructive.sh. Kept in its own
# file so any other hook you add (an auto-approver, a Bash rewriter) can consult
# the same list: if a pattern lives here, nothing can approve a command the
# guard would deny.
#
# Usage:
#   source "$(dirname "${BASH_SOURCE[0]}")/_destructive-patterns.sh"
#   if found=$(is_destructive "$command"); then ...; fi
#
# is_destructive prints "<reason><TAB><suggestion>" and returns 0 when the
# command matches, returns 1 otherwise. Patterns use POSIX classes only, so BSD
# grep (macOS) and GNU grep agree. Matching uses here-strings, not pipes, so it
# stays correct under `set -o pipefail`.
#
# Add project-specific patterns at the end (e.g. a deploy script that must never
# run from an agent session). Test them in the project's hook tests.

# split_commands <blank>: stdin's command line, one simple command per line.
# Outside quotes and inside double quotes a backslash escapes the next
# character, so \" or \x27 opens no string; inside single quotes it is literal.
# With <blank> 1, whitespace inside quotes becomes _. If a quote is still open
# at the end, the quoting was misread: fail closed by also emitting every line
# split on every separator, quotes ignored and nothing blanked.
split_commands() {
  awk -v blank="$1" '
    {
      out = ""
      n = length($0)
      for (i = 1; i <= n; i++) {
        c = substr($0, i, 1)
        if (c == "\\" && q != "\047" && i < n) { i++; c = c substr($0, i, 1) }
        else if (q != "") { if (c == q) q = ""; else if (blank == 1 && (c == " " || c == "\t")) c = "_" }
        else if (c == "\047" || c == "\"") q = c
        else if (c == ";" || c == "&" || c == "|") c = "\n"
        out = out c
      }
      print out
      raw = $0
      gsub(/[;&|]/, "\n", raw)
      lines = lines raw "\n"
    }
    END { if (q != "") printf "%s", lines }'
}

is_destructive() {
  local command=$1
  [ -z "$command" ] && return 1

  # A shell's -c payload is a command line of its own: check it the same way.
  # (bash -c "rm -rf /", sh -c 'cd /tmp; rm -rf ~'). The leading space lets the
  # pattern require a non-word character before the shell name (not ssh -c).
  # Every payload on the line is checked: the greedy prefix finds the last one,
  # which is then cut off and the search repeated. A single-quoted payload may
  # hold '\'' and a double-quoted one \" and \\; both are unescaped before the
  # check.
  local shell_c='[^[:alnum:]_.-](ba|z|da|k)?sh[[:space:]]+(-[[:alpha:]]+[[:space:]]+)*-[[:alpha:]]*c[[:space:]]+'
  local re rest payload sq="'" sq_escaped="'\\''"
  for re in "${shell_c}'(([^']|'\\\\'')*)'" "${shell_c}\"(([^\"\\\\]|\\\\.)*)\""; do
    rest=" $command"
    # The x marks a match, so an empty payload ('') doesn't end the search.
    while payload=$(sed -nE "s/^(.*)${re}.*/x\\4/p" <<<"$rest") && [ -n "$payload" ]; do
      payload=${payload#x}
      case $re in
      *\') payload=${payload//"$sq_escaped"/$sq} ;;
      *) payload=$(sed -E 's/\\([\\"$`])/\1/g' <<<"$payload") ;;
      esac
      if [ "$payload" != "$command" ] && is_destructive "$payload"; then
        return 0
      fi
      rest=$(sed -E "s/^(.*)${re}.*/\\1/" <<<"$rest")
    done
  done

  # m <ere>: case-sensitive match. mi <ere>: case-insensitive.
  m() { grep -qE -e "$1" <<<"$command"; }
  mi() { grep -qiE -e "$1" <<<"$command"; }
  hit() {
    printf '%s\t%s' "$1" "$2"
  }

  # A separator or end of command, used after a dangerous argument.
  local end='([[:space:]]|;|&|\||\)|$)'
  # Start of a command word: start of string, a separator, or whitespace.
  local word='(^|[;&|(`]|[[:space:]])'
  # git and its global options, up to the subcommand: git -C dir, -c k=v,
  # --no-pager, --git-dir=.git or --git-dir .git all reach the same rules.
  local git_='git([[:space:]]+(-[Cc][[:space:]]+[^[:space:]]+|--(git-dir|work-tree|namespace|config-env)[[:space:]]+[^[:space:]]+|-[[:alpha:]]+|--[[:alnum:]-]+(=[^[:space:]]*)?))*[[:space:]]+'

  # --- Simple commands -------------------------------------------------------
  # Most rules look at one simple command at a time, split on ; && || | & and
  # newlines, so `rm -f out.txt && cd ..` or `rm build.log; ls /` don't read the
  # next command's argument as rm's target. Separators inside quotes don't
  # split (echo 'a;rm -rf ~' is one echo); sh -c payloads were checked above.
  # The rule's program must be the segment's command word, possibly after env
  # assignments, a shell keyword (then/do), or a wrapper such as
  # sudo/env/timeout/nice with its options and numeric arguments (sudo -u root,
  # timeout 5, nice -n 10). So a commit message, a PR body or an echo that
  # mentions `git reset --hard` isn't the command it mentions.
  local wrap='(then|do|else|sudo|doas|env|exec|eval|command|builtin|nohup|time|nice|ionice|timeout|stdbuf)'
  local wrap_arg='(-[^[:space:]]*([[:space:]]+[^-[:space:]][^[:space:]]*)?|[0-9][^[:space:]]*|[A-Za-z_][A-Za-z0-9_]*=[^[:space:]]*)'
  local start="^[[:space:]]*((\\\$\\(|[({!\`])[[:space:]]*|${wrap}([[:space:]]+${wrap_arg})*[[:space:]]+|[A-Za-z_][A-Za-z0-9_]*=[^[:space:]]*[[:space:]]+)*[\"']?([^[:space:]\"';&|]*/|\\\\)?"
  # segments: one simple command per line. quiet: the same, with whitespace
  # inside quotes turned into _, so a quoted argument ("explain git push -f")
  # can't supply the separate words a rule looks for.
  local segments quiet
  segments=$(split_commands 0 <<<"$command")
  quiet=$(split_commands 1 <<<"$command")
  # c <ere>: some simple command runs a program matching <ere> (its words
  # follow), quoted arguments excluded.
  c() { grep -qE -e "${start}$1" <<<"$quiet"; }

  # --- Filesystem ------------------------------------------------------------
  local rm_start="${start}rm[[:space:]]"
  local seg
  while IFS= read -r seg; do
    grep -qE -e "$rm_start" <<<"$seg" || continue
    # rm against /, ~, ., .., $HOME, $PWD or their / and /* forms, quoted or
    # not, the quote anywhere ("$HOME"/, "$HOME/"*). Matching on the target
    # rather than the flag cluster closes the split-flag bypass (rm -r -f /).
    if grep -qE -e "[[:space:]=][\"']?(/[\"']?\*?|(~|\\\$HOME|\\\$\{HOME\}|\\\$PWD|\\\$\{PWD\}|\.\.?)[\"']?(/[\"']?\*?)?)[\"']?${end}" <<<"$seg"; then
      hit "rm targeting the filesystem root, home, or the working directory." \
        "Name the exact paths to remove. Ask the user first."
      return 0
    fi
    if grep -qE -e "[[:space:]]\*${end}" <<<"$seg"; then
      hit "rm with a bare wildcard." "Be explicit about which files to remove. Ask the user first."
      return 0
    fi
  done <<<"$segments"

  if m "xargs[[:space:]].*rm[[:space:]]+-[a-zA-Z]*[rRf]"; then
    hit "Recursive or forced rm fed by xargs." "Review the pipeline. Ask the user first."
    return 0
  fi
  if m "${word}find[[:space:]].*-(delete${end}|exec(dir)?[[:space:]]+(rm|unlink|shred)[[:space:]])"; then
    hit "find with -delete or -exec rm deletes every matched file." \
      "Review the find expression and its root path. Ask the user first."
    return 0
  fi
  if m ">[[:space:]]*/dev/(sd|nvme|hd|disk|mapper|vd|xvd)"; then
    hit "Redirecting output to a raw disk device destroys it." "Do not run without explicit confirmation."
    return 0
  fi
  if m "${word}dd[[:space:]].*of=/dev/(sd|nvme|hd|disk|mapper|vd|xvd)"; then
    hit "dd writing to a raw disk device destroys it." "Confirm the of= target. Ask the user first."
    return 0
  fi
  if m "${word}mkfs(\.[[:alnum:]]+)?[[:space:]]"; then
    hit "mkfs reformats a device." "Do not run without explicit confirmation."
    return 0
  fi
  if m "${word}chmod[[:space:]]+(-R[[:space:]]+)?0?777"; then
    hit "chmod 777 makes files world-writable." "Use 755 for directories and 644 for files."
    return 0
  fi
  if m "${word}chown[[:space:]]+([^;&|]*[[:space:]])?-[[:alpha:]]*R[^;&|]*[[:space:]][\"']?(/|~|\\\$HOME|\\\$\{HOME\})/?[\"']?${end}"; then
    hit "Recursive chown of the filesystem root or home." "Name the exact directory. Ask the user first."
    return 0
  fi
  # :(){ :|:& };: and the same shape under any function name.
  if m "\(\)[[:space:]]*\{[^}]*\|[^}]*&[^}]*\}[[:space:]]*;"; then
    hit "A fork bomb exhausts the machine's processes." "Do not run it."
    return 0
  fi

  # --- Git: discarding uncommitted work --------------------------------------
  # Every path at once (. or :/), with or without a ref or options before it
  # (git checkout HEAD -- ., git restore --source=HEAD .), in one command.
  local all_paths="[[:space:]]+(.*[[:space:]])?(\.|:/)${end}"
  if c "${git_}checkout${all_paths}"; then
    hit "git checkout . discards every unstaged change." "Revert specific files: git checkout -- <file>"
    return 0
  fi
  # checkout -f and switch -f/--discard-changes throw away local changes too.
  if c "${git_}(checkout|switch)[[:space:]](.*[[:space:]])?(-[[:alpha:]]*f[[:alpha:]]*|--force|--discard-changes)([[:space:]]|$)"; then
    hit "A forced checkout or switch discards every local change." \
      "Commit or stash the work first. Ask the user first."
    return 0
  fi
  # restore touches the worktree unless only --staged/-S is given, which just
  # unstages. Checked per command, so one restore's --staged can't cover another.
  local restore
  while IFS= read -r restore; do
    grep -qE -e "${start}${git_}restore${all_paths}" <<<"$restore" || continue
    if ! grep -qE -e "[[:space:]](--staged|-[[:alpha:]]*S[[:alpha:]]*)([[:space:]]|$)" <<<"$restore" ||
      grep -qE -e "[[:space:]](--worktree|-[[:alpha:]]*W[[:alpha:]]*)([[:space:]]|$)" <<<"$restore"; then
      hit "git restore . discards every unstaged change." "Revert specific files: git restore <file>"
      return 0
    fi
  done <<<"$quiet"
  if c "${git_}clean[[:space:]]+(.*[[:space:]])?(-[a-zA-Z]*f|--force)"; then
    hit "git clean -f deletes untracked files permanently." "Preview with git clean -n. Ask the user first."
    return 0
  fi
  if c "${git_}reset[[:space:]]+(.*[[:space:]])?--hard"; then
    hit "git reset --hard discards all uncommitted changes, staged and unstaged." \
      "Use git stash or a soft reset to keep the work. Ask the user first."
    return 0
  fi
  if c "${git_}stash[[:space:]]+(drop|clear)"; then
    hit "git stash drop/clear destroys stashed work." "Other sessions may have work in the stash. Ask the user first."
    return 0
  fi
  if c "${git_}branch[[:space:]](.*[[:space:]])?-[a-zA-Z]*D"; then
    hit "git branch -D deletes a branch even when its commits are merged nowhere." \
      "Use git branch -d, which refuses unmerged work. Ask the user first."
    return 0
  fi

  # --- Git: skipping the project's hooks ---------------------------------------
  # commit's -n is --no-verify, alone or in a cluster (-nm, -anm), but not as
  # the argument of -m, -c, -C, -F or -t (-mn is the message "n"). push -n is
  # --dry-run.
  if c "${git_}(commit|push)[[:space:]](.*[[:space:]])?--no-verify([[:space:]]|$)" ||
    c "${git_}commit[[:space:]](.*[[:space:]])?-[abd-lo-su-zABDEG-Z]*n[[:alpha:]]*([[:space:]]|$)"; then
    hit "--no-verify skips the project's git hooks." "Fix what the hook reports instead. Ask the user first."
    return 0
  fi

  # --- Git: rewriting or deleting shared history ------------------------------
  # --force-with-lease and --force-if-includes are allowed.
  if c "${git_}push[[:space:]](.*[[:space:]])?(-f|--force|-[a-zA-Z]*f[a-zA-Z]*)([[:space:]]|$)" ||
    c "${git_}push[[:space:]](.*[[:space:]])?\+[^[:space:]]+"; then
    hit "Force push rewrites remote history." "Ask the user first. If it is needed, use --force-with-lease."
    return 0
  fi
  # --mirror, --delete/-d and a :<ref> refspec delete remote branches.
  if c "${git_}push[[:space:]](.*[[:space:]])?(--mirror|--delete|-[a-zA-Z]*d[a-zA-Z]*|:[^[:space:]]+)([[:space:]]|$)"; then
    hit "This push deletes or overwrites remote branches." "Ask the user first."
    return 0
  fi

  # --- Databases -------------------------------------------------------------
  # Only when a database client runs, so a commit message or an echo that
  # mentions DROP TABLE isn't blocked.
  local db_client="${word}(psql|pgcli|mysql|mariadb|mycli|sqlite3|litecli|sqlcmd|duckdb|clickhouse(-client)?|cockroach|snowsql)([[:space:]]|$)"
  if m "$db_client" && mi "(drop[[:space:]]+(table|database|schema)|truncate[[:space:]]+(table[[:space:]]+)?[[:alnum:]_\".]+)"; then
    hit "DROP or TRUNCATE causes irreversible data loss." "Ask the user first."
    return 0
  fi
  if m "$db_client" && mi "delete[[:space:]]+from[[:space:]]+[^[:space:];]+[[:space:]]*;?[[:space:]]*[\"']?[[:space:]]*$"; then
    hit "DELETE without a WHERE clause empties the table." "Add a WHERE clause, or ask the user first."
    return 0
  fi

  # --- Processes ---------------------------------------------------------------
  if m "${word}(killall[[:space:]]|pkill[[:space:]]+-9|kill[[:space:]]+-9[[:space:]]+-1)"; then
    hit "Broad process killing." "Kill a specific PID, or ask the user."
    return 0
  fi

  # --- Containers, hosting, releases ------------------------------------------
  if c "docker[[:space:]]+(system[[:space:]]+prune|volume[[:space:]]+(rm|prune)|image[[:space:]]+prune|rmi[[:space:]])" ||
    c "docker[[:space:]]+compose[[:space:]]+down[[:space:]](.*[[:space:]])?(-v|--volumes)${end}"; then
    hit "This deletes Docker images or volumes (including database data)." \
      "The Docker context may not be local. Ask the user first."
    return 0
  fi
  if c "gh[[:space:]]+(repo|release)[[:space:]]+delete"; then
    hit "gh repo/release delete is unrecoverable." "Ask the user first."
    return 0
  fi
  # --dry-run or --snapshot excuses only the publishing command it belongs to,
  # so `echo --dry-run; npm publish` is still denied.
  local publish="${start}((npm|pnpm|yarn|bun|uv|cargo|poetry|gem)[[:space:]]+(.*[[:space:]])?publish${end}|twine[[:space:]]+upload|goreleaser[[:space:]]+release)"
  while IFS= read -r seg; do
    grep -qE -e "$publish" <<<"$seg" || continue
    grep -qE -e "[[:space:]][\"']?(--dry-run|--snapshot)(=[^[:space:]]*)?[\"']?${end}" <<<"$seg" && continue
    hit "Publishing a release is irreversible." "Releases are user-initiated. Ask the user first."
    return 0
  done <<<"$quiet"
  if c "(terraform|tofu)[[:space:]]+(.*[[:space:]])?(destroy|apply[[:space:]](.*[[:space:]])?-destroy)${end}" ||
    c "pulumi[[:space:]]+(.*[[:space:]])?destroy${end}" ||
    c "kubectl[[:space:]]+(.*[[:space:]])?delete${end}" ||
    c "aws[[:space:]]+s3[[:space:]]+(rm[[:space:]](.*[[:space:]])?--recursive|rb[[:space:]](.*[[:space:]])?--force)${end}"; then
    hit "This tears down infrastructure or deletes cloud data." "Infrastructure changes are user-initiated. Ask the user first."
    return 0
  fi

  # --- Running downloaded code and raising privileges --------------------------
  # Only in command position (start, after a separator, then/do), so a commit
  # message or an echo that mentions them isn't blocked.
  local cmd='(^|[;&|(`]|then|do)[[:space:]]*'
  local shell_word='(sudo[[:space:]]+)?(ba|z|da|k)?sh([[:space:]]|$)'
  if m "${cmd}(curl|wget)[[:space:]][^;&|]*\|[[:space:]]*${shell_word}" ||
    m "${cmd}(ba|z|da|k)?sh[[:space:]]+<\([[:space:]]*(curl|wget)[[:space:]]"; then
    hit "Piping a download into a shell runs code nobody has read." \
      "Download the script, show it to the user, and ask before running it."
    return 0
  fi
  if m "${cmd}sudo([[:space:]]|$)"; then
    hit "sudo runs the command as root." "Ask the user to run it themselves if it is needed."
    return 0
  fi

  return 1
}
