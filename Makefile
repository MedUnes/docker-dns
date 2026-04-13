OS ?= noble

test:
	go run gotest.tools/gotestsum@latest --format=testdox
build:
	CGO_ENABLED=0 go build -ldflags="-s -w" -o bin/docker-dns
	@echo "binary built and generated at bin/docker-dns"
run:
	go run main.go

test-systemd: build
	./test/systemd/build-local-deb.sh bin/docker-dns 0.0.0-local
	./test/systemd/test-systemd.sh $(OS)

test-systemd-all: build
	@for os in noble jammy focal trixie bookworm bullseye noble-desktop jammy-desktop focal-desktop trixie-desktop bookworm-desktop bullseye-desktop; do \
		echo "=== Testing $$os ==="; \
		./test/systemd/build-local-deb.sh bin/docker-dns 0.0.0-local && \
		./test/systemd/test-systemd.sh $$os || echo "FAILED: $$os"; \
	done

test-systemd-desktop-all: build
	@for os in noble-desktop jammy-desktop focal-desktop trixie-desktop bookworm-desktop bullseye-desktop; do \
		echo "=== Testing $$os ==="; \
		./test/systemd/build-local-deb.sh bin/docker-dns 0.0.0-local && \
		./test/systemd/test-systemd.sh $$os || echo "FAILED: $$os"; \
	done

.PHONY: build test run test-systemd test-systemd-all test-systemd-desktop-all
