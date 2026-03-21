#!/usr/bin/env bash
# brizz-code installer (requires gh CLI)
# Usage: curl -fsSL https://raw.githubusercontent.com/brizzai/brizz-code/master/install.sh | bash
#    or: gh repo clone brizzai/brizz-code /tmp/bc && bash /tmp/bc/install.sh

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

# Require gh CLI
if ! command -v gh &>/dev/null; then
    echo "Error: gh CLI is required. Install it: brew install gh"
    exit 1
fi

if ! gh auth status &>/dev/null; then
    echo "Error: gh CLI is not authenticated. Run: gh auth login"
    exit 1
fi

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

# Resolve version
if [[ -z "$VERSION" ]]; then
    VERSION=$(gh release view --repo "$REPO" --json tagName --jq '.tagName' 2>/dev/null || true)
    if [[ -z "$VERSION" ]]; then
        echo "Error: Could not determine latest version. Check repo access."
        exit 1
    fi
fi

VERSION_NUM="${VERSION#v}"
echo "Version: ${VERSION}"

# Download
ARCHIVE_NAME="brizz-code_${VERSION_NUM}_${OS}_${ARCH}.tar.gz"
TMP_DIR=$(mktemp -d)
trap 'rm -rf "$TMP_DIR"' EXIT

echo "Downloading ${ARCHIVE_NAME}..."
gh release download "$VERSION" --repo "$REPO" --pattern "$ARCHIVE_NAME" --dir "$TMP_DIR"

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
