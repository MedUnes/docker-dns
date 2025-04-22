# Docker DNS Resolver

[![Tests](https://github.com/MedUnes/docker-dns/actions/workflows/test.yml/badge.svg)](https://github.com/MedUnes/docker-dns/actions/workflows/test.yml) [![Release](https://github.com/MedUnes/docker-dns/actions/workflows/release.yml/badge.svg)](https://github.com/MedUnes/docker-dns/actions/workflows/release.yml) [![Go Report Card](https://goreportcard.com/badge/github.com/medunes/docker-dns)](https://goreportcard.com/report/github.com/medunes/docker-dns) [![License](https://img.shields.io/github/license/medunes/docker-dns)](LICENSE) [![Go Reference](https://pkg.go.dev/badge/github.com/medunes/docker-dns.svg)](https://pkg.go.dev/github.com/medunes/docker-dns)

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

### Download and Install the Debian Package

1. **Download the latest `.deb` Package**:
     ```bash
     wget https://raw.githubusercontent.com/MedUnes/docker-dns/master/install_debian_latest.sh
     chmod +x install_debian_latest.sh
     ./install_debian_latest.sh
     ```

2. **Install the Package**:
   ```bash
   sudo dpkg -i docker-dns-1.1.1_amd64.deb
   ```

### Setup & Integration Details (Debian/Ubuntu)

After installation, the package will:

- Detect your system resolver stack (e.g., systemd-resolved, dnsmasq, unbound, or plain `/etc/resolv.conf`).
- Ask you if you want to integrate `docker-dns` with the detected resolver.
- Integrate via safe drop-in config files (never replaces core system files directly).
- Restart the DNS subsystem only when needed.
- Make backups of any config files it adjusts (under `/var/lib/docker-dns/backup`).

#### Integration modes:
- **systemd-resolved**: Adds a drop-in under `/etc/systemd/resolved.conf.d/`, and optionally disables DNS injection from DHCP.
- **dnsmasq**: Adds a config line `server=/.docker/127.0.0.153` under `/etc/dnsmasq.d/`.
- **unbound**: Adds a stub-zone block to `/etc/unbound/unbound.d/`.
- **plain resolv.conf**: Adds a `nameserver 127.0.0.153` line to `/etc/resolv.conf`.

> During installation, if you're running interactively (not with `DEBIAN_FRONTEND=noninteractive`), the installer will ask you to confirm the integration step.

#### Rollback:
- To **remove** the service but keep its config:
   ```bash
   sudo dpkg -r docker-dns
   ```
- To **purge** the service and restore DNS settings:
   ```bash
   sudo dpkg -P docker-dns
   ```
  This will:
    - Stop the service.
    - Remove any configuration drop-ins.
    - Restore previous `resolv.conf` or DNS drop-ins from backup.

#### Notes on `systemd-resolved` integration:
- The package creates a symlink from `/etc/resolv.conf` to the systemd stub: `/run/systemd/resolve/stub-resolv.conf`.
- If you're unfamiliar with systemd-resolved, it's the default name resolution manager in Ubuntu and other modern distros.
- For advanced topics:
    - [systemd-resolved documentation](https://www.freedesktop.org/software/systemd/man/systemd-resolved.service.html)
    - [ArchWiki on systemd-resolved](https://wiki.archlinux.org/title/Systemd-resolved)

---

## Configuration

1. **Check the Service Status**:
   ```bash
   systemctl status docker-dns
   ```

2. **Edit the Configuration (Optional)**:
    - File: `/etc/docker-dns/docker-dns.conf`
   ```ini
   IP=127.0.0.153
   TTL=300
   TLD=docker
   DEFAULT_RESOLVER=8.8.8.8,1.1.1.1,8.8.4.4
   ```

3. **Restart the Service After Changes**:
    ```bash
    sudo systemctl restart docker-dns
    ```

---

## Usage

1. **Web Browser Access**
    - Example: http://my_dashboard.docker:3000

2. **Resolve container by name**
   ```bash
   dig mycontainer.docker @127.0.0.153 +short
   ```

3. **Resolve external domains**
   ```bash
   dig google.com @127.0.0.153 +short
   ```

---

## Full Configuration Details
* It is possible to configure the `docker-dns` server at startup time through a couple of CLI arguments
* The `systemd` setup of the application uses an INI-style configuration file located at `/etc/docker-dns/docker-dns.conf`.
* Below is an explanation of the configurable options:

### `--ip` (INI file variable name: `IP`)
- The ip address the DNS server listens on (default: `127.0.0.153`).
- Typically, it would be within the loop-back range (```127.0.0.0/8```), but could also be an IP for a customer interface.
- Default: `127.0.0.153`
- Change this value if another service (e.g., a DNS server) is already using this IP, or if you target another interface.
- Example: if you don't have any DNS server already listening on the localhost IP (`127.0.0.1`), you can start `docker-dns` as such:
  ```bash
    ./sudo docker-dns --ip=127.0.0.1
    ```

### `--ttl` (INI file variable name:  `TTL` )
- The time-to-live (in seconds) for cached DNS records (default: `300`).
- Use a higher value for less frequent updates (better performance)
- Use a lower value for more real-time accuracy.

### `--tld` (INI file variable name:  `TLD`)
- The top-level domain for resolving container names (default: `docker`).
- Example: if ```TLD=local``` and a container's name is ```mycontainer```, queries to ```mycontainer.local``` will resolve to the container's IP.

### `--default-resolver` (INI file variable name:  `DEFAULT_RESOLVER`)

- A comma-separated list of fallback DNS servers (default: `8.8.8.8,1.1.1.1,8.8.4.4`).
- These are used when a query cannot be resolved via Docker-based DNS.
- Examples:
    - ```8.8.8.8``` (Google DNS)
    - ```1.1.1.1``` (Cloudflare DNS)
    - ```127.0.0.1``` (Some Local DNS server)
- Notes:
    - Specify multiple servers to provide redundancy (e.g., ```DEFAULT_RESOLVER=8.8.8.8,1.1.1.1```).
    - To use a local DNS server for non-Docker queries, add its IP address here (e.g., ```127.0.0.1```).


## Build/Run from Source

### Download built/compiled binaries:
- Visit the [Releases](https://github.com/MedUnes/docker-dns/releases) page.

### Build locally:
```bash
git clone https://github.com/medunes/docker-dns.git
cd docker-dns
make build
```

### Run the binary:
```bash
sudo ./docker-dns -h
```

---

## Contributing

Contributions are welcome!

---

## License

This project is licensed under the AGPL License - see the [LICENSE](./LICENSE) file for details.

---

*Empower your Docker networking with easy-to-use DNS resolution!*


