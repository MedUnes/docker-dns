debug:
	dlv debug --headless --listen=:2345 --api-version=2 --accept-multiclient --log
test:
	go run gotest.tools/gotestsum@latest --format=testdox
build:
	go build -ldflags="-s -w" -o bin/docker-dns
	@echo "binary built and generated at bin/docker-dns"
run:
	go run main.go
.PHONY: build
