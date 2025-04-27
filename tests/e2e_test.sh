#!/usr/bin/env bats

setup() {
    # Start docker-dns service
    systemctl start docker-dns
    sleep 2 # Wait for service start
    
    # Create test container
    docker run -d --name e2e-test-container nginx:alpine
    sleep 1 # Wait for container start
}

teardown() {
    docker rm -f e2e-test-container || true
    systemctl stop docker-dns || true
}

@test "DNS resolution works" {
    run dig @127.0.0.153 e2e-test-container.docker +short
    [ "$status" -eq 0 ]
    [[ "$output" =~ ([0-9]{1,3}\.){3}[0-9]{1,3} ]]
}

@test "Systemd-resolved integration" {
    run resolvectl query e2e-test-container.docker
    [ "$status" -eq 0 ]
    [[ "$output" =~ "172.17.0." ]]
}

