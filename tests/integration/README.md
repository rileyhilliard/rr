# Integration Test Setup

Integration tests require SSH connectivity to a remote host. This document covers three approaches for setting up a test environment.

## Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `RR_TEST_SSH_HOST` | SSH host for testing (format: `host:port` or just `host`) | `localhost` |
| `RR_TEST_SSH_USER` | SSH user for authentication | Current user |
| `RR_TEST_SSH_KEY` | Path to SSH private key | `~/.ssh/id_rsa` |
| `RR_TEST_SKIP_SSH` | Set to `1` to skip SSH-dependent tests | unset |

## Option 1: Local SSH (macOS/Linux)

Use your local machine's SSH server for testing.

### macOS

1. Enable Remote Login in System Settings > General > Sharing
2. Run tests with localhost:
   ```bash
   RR_TEST_SSH_HOST=localhost go test ./tests/integration/... -v
   ```

### Linux

1. Install and start OpenSSH server:
   ```bash
   sudo apt install openssh-server
   sudo systemctl start sshd
   ```
2. Run tests:
   ```bash
   RR_TEST_SSH_HOST=localhost go test ./tests/integration/... -v
   ```

## Option 2: Docker SSH Server

Spin up a disposable SSH server in Docker. Good for CI or when you don't want to enable local SSH.

```bash
# Start the test SSH server
./scripts/test-ssh-server.sh

# Run tests against it
RR_TEST_SSH_HOST=localhost:2222 go test ./tests/integration/... -v

# Clean up when done
docker stop rr-test-ssh && docker rm rr-test-ssh
```

The Docker container uses the `linuxserver/openssh-server` image and exposes SSH on port 2222.

## Option 3: Skip SSH Tests

If you're working on non-SSH features, skip the integration tests entirely:

```bash
RR_TEST_SKIP_SSH=1 go test ./tests/integration/...
```

## Running Tests

```bash
# Run with SSH (requires one of the above setups)
make test-integration

# Skip SSH tests
RR_TEST_SKIP_SSH=1 go test ./tests/integration/...

# Verbose output
RR_TEST_SSH_HOST=localhost go test ./tests/integration/... -v -count=1
```

## Test Categories

### Non-SSH Tests (run with RR_TEST_SKIP_SSH=1)

These tests verify subsystems without requiring SSH:

- **Config Tests**: Loading, validation, variable expansion
- **Lock Tests**: LockInfo creation, serialization, stale detection
- **Output Tests**: Stream handling, formatter behavior, phase display
- **Sync Tests**: Rsync command building, progress parsing
- **Workflow Simulation**: Phase timing without actual execution

### SSH-Dependent Tests

These tests require SSH access:

- Full `rr run` workflow
- Actual file synchronization
- Remote lock acquisition/release

## Manual Testing Steps

Some scenarios benefit from manual testing:

### Lock Contention

Terminal 1:
```bash
./rr run "sleep 30"
```

Terminal 2 (while terminal 1 is running):
```bash
./rr run "echo test"
# Should show "Waiting for lock..." and block until terminal 1 completes
```

### Exit Code Propagation

```bash
./rr run "exit 42"
echo $?  # Should output 42
```

### Output Phases

```bash
./rr run "echo hello"
# Should display:
# - Connecting phase with timing
# - Syncing phase with timing
# - Lock acquire phase
# - Command output
# - Summary with total time
```

### Sync Exclusions

```bash
# Verify .git and other excluded patterns are not synced
./rr sync
# Check remote directory doesn't contain .git/, node_modules/, etc.
```

## Troubleshooting

**Connection refused**: Verify SSH is running on the target host and port.

**Permission denied**: Check that your SSH key is authorized on the target host.

**Host key verification failed**: The test host might not be in your known_hosts. Connect manually first or use a fresh Docker container.

**Tests timeout**: Some tests have built-in timeouts. If running against slow hosts, consider adjusting or skipping those tests.
