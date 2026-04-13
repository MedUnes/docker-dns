#!/bin/bash
set -euo pipefail

# Test docker-dns .deb install/uninstall inside a systemd Docker container.
# Usage: ./test-systemd.sh <os-codename>
# Example: ./test-systemd.sh noble

if [ "$#" -lt 1 ]; then
    echo "Usage: $0 <os-codename>"
    echo "Example: $0 noble"
    exit 1
fi

OS="$1"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
DEB_DIR="$PROJECT_ROOT/build/dpkg"
CONTAINER_NAME="docker-dns-test-${OS}"
IMAGE_NAME="docker-dns-test:${OS}"
DOCKERFILE="$SCRIPT_DIR/Dockerfile.${OS}"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
NC='\033[0m' # No Color

PASS_COUNT=0
FAIL_COUNT=0

pass() {
    echo -e "${GREEN}PASS${NC}: $1"
    PASS_COUNT=$((PASS_COUNT + 1))
}

fail() {
    echo -e "${RED}FAIL${NC}: $1"
    FAIL_COUNT=$((FAIL_COUNT + 1))
}

warn() {
    echo -e "${YELLOW}WARN${NC}: $1"
}

# Retry a dig query inside the container until it matches or times out.
# Args: <dig-args> <expected-pattern> <description>
assert_dig_retry() {
    local dig_args="$1"
    local expected_pattern="$2"
    local description="$3"
    local max_attempts=15
    local interval=2

    for ((i=1; i<=max_attempts; i++)); do
        result=$(docker exec "$CONTAINER_NAME" dig $dig_args +short 2>/dev/null || true)
        if echo "$result" | grep -qE "$expected_pattern"; then
            pass "$description -> $result"
            return 0
        fi
        if [ "$i" -lt "$max_attempts" ]; then
            echo "  Attempt $i/$max_attempts: dig $dig_args -> '${result:-empty}', retrying in ${interval}s..."
            sleep $interval
        fi
    done
    fail "$description (got '${result:-empty}', expected match for '$expected_pattern')"
    return 1
}

# Assert a dig query does NOT resolve to an IP.
# dig +short prints timeout/connection-error diagnostics (e.g.
# ";; communications error to 127.0.0.53#53: timed out") to stdout, so a simple
# emptiness check would misread those as a resolution. Only a bare IPv4 line
# counts as "resolved" — everything else means "not resolving," which is what
# this helper is checking for.
assert_dig_empty() {
    local dig_args="$1"
    local description="$2"

    result=$(docker exec "$CONTAINER_NAME" dig $dig_args +short 2>/dev/null || true)
    ips=$(echo "$result" | grep -oE '^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$' || true)
    if [ -z "$ips" ]; then
        pass "$description -> not resolving (as expected)"
    else
        fail "$description (expected no IP, got '$ips')"
    fi
}

# Cleanup function — always runs on exit
cleanup() {
    echo ""
    echo "=== Cleanup ==="
    docker stop "$CONTAINER_NAME" 2>/dev/null || true
    docker rm "$CONTAINER_NAME" 2>/dev/null || true
    echo "Container removed."
}
trap cleanup EXIT

# Wait for a systemd service to become active inside the container.
# Args: <service-name> <timeout-seconds>
wait_for_service() {
    local service="$1"
    local timeout="${2:-30}"
    local interval=2
    local elapsed=0

    while ! docker exec "$CONTAINER_NAME" systemctl is-active --quiet "$service" 2>/dev/null; do
        if [ "$elapsed" -ge "$timeout" ]; then
            echo "Timeout waiting for $service to start."
            docker exec "$CONTAINER_NAME" systemctl status "$service" 2>/dev/null || true
            docker exec "$CONTAINER_NAME" journalctl -u "$service" --no-pager -n 20 2>/dev/null || true
            return 1
        fi
        echo "  Waiting for $service... (${elapsed}s / ${timeout}s)"
        sleep $interval
        elapsed=$((elapsed + interval))
    done
    echo "  $service is active."
}

