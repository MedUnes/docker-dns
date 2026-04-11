#!/bin/bash
set -euo pipefail

# Build a .deb package from a locally compiled binary.
# Usage: ./build-local-deb.sh <path-to-binary> <version>
# Example: ./build-local-deb.sh bin/docker-dns 0.0.0-local

if [ "$#" -lt 2 ]; then
    echo "Usage: $0 <path-to-binary> <version>"
    echo "Example: $0 bin/docker-dns 0.0.0-local"
    exit 1
fi

BINARY_PATH="$1"
VERSION="$2"
PACKAGE_NAME="docker-dns"

# Resolve project root (two levels up from this script)
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
DPKG_DIR="$PROJECT_ROOT/build/dpkg"
STAGING_DIR="$DPKG_DIR/$PACKAGE_NAME"

# Resolve binary path relative to project root if not absolute
if [[ "$BINARY_PATH" != /* ]]; then
    BINARY_PATH="$PROJECT_ROOT/$BINARY_PATH"
fi

if [ ! -f "$BINARY_PATH" ]; then
    echo "ERROR: Binary not found at $BINARY_PATH"
    exit 1
fi

echo "Building .deb package from local binary..."
echo "  Binary: $BINARY_PATH"
echo "  Version: $VERSION"

# Clean previous staging
rm -rf "$STAGING_DIR"
rm -f "$DPKG_DIR"/${PACKAGE_NAME}-*.deb

# Create directory structure
mkdir -p "$STAGING_DIR/usr/bin"

# Copy binary
cp "$BINARY_PATH" "$STAGING_DIR/usr/bin/$PACKAGE_NAME"
chmod +x "$STAGING_DIR/usr/bin/$PACKAGE_NAME"

# Copy DEBIAN control files
cp -r "$DPKG_DIR/DEBIAN" "$STAGING_DIR/"
sed -i "s/__VERSION__/$VERSION/g" "$STAGING_DIR/DEBIAN/control"
chmod -R 755 "$STAGING_DIR/DEBIAN"

# Copy systemd service file
cp -r "$DPKG_DIR/lib" "$STAGING_DIR/"

# Copy config file
cp -r "$DPKG_DIR/etc" "$STAGING_DIR/"

# Build the .deb
DEB_FILE="$DPKG_DIR/${PACKAGE_NAME}-${VERSION}_amd64.deb"
# Use xz compression for compatibility with older dpkg (e.g. Bullseye's 1.20.x,
# which doesn't support the zstd default on newer hosts).
dpkg-deb -Zxz --root-owner-group --build "$STAGING_DIR" "$DEB_FILE"

echo "Package built: $DEB_FILE"

# Clean staging
rm -rf "$STAGING_DIR"
