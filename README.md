# Docker DNS Resolver

[![Tests](https://github.com/MedUnes/docker-dns/actions/workflows/test.yml/badge.svg)](https://github.com/MedUnes/docker-dns/actions/workflows/test.yml) [![Release](https://github.com/MedUnes/docker-dns/actions/workflows/release.yml/badge.svg)](https://github.com/MedUnes/docker-dns/actions/workflows/release.yml) [![Go Report Card](https://goreportcard.com/badge/github.com/medunes/docker-dns)](https://goreportcard.com/report/github.com/medunes/docker-dns) [![License](https://img.shields.io/github/license/medunes/docker-dns)](LICENSE) [![Go Reference](https://pkg.go.dev/badge/github.com/medunes/docker-dns.svg)](https://pkg.go.dev/github.com/medunes/docker-dns)[![Latest version of 'docker-dns' @ Cloudsmith](https://api-prd.cloudsmith.io/v1/badges/version/medunes-fchf/docker-dns/deb/docker-dns/latest/a=all;xc=main;d=debian%252Fany-version;t=binary/?render=true&show_latest=true)](https://cloudsmith.io/~medunes-fchf/repos/docker-dns/packages/detail/deb/docker-dns/latest/a=all;xc=main;d=debian%252Fany-version;t=binary/)

![Logo](./logo.png)

A lightweight DNS server that resolves Docker container names to their IP addresses, enabling seamless networking between containers and the host.

## Features

- **Automatic DNS Resolution**: Resolve Docker container names with a custom TLD to their respective IP addresses.
- **Caching Mechanism**: Configurable TTL for DNS cache entries to improve performance.
- **Simple Configuration**: Minimal setup with command-line options or a configuration file.
- **Debian Package**: Easy-to-install `.deb` package for seamless integration.
- **Fallback DNS**: Forwards non-Docker queries to configurable DNS servers.
- **Lightweight and Fast**: Built with Go for high performance.

## Installation for Linux/Debian:

### Installation with APT package manager (recommended)
* To install packages, you can quickly setup the repository automatically

1. **Add docker-dns to your debian package list**:

```bash
curl -1sLf 'https://dl.cloudsmith.io/public/medunes-fchf/docker-dns/setup.deb.sh' | sudo -E bash
```

2. **Install `docker-dns` package**:

```bash
sudo apt-get install docker-dns
```

### Download and Install the Debian Package manually

1. **Download the `.deb` Package**:
   - Go to the [Releases](https://github.com/MedUnes/docker-dns/releases) page.
   - Find the release matching your desired version (e.g., `v1.0.0`).
   - Download the `.deb` file:
     ```
     wget https://github.com/MedUnes/docker-dns/releases/download/v1.0.0/docker-dns-1.0.0_amd64.deb
     ```

2. **Install the Package**:
   ```bash
   sudo dpkg -i docker-dns-1.0.0_amd64.deb
   ```
## Configuration

1. **Check the Service Status**:
   - Ensure the service is running:
     ```bash
     systemctl status docker-dns
     ```

2. **Edit the Configuration (Optional)**:
   - The configuration file is located at `/etc/docker-dns/docker-dns.conf`.
   - Example:
     ```ini
     # Docker DNS Configuration
     PORT=5335
     TTL=300
     TLD=docker
     DEFAULT_RESOLVER=8.8.8.8,1.1.1.1,8.8.4.4
     ```
   - After making changes, restart the service:
     ```bash
     sudo systemctl restart docker-dns
     ```
---

## Usage

### Resolving Docker Container Names

1. **Test the Main Scenario**:
   Assuming a container named `mycontainer` is running, and that `docker-dns` has been configured to listen on port `5335`, and the TLD is `.docker`, let's resolve the container's IP:
   ```bash
   dig mycontainer.docker @127.0.0.1 -p 5335 +short
   ```

2. **Test the Fallback DNS**:
   Check that `docker-dns` is also capable of resolving domains which are not "internal" docker container names
   Verify non-Docker queries are forwarded to the fallback DNS:
   ```bash
   dig google.com @127.0.0.1 -p 5335 +short
   ```

---

## Configuration

The application uses an INI-style configuration file located at `/etc/docker-dns/docker-dns.conf`. Below is an explanation of the configurable options:

- `PORT`: The port number the DNS server listens on (default: `5335`).
- `TTL`: The time-to-live (in seconds) for cached DNS records (default: `300`).
- `TLD`: The top-level domain for resolving container names (default: `docker`).
- `DEFAULT_RESOLVER`: A comma-separated list of fallback DNS servers (default: `8.8.8.8,1.1.1.1,8.8.4.4`).

### Restarting the Service After Changes

If you edit the configuration file, restart the service to apply changes:
```bash
sudo systemctl restart docker-dns
```

---

## Important Notes

- **Default Resolver Integration**: While Docker DNS runs on a custom port (not `53`), it is possible to use tools like `dig` to test queries. However, making Docker DNS the system-wide default resolver requires additional configuration or hacks.
- **Systemd-Resolved Compatibility**: Direct integration with `systemd-resolved` is non-trivial and not recommended without advanced setup.

---

## Build from Source

For developers, you can build the application from source:

1. Clone the repository:
   ```bash
   git clone https://github.com/medunes/docker-dns.git
   cd docker-dns
   ```

2. Build the binary:
   ```bash
   go build -o docker-dns main.go
   ```

3. Run the application:
   ```bash
   ./docker-dns
   ```

---

## Contributing

Contributions are welcome!

1. Fork the repository.
2. Create a feature branch:
   ```bash
   git checkout -b feature/YourFeature
   ```
3. Commit your changes:
   ```bash
   git commit -am 'Add YourFeature'
   ```
4. Push to the branch:
   ```bash
   git push origin feature/YourFeature
   ```
5. Open a Pull Request.

---

## License

This project is licensed under the AGPL License - see the [LICENSE](./LICENSE) file for details.

---

*Empower your Docker networking with easy-to-use DNS resolution!*