# =====================================================================
# Phase 0: Pre-flight
# =====================================================================
echo "=== Phase 0: Pre-flight checks ==="

if ! command -v docker &>/dev/null; then
    echo "ERROR: docker CLI not found. Install Docker to run this test."
    exit 1
fi

if [ ! -f "$DOCKERFILE" ]; then
    echo "ERROR: Dockerfile not found: $DOCKERFILE"
    exit 1
fi

DEB_FILE=$(ls "$DEB_DIR"/docker-dns-*.deb 2>/dev/null | head -1)
if [ -z "$DEB_FILE" ]; then
    echo "ERROR: No .deb file found in $DEB_DIR. Run build-local-deb.sh first."
    exit 1
fi
echo "Using .deb: $DEB_FILE"

# =====================================================================
# Phase 1: Build the test image
# =====================================================================
echo ""
echo "=== Phase 1: Build test image ==="
docker build -t "$IMAGE_NAME" -f "$DOCKERFILE" "$SCRIPT_DIR"

# =====================================================================
# Phase 2: Start systemd container
# =====================================================================
echo ""
echo "=== Phase 2: Start systemd container ==="

# Remove any leftover container from a previous run
docker rm -f "$CONTAINER_NAME" 2>/dev/null || true

docker run -d \
    --name "$CONTAINER_NAME" \
    --privileged \
    --cgroupns=host \
    -v "$DEB_DIR":/packages:ro \
    "$IMAGE_NAME"

echo "Waiting for systemd to initialize..."
sleep 5

# Wait for systemd to reach a usable state
timeout=30
elapsed=0
while true; do
    state=$(docker exec "$CONTAINER_NAME" systemctl is-system-running 2>/dev/null | tr -d '[:space:]' || echo "not-ready")
    if [ "$state" = "running" ] || [ "$state" = "degraded" ]; then
        echo "  systemd state: $state"
        break
    fi
    if [ "$elapsed" -ge "$timeout" ]; then
        echo "Timeout waiting for systemd. State: $state"
        docker exec "$CONTAINER_NAME" systemctl list-units --failed 2>/dev/null || true
        exit 1
    fi
    echo "  Waiting for systemd... state=$state (${elapsed}s / ${timeout}s)"
    sleep 2
    elapsed=$((elapsed + 2))
done

# =====================================================================
# Phase 3: Install Docker inside the container
# =====================================================================
echo ""
echo "=== Phase 3: Install Docker inside container ==="

docker exec "$CONTAINER_NAME" bash -c '
    export DEBIAN_FRONTEND=noninteractive
    apt-get update -qq
    apt-get install -y --no-install-recommends docker.io dnsutils >/dev/null 2>&1
    # Trixie+ splits the CLI into a separate package
    if ! command -v docker &>/dev/null; then
        apt-get install -y --no-install-recommends docker-cli >/dev/null 2>&1
    fi
'

# Docker-in-Docker: overlay-on-overlay fails, use vfs storage driver
docker exec "$CONTAINER_NAME" mkdir -p /etc/docker
docker exec "$CONTAINER_NAME" bash -c 'echo "{\"storage-driver\": \"vfs\"}" > /etc/docker/daemon.json'

docker exec "$CONTAINER_NAME" systemctl enable --now docker
echo "Waiting for Docker service..."
wait_for_service "docker.service" 30
docker exec "$CONTAINER_NAME" docker info --format '{{.ServerVersion}}' 2>/dev/null && echo "Docker is running inside the container."

# =====================================================================
# Phase 4: Install the .deb package
# =====================================================================
echo ""
echo "=== Phase 4: Install docker-dns .deb ==="

docker exec "$CONTAINER_NAME" bash -c 'dpkg -i /packages/docker-dns-*.deb'

echo "Waiting for docker-dns service..."
wait_for_service "docker-dns.service" 15

# =====================================================================
# Phase 5: Post-install assertions
# =====================================================================
echo ""
echo "=== Phase 5: Post-install assertions ==="

# Service assertions
if docker exec "$CONTAINER_NAME" systemctl is-active --quiet docker-dns; then
    pass "docker-dns service is active"
