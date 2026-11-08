---
title: SSH setup
description: Set up passwordless SSH from your machine to a remote host so rr can sync and run there.
group: getting-started
order: 10
---

# SSH setup

Before using rr, you need passwordless SSH access to your remote machine. This guide walks through setting that up.

## Prerequisites

- A remote machine running SSH (Linux, macOS, or WSL)
- Network access to that machine (LAN, VPN, or internet)

## Quick version

If you already have SSH keys and just need to copy them to a new machine:

```bash
ssh-copy-id user@your-remote-machine
```

Done. Skip to [Verify it works](#4-verify-it-works).

## Full walkthrough

### 1. Check for existing SSH keys

```bash
ls ~/.ssh/id_*.pub
```

If you see files like `id_ed25519.pub` or `id_rsa.pub`, you already have keys. Skip to [Copy your key to the remote](#3-copy-your-key-to-the-remote).

If you get "No such file or directory", continue to generate new keys.

### 2. Generate an SSH key

```bash
ssh-keygen -t ed25519 -C "your-email@example.com"
```

Press Enter to accept the default location (`~/.ssh/id_ed25519`).

**Passphrase:** You can set one for extra security or leave it empty for convenience. If you set one, you'll need to use ssh-agent (covered below).

Example output:
```
Generating public/private ed25519 key pair.
Enter file in which to save the key (/Users/you/.ssh/id_ed25519):
Enter passphrase (empty for no passphrase):
Your identification has been saved in /Users/you/.ssh/id_ed25519
Your public key has been saved in /Users/you/.ssh/id_ed25519.pub
```

### 3. Copy your key to the remote

The easiest way:

```bash
ssh-copy-id user@your-remote-machine
```

Replace `user` with your username on the remote machine (often the same as your local username).

You'll be prompted for the remote password one last time. After this, you won't need it again.

**If ssh-copy-id isn't available** (some macOS versions), do it manually:

```bash
cat ~/.ssh/id_ed25519.pub | ssh user@your-remote-machine "mkdir -p ~/.ssh && cat >> ~/.ssh/authorized_keys"
```

### 4. Verify it works

```bash
ssh user@your-remote-machine "echo 'SSH is working'"
```

If this prints "SSH is working" without asking for a password, you're done.

This first connection also matters for another reason: rr verifies host keys against `~/.ssh/known_hosts` (or the files your `~/.ssh/config` names) and can't prompt to accept a new one. Connect with `ssh` once per hostname or alias you list in rr's config, so the key gets recorded. Otherwise rr fails with a host key error (`SSH_HOST_KEY`). If you'd rather rr record new keys itself, set `StrictHostKeyChecking accept-new` for the host (see [How rr uses your SSH config](#how-rr-uses-your-ssh-config)).

## Using ssh-agent (for passphrase-protected keys)

If you set a passphrase on your key, you'll want ssh-agent to remember it so you don't type it constantly.

### macOS

macOS has built-in keychain integration:

```bash
# Add key to keychain
ssh-add --apple-use-keychain ~/.ssh/id_ed25519
```

Add this to `~/.ssh/config` to auto-load from keychain:

```
Host *
    UseKeychain yes
    AddKeysToAgent yes
```

### Linux

Start the agent and add your key:

```bash
eval $(ssh-agent)
ssh-add ~/.ssh/id_ed25519
```

To persist across terminal sessions, add to your `~/.bashrc` or `~/.zshrc`:

```bash
if [ -z "$SSH_AUTH_SOCK" ]; then
    eval $(ssh-agent -s) > /dev/null
    ssh-add ~/.ssh/id_ed25519 2>/dev/null
fi
```

## Setting up an SSH config alias

SSH config aliases make your life easier. Instead of typing `ssh user@192.168.1.50`, you can type `ssh devbox`.

Edit `~/.ssh/config` (create it if it doesn't exist):

```
Host devbox
    HostName 192.168.1.50
    User riley
    IdentityFile ~/.ssh/id_ed25519
    AddKeysToAgent yes
    UseKeychain yes
```

Now `ssh devbox` connects to `riley@192.168.1.50` using your key.

**Important:** The `AddKeysToAgent` and `UseKeychain` options (macOS) ensure your key is automatically loaded into the SSH agent. This is required for rr to work with passphrase-protected keys, since rr cannot prompt for passphrases interactively.

The `IdentityFile` line tells SSH which private key to use. This is what makes the connection passwordless (assuming you've copied the public key to the remote with `ssh-copy-id`).

### Multiple ways to reach the same machine

If your machine is reachable via LAN and Tailscale, set up both:

```
Host devbox-lan
    HostName 192.168.1.50
    User riley
    IdentityFile ~/.ssh/id_ed25519
    AddKeysToAgent yes
    UseKeychain yes

Host devbox-tailscale
    HostName devbox.tailnet-name.ts.net
    User riley
    IdentityFile ~/.ssh/id_ed25519
    AddKeysToAgent yes
    UseKeychain yes
```

Then in your global config (`~/.rr/config.yaml`), list both and rr will use whichever one is reachable:

```yaml
# ~/.rr/config.yaml
hosts:
  devbox:
    ssh:
      - devbox-lan        # Try LAN first (faster)
      - devbox-tailscale  # Fall back to Tailscale
    dir: ~/projects/${PROJECT}
```

## Using rr setup

Once you have SSH working, `rr setup` can help configure additional machines:

```bash
rr setup user@new-machine
```

This will:
1. Generate an ed25519 key if you don't have one
2. Test the connection
3. If authentication fails, offer to copy your public key to the remote, then test again

## How rr uses your SSH config

rr runs commands over its own SSH client, not `ssh` (sync uses rsync, which does use `ssh`). That client reads `~/.ssh/config` but only understands part of it:

| Option | Supported |
|--------|-----------|
| `HostName`, `Port`, `User` | Yes |
| `IdentityFile` | Yes, including several `IdentityFile` lines for one host |
| `IdentityAgent` (1Password, Secretive, YubiKey agents) | Yes |
| `IdentitiesOnly` | Yes. Only the agent keys matching the host's `IdentityFile`s (or the default key files, when none is set) are offered, and other default key files are skipped |
| `ProxyCommand` | Yes |
| `ProxyJump` | Yes. rr runs `ssh -W` to the jump host, as OpenSSH does, so the jump host is resolved through your config and your `ssh`. Chains (`a,b`) and `ProxyJump none` work. A `ProxyCommand` on the same host wins, where OpenSSH takes whichever comes first |
| `StrictHostKeyChecking` | Yes: `yes`, `ask`, `accept-new`, `no`/`off`. See [Host keys](#host-keys) |
| `UserKnownHostsFile`, `GlobalKnownHostsFile` | Yes, including several paths, `~`, `${VAR}`, `none` and `/dev/null`. Paths rr can't use are skipped with a warning: ones with `%` tokens, ones naming an unset variable, and relative paths. See [Host keys](#host-keys) |
| `Match` blocks | No. Host entries after the first `Match` line aren't seen, so put the hosts rr uses above it |

For authentication, rr tries the SSH agent first, then each `IdentityFile` from your config that the agent doesn't already hold, then `~/.ssh/id_ed25519`, `~/.ssh/id_rsa`, and `~/.ssh/id_ecdsa`. It can't prompt for a passphrase, so passphrase-protected keys have to be loaded in the agent.

### Host keys

rr checks each host's key against the files in `UserKnownHostsFile` (default `~/.ssh/known_hosts`) and `GlobalKnownHostsFile` (default `/etc/ssh/ssh_known_hosts`, when it exists). `none` or `/dev/null` means no file. What happens with an unknown key depends on the host's `StrictHostKeyChecking`:

| Value | What rr does |
|-------|--------------|
| `yes`, `ask` (the default) | Fails with a host key error. rr can't prompt, so `ask` acts like `yes`, as it does for `ssh -o BatchMode=yes` |
| `accept-new` | Appends the key to the host's first `UserKnownHostsFile` and connects. A key that changed still fails. When the first entry is `/dev/null` or `none` (`UserKnownHostsFile /dev/null ~/.ssh/known_hosts`, say) it accepts the key for that connection and writes nothing, as `ssh` does. When the first entry is a path rr skips (see below), it checks keys as `yes` does. If the file can't be written, rr warns and connects anyway |
| `no`, `off` | Doesn't check the key. Unlike OpenSSH, rr writes nothing to known_hosts in this mode |

rr expands `~/` and `${VAR}` in these paths. It skips, with a warning, a path that uses `%` tokens (`~/.ssh/known_hosts.d/%k`), names a variable that isn't set, or isn't absolute after expansion. `ssh` reads a relative path from its working directory, and rr's is your synced project, so rr never reads or writes one. `ssh` records new keys in the first `UserKnownHostsFile` entry, so when rr skips that entry it can't record keys where `ssh` would look for them. Rather than accept every new key and record none, rr then treats `accept-new` like `yes`. To use `accept-new` with rr, list a plain path such as `~/.ssh/known_hosts` first.

A known_hosts file rr can't parse stops the connection with an error naming the file, if it's one of your `UserKnownHostsFile` files. A bad global file is skipped with a warning. rr doesn't hash the lines it writes, whatever `HashKnownHosts` says.

This is what lets VMs that ship their own SSH config work as they do with `ssh`. Lima, for example, writes `StrictHostKeyChecking no` and `UserKnownHostsFile /dev/null` for its VMs, so rr skips the check for them too.

For throwaway CI machines where host keys change on every run, `--no-strict-host-key-checking` skips host key verification for every host, whatever `~/.ssh/config` says, and passes `-o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null` to the `ssh` that rsync and `ProxyJump` run. Don't use it for machines you care about.

## Troubleshooting

### "Permission denied (publickey)"

Your key isn't on the remote, or permissions are wrong.

**Check your key is loaded:**
```bash
ssh-add -l
```

If empty, add your key:
```bash
ssh-add ~/.ssh/id_ed25519
```

**Check remote permissions:**
```bash
ssh user@remote "ls -la ~/.ssh"
```

The `.ssh` directory should be `700` (drwx------) and `authorized_keys` should be `600` (-rw-------).

Fix if needed:
```bash
ssh user@remote "chmod 700 ~/.ssh && chmod 600 ~/.ssh/authorized_keys"
```

### "Connection refused"

SSH server isn't running on the remote.

```bash
# On the remote machine:
sudo systemctl status sshd    # Check status
sudo systemctl start sshd     # Start if not running
```

### "Connection timed out"

The remote machine isn't reachable. Check:
- Is it powered on?
- Are you on the right network (LAN vs VPN)?
- Is a firewall blocking port 22?

```bash
# Test if port 22 is reachable
nc -zv your-remote-machine 22
```

### "Host key verification failed"

Either rr has no key for the host yet, or the remote machine's key changed (reinstalled OS, different machine at same IP, etc.). The error names the known_hosts files rr checked.

First connection, with the default `StrictHostKeyChecking`:
```bash
ssh -o StrictHostKeyChecking=accept-new your-remote-machine exit
```

Or set `StrictHostKeyChecking accept-new` for the host in `~/.ssh/config`, and rr records the key itself on the next connection. If the host has `UserKnownHostsFile /dev/null`, accepting the key with `ssh` records it nowhere; use `StrictHostKeyChecking no` for a host like that (a local VM), or point `UserKnownHostsFile` at a real file.

If the key changed and you trust the change, remove the old entry from the file the error names, then connect once to accept the new key. For a port other than 22 the entry is `[host]:port`:
```bash
ssh-keygen -R your-remote-machine -f ~/.ssh/known_hosts
ssh-keygen -R '[your-remote-machine]:2222' -f ~/.ssh/known_hosts
ssh user@your-remote-machine   # Accept the new key
```

## Next steps

Once SSH is working:

```bash
cd your-project
rr init      # Set up rr for this project
rr doctor    # Verify everything works
rr run "echo hello"  # Test it out
```
