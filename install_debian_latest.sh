#!/bin/bash
set -euo pipefail # Exit immediately if a command exits with a non-zero status,
                  # exit if a variable is used unset,
                  # exit if a piped command fails.

# Define the GitHub repository details
OWNER="MedUnes"
REPO="docker-dns"
DEB_PATTERN="_amd64.deb"         # Pattern for the debian package asset
CHECKSUM_PATTERN="_checksums.txt" # Pattern for the checksum file asset

# --- Check for dependencies ---
if ! command -v curl &> /dev/null; then
    echo "Error: curl is not installed. Please install it."
    exit 1
fi

if ! command -v jq &> /dev/null; then
    echo "Error: jq is not installed. Please install it (e.g., sudo apt-get install jq)."
    exit 1
fi

if ! command -v sha256sum &> /dev/null; then
    echo "Error: sha256sum is not installed. Please install it (usually part of coreutils)."
    exit 1
fi
# --- End dependency check ---


echo "Fetching latest release information for $OWNER/$REPO..."
# Get the latest release information from the GitHub API
LATEST_RELEASE_INFO=$(curl -s "https://api.github.com/repos/$OWNER/$REPO/releases/latest")

# Check if curl failed or returned empty
if [ -z "$LATEST_RELEASE_INFO" ]; then
  echo "Error: Failed to fetch latest release information from GitHub API."
  exit 1
fi

# Extract the download URL for the amd64 debian package using jq
DEB_URL=$(echo "$LATEST_RELEASE_INFO" | jq -r ".assets[] | select(.name | endswith(\"$DEB_PATTERN\")) | .browser_download_url")

# Extract the download URL for the checksum file using jq
CHECKSUM_URL=$(echo "$LATEST_RELEASE_INFO" | jq -r ".assets[] | select(.name | endswith(\"$CHECKSUM_PATTERN\")) | .browser_download_url")


# Check if both download URLs were found
if [ -z "$DEB_URL" ]; then
  echo "Error: Could not find the latest release asset matching the pattern '$DEB_PATTERN'."
  exit 1
fi

if [ -z "$CHECKSUM_URL" ]; then
  echo "Error: Could not find the latest release asset matching the pattern '$CHECKSUM_PATTERN'."
  exit 1
fi

# Determine filenames from URLs
DEB_FILE=$(basename "$DEB_URL")
CHECKSUM_FILE=$(basename "$CHECKSUM_URL")

echo "Found latest package: $DEB_FILE"
echo "Found latest checksum file: $CHECKSUM_FILE"

# --- Download files ---
echo "Downloading package: $DEB_FILE"
if ! wget -q -O "$DEB_FILE" "$DEB_URL"; then
    echo "Error: Failed to download package."
    exit 1
fi

echo "Downloading checksum file: $CHECKSUM_FILE"
if ! wget -q -O "$CHECKSUM_FILE" "$CHECKSUM_URL"; then
    echo "Error: Failed to download checksum file."
    # Clean up downloaded deb package
    rm -f "$DEB_FILE"
    exit 1
fi
# --- End download ---


# --- Verify checksum ---
echo "Verifying checksum..."

# Read the expected checksum from the downloaded checksum file
# Assumes the checksum file format is "checksum filename"
EXPECTED_CHECKSUM=$(grep "$DEB_FILE" "$CHECKSUM_FILE" | awk '{print $1}')

if [ -z "$EXPECTED_CHECKSUM" ]; then
    echo "Error: Could not find checksum for '$DEB_FILE' in '$CHECKSUM_FILE'."
    # Clean up downloaded files
    rm -f "$DEB_FILE" "$CHECKSUM_FILE"
    exit 1
fi

# Calculate the actual checksum of the downloaded deb file
ACTUAL_CHECKSUM=$(sha256sum "$DEB_FILE" | awk '{print $1}')

# Compare the checksums
if [ "$ACTUAL_CHECKSUM" = "$EXPECTED_CHECKSUM" ]; then
    echo "Checksum verification successful."
else
    echo "Error: Checksum verification failed!"
    echo "  Expected: $EXPECTED_CHECKSUM"
    echo "  Actual:   $ACTUAL_CHECKSUM"
    # Clean up downloaded files
    rm -f "$DEB_FILE" "$CHECKSUM_FILE"
    exit 1
fi
# --- End verification ---


# --- Install package ---
echo "Installing package: $DEB_FILE"
# Using sudo requires the user to potentially enter their password
if ! sudo dpkg -i "$DEB_FILE"; then
    echo "Error: Failed to install package using dpkg."
    # Keep the downloaded file for inspection if installation fails
    # rm -f "$DEB_FILE"
    rm -f "$CHECKSUM_FILE" # Clean up checksum file
    exit 1
fi
# --- End install ---

echo "Package installed successfully."

# Clean up downloaded files after successful installation
echo "Cleaning up downloaded files."
rm -f "$DEB_FILE" "$CHECKSUM_FILE"

exit 0