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

Done. Skip to [Verify it works](#verify-it-works).

## Full walkthrough

### 1. Check for existing SSH keys

```bash
ls ~/.ssh/id_*.pub
```

If you see files like `id_ed25519.pub` or `id_rsa.pub`, you already have keys. Skip to [Copy your key to the remote](#2-copy-your-key-to-the-remote).

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
1. Generate a key if you don't have one
2. Copy your public key to the remote
3. Test the connection

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

The remote machine's key changed (reinstalled OS, different machine at same IP, etc.).

If you trust the change:
```bash
ssh-keygen -R your-remote-machine
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
