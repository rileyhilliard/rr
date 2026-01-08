#!/bin/bash
#
# Runs a Docker SSH server for integration testing.
# Use this when you don't want to enable local SSH or need a clean test environment.
#
# Usage:
#   ./scripts/test-ssh-server.sh        # Start the server
#   ./scripts/test-ssh-server.sh stop   # Stop and remove the container
#
# Then run tests with:
#   RR_TEST_SSH_HOST=localhost:2222 go test ./tests/integration/... -v

set -e

CONTAINER_NAME="rr-test-ssh"
SSH_PORT="2222"

case "${1:-start}" in
  start)
    # Check if container already exists
    if docker ps -a --format '{{.Names}}' | grep -q "^${CONTAINER_NAME}$"; then
      echo "Container ${CONTAINER_NAME} already exists."
      if docker ps --format '{{.Names}}' | grep -q "^${CONTAINER_NAME}$"; then
        echo "It's already running on port ${SSH_PORT}."
      else
        echo "Starting existing container..."
        docker start "${CONTAINER_NAME}"
      fi
    else
      echo "Starting SSH test server..."
      docker run -d \
        --name "${CONTAINER_NAME}" \
        -p "${SSH_PORT}:22" \
        -e PUID=1000 \
        -e PGID=1000 \
        -e TZ=UTC \
        -e PASSWORD_ACCESS=true \
        -e USER_PASSWORD=testpassword \
        -e USER_NAME=testuser \
        linuxserver/openssh-server

      echo "Waiting for SSH server to start..."
      sleep 3
    fi

    echo ""
    echo "SSH test server running on localhost:${SSH_PORT}"
    echo ""
    echo "To run integration tests:"
    echo "  RR_TEST_SSH_HOST=localhost:${SSH_PORT} go test ./tests/integration/... -v"
    echo ""
    echo "To stop the server:"
    echo "  ./scripts/test-ssh-server.sh stop"
    ;;

  stop)
    echo "Stopping SSH test server..."
    docker stop "${CONTAINER_NAME}" 2>/dev/null || true
    docker rm "${CONTAINER_NAME}" 2>/dev/null || true
    echo "Done."
    ;;

  *)
    echo "Usage: $0 [start|stop]"
    exit 1
    ;;
esac