else
    fail "docker-dns service is NOT active"
    docker exec "$CONTAINER_NAME" journalctl -u docker-dns --no-pager -n 30 2>/dev/null || true
fi

if docker exec "$CONTAINER_NAME" systemctl is-enabled --quiet docker-dns; then
    pass "docker-dns service is enabled"
else
    fail "docker-dns service is NOT enabled"
fi

# Config assertion — detect which of the three postinst branches was taken.
# Mirrors postinst's logic: resolved first, then NetworkManager, then plain resolv.conf.
MODE=resolvconf
if docker exec "$CONTAINER_NAME" systemctl is-active --quiet systemd-resolved 2>/dev/null; then
    MODE=resolved
elif docker exec "$CONTAINER_NAME" systemctl is-active --quiet NetworkManager 2>/dev/null; then
    MODE=nm
fi
echo "Detected resolver-integration mode: $MODE"

case "$MODE" in
    resolved)
        if docker exec "$CONTAINER_NAME" test -f /etc/systemd/resolved.conf.d/docker-dns.conf; then
            pass "resolved drop-in config exists"
            if docker exec "$CONTAINER_NAME" grep -q 'Domains=.*~docker' /etc/systemd/resolved.conf.d/docker-dns.conf; then
                pass "resolved drop-in contains Domains=~docker"
            else
                fail "resolved drop-in missing Domains=~docker"
                docker exec "$CONTAINER_NAME" cat /etc/systemd/resolved.conf.d/docker-dns.conf
            fi
        else
            fail "resolved drop-in config does NOT exist"
        fi
        ;;
    nm)
        if docker exec "$CONTAINER_NAME" test -x /etc/NetworkManager/dispatcher.d/docker-dns; then
            pass "NM dispatcher script exists and is executable"
        else
            fail "NM dispatcher script missing or not executable"
            docker exec "$CONTAINER_NAME" ls -l /etc/NetworkManager/dispatcher.d/ 2>/dev/null || true
        fi
        if docker exec "$CONTAINER_NAME" grep -q 'nameserver 127.0.0.153' /etc/resolv.conf; then
            pass "/etc/resolv.conf contains nameserver 127.0.0.153"
        else
            fail "/etc/resolv.conf missing nameserver 127.0.0.153"
            docker exec "$CONTAINER_NAME" cat /etc/resolv.conf
        fi
        ;;
    resolvconf)
        if docker exec "$CONTAINER_NAME" grep -q 'nameserver 127.0.0.153' /etc/resolv.conf; then
            pass "/etc/resolv.conf contains nameserver 127.0.0.153"
        else
            fail "/etc/resolv.conf missing nameserver 127.0.0.153"
            docker exec "$CONTAINER_NAME" cat /etc/resolv.conf
        fi
        ;;
esac

# Start a test Docker container for DNS resolution tests
echo ""
echo "Starting test container for DNS resolution..."
docker exec "$CONTAINER_NAME" docker run -d --name test-nginx nginx:alpine
sleep 2

# Direct resolution (bypassing system resolver, querying docker-dns directly)
echo ""
echo "--- Direct resolution tests (dig @127.0.0.153) ---"
assert_dig_retry "@127.0.0.153 test-nginx.docker" "^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$" \
    "Direct: dig @127.0.0.153 test-nginx.docker resolves to an IP"

assert_dig_retry "@127.0.0.153 google.com" "^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$" \
    "Direct: dig @127.0.0.153 google.com forwards correctly"

# System-level resolution (through /etc/resolv.conf -> systemd-resolved -> routing domain -> docker-dns)
echo ""
echo "--- System-level resolution tests (dig without @) ---"
assert_dig_retry "test-nginx.docker" "^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$" \
    "System: dig test-nginx.docker resolves through system resolver"

