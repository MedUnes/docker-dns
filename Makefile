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

.PHONY: build test run test-systemd
