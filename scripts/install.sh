#!/bin/bash
set -e

# rr installer script
# Downloads and installs the latest release of rr (Road Runner CLI)
# Usage: curl -sSL https://raw.githubusercontent.com/rileyhilliard/rr/main/scripts/install.sh | bash

REPO="rileyhilliard/rr"
INSTALL_DIR="/usr/local/bin"

# Detect OS
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$OS" in
  darwin|linux)
    ;;
  *)
    echo "Error: Unsupported operating system: $OS"
    echo "Supported: darwin (macOS), linux"
    exit 1
    ;;
esac

# Detect architecture
ARCH=$(uname -m)
case "$ARCH" in
  x86_64|amd64)
    ARCH="amd64"
    ;;
  aarch64|arm64)
    ARCH="arm64"
    ;;
  *)
    echo "Error: Unsupported architecture: $ARCH"
    echo "Supported: amd64 (x86_64), arm64 (aarch64)"
    exit 1
    ;;
esac

echo "Detecting system: ${OS}/${ARCH}"

# Get latest version from GitHub API
echo "Fetching latest release..."
VERSION=$(curl -sL "https://api.github.com/repos/${REPO}/releases/latest" | grep '"tag_name":' | cut -d'"' -f4)

if [ -z "$VERSION" ]; then
  echo "Error: Could not fetch latest version from GitHub"
  echo "Check your internet connection or try again later"
  exit 1
fi

echo "Latest version: ${VERSION}"

# Construct download URL
FILENAME="rr_${OS}_${ARCH}.tar.gz"
URL="https://github.com/${REPO}/releases/download/${VERSION}/${FILENAME}"

echo "Downloading ${URL}..."

# Create temp directory for download
TMP_DIR=$(mktemp -d)
trap 'rm -rf "$TMP_DIR"' EXIT

# Download and extract
if ! curl -sL "$URL" -o "${TMP_DIR}/${FILENAME}"; then
  echo "Error: Failed to download ${URL}"
  exit 1
fi

if ! tar xzf "${TMP_DIR}/${FILENAME}" -C "$TMP_DIR"; then
  echo "Error: Failed to extract archive"
  exit 1
fi

# Find the binary (could be at root or in a subdirectory)
BINARY=$(find "$TMP_DIR" -name "rr" -type f -perm +111 | head -1)
if [ -z "$BINARY" ]; then
  # Try without execute permission check for freshly extracted files
  BINARY=$(find "$TMP_DIR" -name "rr" -type f | head -1)
fi

if [ -z "$BINARY" ]; then
  echo "Error: Could not find rr binary in archive"
  exit 1
fi

# Make sure it's executable
chmod +x "$BINARY"

# Install to destination
echo "Installing to ${INSTALL_DIR}/rr..."
if [ -w "$INSTALL_DIR" ]; then
  mv "$BINARY" "${INSTALL_DIR}/rr"
else
  echo "Requesting sudo to install to ${INSTALL_DIR}..."
  sudo mv "$BINARY" "${INSTALL_DIR}/rr"
fi

echo ""
echo "Successfully installed rr ${VERSION}"
"${INSTALL_DIR}/rr" --version
echo ""
echo "Run 'rr --help' to get started"
