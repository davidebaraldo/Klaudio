#!/usr/bin/env bash
# Klaudio Linux install script
# Downloads the latest release binary and installs it as a systemd service.
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/davidebaraldo/Klaudio/main/scripts/install.sh | sudo bash
#
# Or with a specific version:
#   curl -fsSL ... | sudo bash -s -- v0.3.0

set -euo pipefail

REPO="davidebaraldo/Klaudio"
INSTALL_DIR="/usr/local/bin"
BINARY_NAME="klaudio"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

info()  { echo -e "${GREEN}[INFO]${NC} $*"; }
warn()  { echo -e "${YELLOW}[WARN]${NC} $*"; }
error() { echo -e "${RED}[ERROR]${NC} $*" >&2; exit 1; }

# Check root
[[ $EUID -eq 0 ]] || error "This script must be run as root (use sudo)"

# Check architecture
ARCH=$(uname -m)
case "$ARCH" in
    x86_64|amd64) GOARCH="amd64" ;;
    aarch64|arm64) GOARCH="arm64" ;;
    *) error "Unsupported architecture: $ARCH" ;;
esac

GOOS="linux"

# Determine version
VERSION="${1:-latest}"
if [[ "$VERSION" == "latest" ]]; then
    info "Fetching latest release..."
    VERSION=$(curl -fsSL "https://api.github.com/repos/$REPO/releases/latest" | grep '"tag_name"' | sed 's/.*: "//;s/".*//')
    [[ -n "$VERSION" ]] || error "Could not determine latest version"
fi
info "Installing Klaudio $VERSION ($GOOS/$GOARCH)"

# Download
DOWNLOAD_URL="https://github.com/$REPO/releases/download/$VERSION/klaudio-${GOOS}-${GOARCH}"
TMP_FILE=$(mktemp)
trap "rm -f $TMP_FILE" EXIT

info "Downloading from $DOWNLOAD_URL..."
curl -fsSL -o "$TMP_FILE" "$DOWNLOAD_URL" || error "Download failed. Check if version $VERSION exists."

# Verify it's an executable
file "$TMP_FILE" | grep -q "ELF" || error "Downloaded file is not a valid Linux binary"

# Install binary
info "Installing to $INSTALL_DIR/$BINARY_NAME..."
install -m 755 "$TMP_FILE" "$INSTALL_DIR/$BINARY_NAME"

# Verify
INSTALLED_VERSION=$("$INSTALL_DIR/$BINARY_NAME" --version 2>&1 || true)
info "Installed: $INSTALLED_VERSION"

# Check Docker
if command -v docker &>/dev/null && docker info &>/dev/null; then
    info "Docker is available"
else
    warn "Docker is not running. Klaudio requires Docker to manage agent containers."
    warn "Install Docker: curl -fsSL https://get.docker.com | sh"
fi

# Install as service
info "Installing as systemd service..."
"$INSTALL_DIR/$BINARY_NAME" service install

echo ""
info "Installation complete!"
echo ""
echo "Next steps:"
echo "  1. Edit the config:     sudo nano /etc/klaudio/config.yaml"
echo "  2. Start the service:   sudo klaudio service start"
echo "  3. Check status:        sudo klaudio service status"
echo "  4. View logs:           journalctl -u klaudio -f"
echo "  5. Open the web UI:     http://localhost:8080"
