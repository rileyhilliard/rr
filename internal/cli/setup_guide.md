# rr setup guide

`rr setup` has already looked at this project: `next_steps` says what's left,
in order. This guide covers how to do each step and what to do when one fails.
Work through the steps in order and fix problems as they come up. Run
`rr setup` again whenever you want a fresh read of what's left.

rr emits structured JSON by default: `doctor`, `status` and `tasks` print a
`{"success":...,"data":...}` envelope on stdout, and a failure prints
`{"success":false,"error":{"code":...,"suggestion":...}}`. Branch on
`error.code`, not the message.

## 1. Hosts (`~/.rr/config.yaml`)

Hosts are personal, so they live in the global config, not the repo. If there
are none, ask the user for the SSH alias or `user@host` of each machine and
where the project should live there, then add it:

```bash
rr host add --name <name> --ssh "<alias>" --dir '~/rr/${PROJECT}' --skip-probe
```

`--ssh` takes several comma-separated aliases (LAN first, then VPN); rr prefers
earlier ones. Drop `--skip-probe` to test the connection and detect PATH
differences while adding.

To run on this machine (no remote, or in rotation with remotes, for example so
concurrent agents queue on one lock):

```bash
rr host add --local --name dev
```

A local host runs in place in the project directory with no SSH or sync. Only
one host can be local, it can't be named `local`, and local hosts don't work on
native Windows. `CONFIG_INVALID` with "only one host can be local" means one
already exists; use that one. Projects with no `hosts:` list in `.rr.yaml` pick
it up automatically, so tell the user if they have other projects.

## 2. Project config (`.rr.yaml`)

`rr setup` writes `.rr.yaml` when the project has none, with `require:` and
`tasks:` filled in from the detected stack (`data.stack`). It never overwrites
an existing one: compare it with `data.stack` and add what's missing by hand.

Check the generated file:

- **Tasks.** Make sure each `run:` is the command the project really uses (CI
  config and the README are good sources). Add tasks for anything run often.
- **Hosts.** With no `hosts:` list, every global host is used. List hosts to
  restrict the project to them, in preference order.
- **Sync.** A custom `sync.exclude` list replaces rr's defaults, so keep the
  defaults in it. Add large build outputs and data dirs. `.gitignore` is applied
  too.

If rr fails with `CONFIG_NOT_FOUND` naming a `.rr.yaml` above the current
checkout, you're in a worktree nested inside the main checkout. Commit
`.rr.yaml` (or copy it in with the `cp` command from the error) so the
worktree has its own.

## 3. Connectivity

```bash
rr doctor
```

- `data.summary.all_clear == true` means setup is OK.
- Otherwise read `data.categories[].results[]` with a non-zero `status`
  (`0` pass, `1` warn, `2` fail). Doctor exits 1 when any check fails and 0 on
  warnings only. A warning (no SSH agent, one unreachable host while another
  answers) doesn't block runs.

For per-host detail, `rr status` lists `data.hosts[]`. A host with
`healthy: false` has an `error` on each alias:

| Error | Fix |
| ----- | --- |
| connection timed out | Network or VPN. Check `ping <hostname>`, try another alias |
| authentication failed | Key not deployed or not in the agent. `rr setup <host>` copies the key; `ssh-add` loads it |
| host key verification failed (first connection) | Verify the fingerprint through a trusted channel (for example `ssh-keygen -lf /etc/ssh/ssh_host_ed25519_key.pub` on the host's console), then `ssh -o StrictHostKeyChecking=accept-new <alias> exit` |
| host key changed | Don't accept it automatically: it can mean interception. Have the user confirm the new fingerprint, then `ssh-keygen -R <hostname>` and connect again |

Debug SSH by hand with `ssh <alias>` and `grep -A5 "<alias>" ~/.ssh/config`.
If `ssh` works but rr says "handshake failed", the key isn't in the agent. Run
`ssh-add <key>`, and for passphrase-protected keys add this to the host's
`~/.ssh/config` entry so it stays loaded:

```
    AddKeysToAgent yes
    UseKeychain yes   # macOS
```

A local host has no SSH: `rr doctor` skips the key and agent checks when every
host is local, and `rr setup <local host>` refuses by design.

## 4. Execution

```bash
rr exec "echo rr-test-ok"   # runs without syncing
rr run "ls -la"             # syncs, then runs
```

stdout has the command's output; stderr has JSON phase events and a final
`{"type":"result",...}` line. If the command ran and failed, read that result's
`exit_code` and `details`. If rr failed before the command started, stderr has
an error envelope instead:

- `LOCK_HELD`: the message names the holder. If it's gone, `rr unlock <host>`.
- `INTERRUPTED` (exit 130): rr was stopped while waiting for a lock; nothing ran.
- Wrong directory: check the host's `dir` in `~/.rr/config.yaml`. A local host
  has no `dir`: it runs in the project directory, and the sync phase reports
  `skipped` with `reason: in_place`.
- "command not found": the remote shell's PATH misses the tool. Add
  `shell: "zsh -l -c"` or `setup_commands` to the host.

## 5. Requirements

```bash
rr doctor --requirements
```

The `REQUIREMENTS` category lists missing tools, marked "(can install)" when rr
has an installer. Install those with:

```bash
rr provision --check   # report what's missing
rr provision --yes     # install without prompts
```

Install the rest over SSH, after checking with the user:

```bash
ssh <alias> "curl -LsSf https://astral.sh/uv/install.sh | sh"      # uv
ssh <alias> "curl -fsSL https://bun.sh/install | bash"             # bun
ssh <alias> "curl -o- https://raw.githubusercontent.com/nvm-sh/nvm/v0.39.0/install.sh | bash && . ~/.nvm/nvm.sh && nvm install --lts"  # node via nvm (use the version the project pins, if any)
```

Then make the tools visible to rr's non-interactive shell through the host's
`setup_commands` in `~/.rr/config.yaml`:

```yaml
setup_commands:
  - source ~/.local/bin/env            # uv
  - export PATH="$HOME/.bun/bin:$PATH" # bun
```

If a `setup_commands` line references a file that doesn't exist, install the
tool that creates it or remove the line. `--skip-requirements` on `rr run` or
`rr exec` bypasses the checks for one run.

## 6. Final check

Run the project's real test command end to end, for example `rr test` when a
`test` task exists. Exit code 0 means setup is done.

## Troubleshooting

| Problem | Fix |
| ------- | --- |
| SSH fails | `ssh <alias>` by hand, check `~/.ssh/config` |
| "handshake failed" | Key not in the agent: `ssh-add`, add `AddKeysToAgent yes` |
| "command not found" | `shell: "zsh -l -c"` or `setup_commands` on the host |
| Sync slow | Add large dirs to `sync.exclude` |
| Lock stuck | `rr unlock <host>` |
| Wrong directory | Check `dir` in the global config |
