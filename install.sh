#!/usr/bin/env bash
# brizz-code installer
# Usage: curl -fsSL https://raw.githubusercontent.com/brizzai/brizz-code/main/install.sh | bash
# Private repo: GITHUB_TOKEN=ghp_xxx curl -fsSL -H "Authorization: Bearer $GITHUB_TOKEN" \
#   https://raw.githubusercontent.com/brizzai/brizz-code/main/install.sh | GITHUB_TOKEN=ghp_xxx bash

set -euo pipefail

REPO="brizzai/brizz-code"
INSTALL_DIR="${HOME}/.local/bin"
VERSION=""

# Parse args
while [[ $# -gt 0 ]]; do
    case $1 in
        --version) VERSION="$2"; shift 2 ;;
        --dir) INSTALL_DIR="$2"; shift 2 ;;
        *) echo "Unknown option: $1"; exit 1 ;;
    esac
done

# macOS only
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
if [[ "$OS" != "darwin" ]]; then
    echo "Error: brizz-code only supports macOS"
    exit 1
fi

# Detect architecture
ARCH=$(uname -m)
case "$ARCH" in
    x86_64|amd64) ARCH="amd64" ;;
    arm64|aarch64) ARCH="arm64" ;;
    *) echo "Error: Unsupported architecture: $ARCH"; exit 1 ;;
esac

echo "brizz-code installer"
echo "Platform: ${OS}/${ARCH}"

# Auth header for private repos
AUTH_ARGS=()
if [[ -n "${GITHUB_TOKEN:-}" ]]; then
    AUTH_ARGS=(-H "Authorization: Bearer $GITHUB_TOKEN")
fi

# Resolve version
if [[ -z "$VERSION" ]]; then
    VERSION=$(curl -fsSL "${AUTH_ARGS[@]}" "https://api.github.com/repos/${REPO}/releases/latest" 2>/dev/null \
        | grep '"tag_name"' | sed -E 's/.*"([^"]+)".*/\1/')

    if [[ -z "$VERSION" ]]; then
        echo "Error: Could not determine latest version."
        echo "For private repos, set GITHUB_TOKEN."
        exit 1
    fi
fi

VERSION_NUM="${VERSION#v}"
echo "Version: ${VERSION}"

# Download
ARCHIVE_NAME="brizz-code_${VERSION_NUM}_${OS}_${ARCH}.tar.gz"
DOWNLOAD_URL="https://github.com/${REPO}/releases/download/${VERSION}/${ARCHIVE_NAME}"
TMP_DIR=$(mktemp -d)
trap 'rm -rf "$TMP_DIR"' EXIT

echo "Downloading ${ARCHIVE_NAME}..."

if [[ -n "${GITHUB_TOKEN:-}" ]]; then
    # Private repo: use GitHub API to download release asset
    ASSET_URL=$(curl -fsSL "${AUTH_ARGS[@]}" \
        "https://api.github.com/repos/${REPO}/releases/tags/${VERSION}" 2>/dev/null \
        | grep -B2 "\"name\": \"${ARCHIVE_NAME}\"" | grep '"url"' | sed -E 's/.*"(https[^"]+)".*/\1/')

    if [[ -n "$ASSET_URL" ]]; then
        curl -fsSL "${AUTH_ARGS[@]}" -H "Accept: application/octet-stream" \
            "$ASSET_URL" -o "$TMP_DIR/$ARCHIVE_NAME"
    else
        curl -fsSL -L "${AUTH_ARGS[@]}" "$DOWNLOAD_URL" -o "$TMP_DIR/$ARCHIVE_NAME"
    fi
else
    curl -fsSL -L "$DOWNLOAD_URL" -o "$TMP_DIR/$ARCHIVE_NAME"
fi

# Extract and install
tar -xzf "$TMP_DIR/$ARCHIVE_NAME" -C "$TMP_DIR"
mkdir -p "$INSTALL_DIR"
mv "$TMP_DIR/brizz-code" "$INSTALL_DIR/brizz-code"
chmod +x "$INSTALL_DIR/brizz-code"

# Check PATH
if [[ ":$PATH:" != *":$INSTALL_DIR:"* ]]; then
    echo ""
    echo "Note: $INSTALL_DIR is not in your PATH."
    echo "Add to your shell config:"
    echo "  export PATH=\"\$HOME/.local/bin:\$PATH\""
fi

# Check dependencies
echo ""
if command -v tmux &>/dev/null; then
    echo "$(tmux -V 2>/dev/null) [OK]"
else
    echo "Warning: tmux is not installed (required)"
    echo "  brew install tmux"
fi

if command -v git &>/dev/null; then
    echo "git $(git --version 2>/dev/null | cut -d' ' -f3) [OK]"
else
    echo "Warning: git is not installed (required)"
fi

echo ""
echo "Installed brizz-code ${VERSION} to ${INSTALL_DIR}/brizz-code"
echo ""
echo "Get started:"
echo "  brizz-code           # Launch TUI"
echo "  brizz-code --version # Check version"
