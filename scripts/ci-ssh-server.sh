#!/bin/bash
#
# Sets up a Docker SSH server with key-based authentication for CI testing.
# This script generates ephemeral SSH keys and configures the container to accept them.
#
# Usage:
#   ./scripts/ci-ssh-server.sh start   # Start server and configure keys
#   ./scripts/ci-ssh-server.sh stop    # Stop and clean up
#   ./scripts/ci-ssh-server.sh env     # Print environment variables for tests
#
# After starting, run tests with:
#   eval $(./scripts/ci-ssh-server.sh env)
#   go test ./tests/integration/... -v

set -e

CONTAINER_NAME="rr-ci-ssh"
SSH_PORT="${RR_CI_SSH_PORT:-2222}"
TEST_USER="testuser"
KEY_DIR="${TMPDIR:-/tmp}/rr-ci-ssh-keys"

start_server() {
    echo "Setting up CI SSH test environment..."

    # Clean up any existing state
    stop_server 2>/dev/null || true

    # Create key directory
    mkdir -p "$KEY_DIR"
    chmod 700 "$KEY_DIR"

    # Generate ephemeral SSH key pair (no passphrase)
    ssh-keygen -t ed25519 -f "$KEY_DIR/id_ed25519" -N "" -C "rr-ci-test" -q
    chmod 600 "$KEY_DIR/id_ed25519"
    chmod 644 "$KEY_DIR/id_ed25519.pub"

    echo "Generated SSH key pair in $KEY_DIR"

    # Start SSH server container
    # Note: linuxserver/openssh-server listens on port 2222 internally by default
    docker run -d \
        --name "$CONTAINER_NAME" \
        -p "${SSH_PORT}:2222" \
        -e PUID=1000 \
        -e PGID=1000 \
        -e TZ=UTC \
        -e USER_NAME="$TEST_USER" \
        -e PUBLIC_KEY="$(cat "$KEY_DIR/id_ed25519.pub")" \
        linuxserver/openssh-server >/dev/null

    echo "Started SSH server container on port $SSH_PORT"

    # Wait for SSH to be ready
    echo -n "Waiting for SSH server..."
    for _ in {1..30}; do
        if docker exec "$CONTAINER_NAME" pgrep -x sshd >/dev/null 2>&1; then
            # Also try a TCP connection
            if nc -z localhost "$SSH_PORT" 2>/dev/null; then
                echo " ready!"
                break
            fi
        fi
        echo -n "."
        sleep 1
    done

    # Add to known_hosts to avoid host key verification prompts
    mkdir -p ~/.ssh
    chmod 700 ~/.ssh

    # Remove any old entries for this host:port
    ssh-keygen -R "[localhost]:$SSH_PORT" 2>/dev/null || true

    # Scan and add the new host key
    for _ in {1..10}; do
        if ssh-keyscan -p "$SSH_PORT" localhost >> ~/.ssh/known_hosts 2>/dev/null; then
            echo "Added host key to known_hosts"
            break
        fi
        sleep 1
    done

    # Verify connection works
    echo -n "Verifying SSH connection..."
    if ssh -i "$KEY_DIR/id_ed25519" \
           -p "$SSH_PORT" \
           -o StrictHostKeyChecking=no \
           -o ConnectTimeout=5 \
           "${TEST_USER}@localhost" "echo ok" >/dev/null 2>&1; then
        echo " success!"
    else
        echo " failed!"
        echo "SSH connection test failed. Container logs:"
        docker logs "$CONTAINER_NAME" 2>&1 | tail -20
        exit 1
    fi

    echo ""
    echo "CI SSH server is ready!"
    echo ""
    echo "To run tests:"
    echo "  eval \$(./scripts/ci-ssh-server.sh env)"
    echo "  go test ./tests/integration/... -v"
    echo ""
    echo "Or in one line:"
    echo "  RR_TEST_SSH_HOST=localhost:$SSH_PORT RR_TEST_SSH_KEY=$KEY_DIR/id_ed25519 RR_TEST_SSH_USER=$TEST_USER go test ./tests/integration/... -v"
}

stop_server() {
    echo "Stopping CI SSH server..."
    docker stop "$CONTAINER_NAME" 2>/dev/null || true
    docker rm "$CONTAINER_NAME" 2>/dev/null || true

    # Clean up keys
    if [ -d "$KEY_DIR" ]; then
        if rm -rf "$KEY_DIR"; then
            echo "Removed SSH keys from $KEY_DIR"
        else
            echo "Warning: Failed to remove SSH keys from $KEY_DIR" >&2
        fi
    fi

    # Remove from known_hosts
    ssh-keygen -R "[localhost]:$SSH_PORT" 2>/dev/null || true

    echo "Done."
}

print_env() {
    if [ ! -f "$KEY_DIR/id_ed25519" ]; then
        echo "Error: SSH key not found. Run './scripts/ci-ssh-server.sh start' first." >&2
        exit 1
    fi

    echo "export RR_TEST_SSH_HOST=localhost:$SSH_PORT"
    echo "export RR_TEST_SSH_KEY=$KEY_DIR/id_ed25519"
    echo "export RR_TEST_SSH_USER=$TEST_USER"
}

case "${1:-start}" in
    start)
        start_server
        ;;
    stop)
        stop_server
        ;;
    env)
        print_env
        ;;
    *)
        echo "Usage: $0 [start|stop|env]"
        echo ""
        echo "Commands:"
        echo "  start  - Start SSH server and generate keys"
        echo "  stop   - Stop server and clean up"
        echo "  env    - Print environment variables for shell eval"
        exit 1
        ;;
esac