# =====================================================================
# Phase 5b: NetworkManager survivability (desktop variants only)
# =====================================================================
# On real Debian Desktop, NetworkManager writes /etc/resolv.conf directly (dns=default
# plugin) and overwrites docker-dns's prepended nameserver on restart/DHCP-renewal.
# On Ubuntu Desktop, NM delegates DNS to systemd-resolved, so the ~docker routing
# domain survives. We exercise the system resolver (bare `dig`, no @127.0.0.153) so
# this assertion fails precisely when NM has clobbered docker-dns from resolv.conf.
if docker exec "$CONTAINER_NAME" systemctl is-active --quiet NetworkManager 2>/dev/null; then
    echo ""
    echo "=== Phase 5b: NetworkManager survivability ==="
    echo "NetworkManager is active — restarting it and re-checking system resolver"
    docker exec "$CONTAINER_NAME" systemctl restart NetworkManager
    sleep 3
    assert_dig_retry "test-nginx.docker" "^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$" \
        "docker-dns resolution survives NetworkManager restart"
fi

# =====================================================================
# Phase 6: Uninstall the .deb package
# =====================================================================
echo ""
echo "=== Phase 6: Uninstall docker-dns ==="

docker exec "$CONTAINER_NAME" dpkg -r docker-dns

# Give systemd a moment to process the stop
sleep 2

# =====================================================================
# Phase 7: Post-uninstall assertions
# =====================================================================
echo ""
echo "=== Phase 7: Post-uninstall assertions ==="

# Binary removed
if docker exec "$CONTAINER_NAME" test ! -f /usr/bin/docker-dns; then
    pass "Binary /usr/bin/docker-dns removed"
else
    fail "Binary /usr/bin/docker-dns still exists"
fi

# Service file removed
if docker exec "$CONTAINER_NAME" test ! -f /lib/systemd/system/docker-dns.service; then
    pass "Service file removed"
else
    fail "Service file still exists"
fi

# Service stopped
if ! docker exec "$CONTAINER_NAME" systemctl is-active --quiet docker-dns 2>/dev/null; then
    pass "docker-dns service is not active"
else
    fail "docker-dns service is still active after uninstall"
fi

# DNS config cleaned — branch on the same MODE detected pre-install.
case "$MODE" in
    resolved)
        if docker exec "$CONTAINER_NAME" test ! -f /etc/systemd/resolved.conf.d/docker-dns.conf; then
            pass "resolved drop-in config removed"
        else
            fail "resolved drop-in config still exists after uninstall"
        fi
        ;;
    nm)
        if docker exec "$CONTAINER_NAME" test ! -e /etc/NetworkManager/dispatcher.d/docker-dns; then
            pass "NM dispatcher script removed"
        else
            fail "NM dispatcher script still exists after uninstall"
        fi
        if ! docker exec "$CONTAINER_NAME" grep -q 'nameserver 127.0.0.153' /etc/resolv.conf 2>/dev/null; then
            pass "nameserver 127.0.0.153 removed from /etc/resolv.conf"
        else
            fail "nameserver 127.0.0.153 still in /etc/resolv.conf after uninstall"
            docker exec "$CONTAINER_NAME" cat /etc/resolv.conf
        fi
        ;;
    resolvconf)
        if ! docker exec "$CONTAINER_NAME" grep -q 'nameserver 127.0.0.153' /etc/resolv.conf 2>/dev/null; then
            pass "nameserver 127.0.0.153 removed from /etc/resolv.conf"
        else
            fail "nameserver 127.0.0.153 still in /etc/resolv.conf after uninstall"
            docker exec "$CONTAINER_NAME" cat /etc/resolv.conf
        fi
        ;;
esac

# System DNS still works after uninstall
echo ""
echo "--- Post-uninstall DNS checks ---"
assert_dig_retry "google.com" "^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$" \
    "System DNS still works after uninstall (dig google.com)"

# Docker container resolution should be gone
assert_dig_empty "test-nginx.docker" \
    "dig test-nginx.docker returns empty after uninstall"

# =====================================================================
# Summary
# =====================================================================
echo ""
echo "========================================"
echo -e "Results: ${GREEN}${PASS_COUNT} passed${NC}, ${RED}${FAIL_COUNT} failed${NC}"
echo "========================================"

if [ "$FAIL_COUNT" -gt 0 ]; then
    exit 1
fi
