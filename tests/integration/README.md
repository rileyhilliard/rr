# Integration test setup

Integration tests require SSH connectivity to a remote host. This document covers three approaches for setting up a test environment.

## Environment variables

| Variable | Description | Example |
|----------|-------------|---------|
| `RR_TEST_SSH_HOST` | SSH host for testing (format: `host:port` or `host`) | `localhost:2222` |
| `RR_TEST_SSH_USER` | SSH user for authentication | `testuser` |
| `RR_TEST_SSH_KEY` | Path to SSH private key (for CI/Docker) | `/tmp/rr-ci-ssh-keys/id_ed25519` |
| `RR_TEST_SKIP_SSH` | Set to `1` to skip SSH-dependent tests | `1` |

When `RR_TEST_SSH_KEY` is set, tests automatically disable strict host key checking since Docker containers regenerate host keys on each start.

## Option 1: Docker SSH server (recommended)

Spin up a disposable SSH server with ephemeral keys. Works locally and in CI.

```bash
# Start server and generate keys
./scripts/ci-ssh-server.sh start

# Set env vars and run tests
eval $(./scripts/ci-ssh-server.sh env)
go test -v ./tests/integration/... ./pkg/sshutil/...

# Clean up
./scripts/ci-ssh-server.sh stop
```

The script uses `linuxserver/openssh-server` on port 2222 with key-based auth (no passwords).

## Option 2: Local SSH (macOS/Linux)

Use your machine's SSH server. Requires SSH enabled locally.

**macOS:**
1. Enable Remote Login in System Settings > General > Sharing
2. Run tests:
   ```bash
   RR_TEST_SSH_HOST=localhost go test ./tests/integration/... -v
   ```

**Linux:**
```bash
sudo apt install openssh-server
sudo systemctl start sshd
RR_TEST_SSH_HOST=localhost go test ./tests/integration/... -v
```

## Option 3: Skip SSH tests

When working on non-SSH features:

```bash
RR_TEST_SKIP_SSH=1 go test ./tests/integration/...
```

## Running tests

```bash
# Docker approach (recommended)
./scripts/ci-ssh-server.sh start
eval $(./scripts/ci-ssh-server.sh env)
go test -v ./tests/integration/... ./pkg/sshutil/...
./scripts/ci-ssh-server.sh stop

# Or use make targets
make test-integration    # Requires SSH setup
RR_TEST_SKIP_SSH=1 make test-integration  # Skip SSH tests
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
