# Docker DNS Resolver

[![Tests](https://github.com/MedUnes/docker-dns/actions/workflows/test.yml/badge.svg)](https://github.com/MedUnes/docker-dns/actions/workflows/test.yml) [![Release](https://github.com/MedUnes/docker-dns/actions/workflows/release.yml/badge.svg)](https://github.com/MedUnes/docker-dns/actions/workflows/release.yml) [![Go Report Card](https://goreportcard.com/badge/github.com/medunes/docker-dns)](https://goreportcard.com/report/github.com/medunes/docker-dns) [![License](https://img.shields.io/github/license/medunes/docker-dns)](LICENSE) [![Go Reference](https://pkg.go.dev/badge/github.com/medunes/docker-dns.svg)](https://pkg.go.dev/github.com/medunes/docker-dns)

<img src="./logo.png" height="512" width="1024" />

A lightweight DNS server that resolves Docker container names to their IP addresses, enabling seamless networking between containers and the host.

## Features

- **Automatic DNS Resolution**: Resolve Docker container names with a custom TLD to their respective IP addresses.
- **Systemd-resolved Integration**: Native integration with modern Linux systems using systemd-resolved.
- **Caching Mechanism**: Configurable TTL for DNS cache entries to improve performance.
- **Simple Configuration**: Minimal setup with command-line options or a configuration file.
- **Debian Package**: Easy-to-install `.deb` package with automatic systemd-resolved configuration.
- **Fallback DNS**: Forwards non-Docker queries to configurable DNS servers.
- **Lightweight and Fast**: Built with Go for high performance.
## System Requirements

**Important Note on DNS Integration**  
Seamless host-wide DNS integration currently requires:  
✓ **Debian/Ubuntu-based systems**  
✓ **systemd-resolved** as the active DNS resolver

If your system doesn't meet these requirements, you can still use docker-dns by:
1. Adding its resolver IP (`127.0.0.153` by default) to your DNS configuration
2. Manually configuring your DNS stack to forward queries for the `.docker` domain

**Manual Integration Example** (for non-systemd-resolved systems):
```bash
# Add to /etc/resolv.conf (temporary)
sudo sed -i '1s/^/nameserver 127.0.0.153\n/' /etc/resolv.conf

# Or configure your DNS software (dnsmasq/unbound/etc) to forward:
# All *.docker queries to 127.0.0.153
```

> 💡 The resolver IP can be customized—see [configuration details](#configuration) below.  
> 🔍 Check current resolver IP in `/etc/docker-dns/docker-dns.conf`

---

## Installation for Linux/Debian:

### Download and Install the Debian Package

1. **Run the following command to get the latest Debian package downloaded and installed**:
     ```bash
     wget https://raw.githubusercontent.com/MedUnes/docker-dns/master/install_debian_latest.sh && \
     chmod +x install_debian_latest.sh && \
     ./install_debian_latest.sh && \
      rm ./install_debian_latest.sh
     ```
### Setup & Integration Details (Debian/Ubuntu)

After installation, the package will:

- Check if systemd-resolved is active
- Automatically configure systemd-resolved integration if detected
- Create safe drop-in config files (never replaces core system files directly)
- Restart the DNS subsystem only when needed
- Make backups of any config files it adjusts (under `/var/lib/docker-dns/backup`)

#### Systemd-resolved Integration:

- Creates a drop-in configuration under `/etc/systemd/resolved.conf.d/`
- Optionally disables DNS injection from DHCP via NetworkManager
- Creates a symlink from `/etc/resolv.conf` to the systemd stub resolver

If systemd-resolved is not active, the installer will:
- Leave your DNS configuration unchanged
- Provide instructions for manual integration
- Run the service on 127.0.0.153 for optional manual configuration

#### Rollback:
- To **remove** the service but keep its config:
   ```bash
   sudo dpkg -r docker-dns
   ```
- To **purge** the service and restore DNS settings:
   ```bash
   sudo dpkg -p docker-dns
   ```
  This will:
    - Stop the service
    - Remove systemd-resolved configuration drop-ins
    - Restore the previous resolver configuration from the backup
    - Remove all package files

#### Notes on `systemd-resolved` integration:
- The package creates a symlink from `/etc/resolv.conf` to the systemd stub: `/run/systemd/resolve/stub-resolv.conf`
- This is the default name resolution manager in Ubuntu and other modern systemd-based distributions
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
    - Example: http://hellow-world.dev.docker
2. **Access containers by names**:
    - Example: If your **container name** is mysql-test, you can access it as follows:
   ```bash
   mysql -h mysql-test.docker -P 3306-u admin -p
   ```
## Resolving containers

* Note: the ```@127.0.0.153``` is not required if `docker-dns` was host-wide integrated.

1. **Resolve container by name**
   ```bash
   dig mycontainer.docker @127.0.0.153 +short
   ```

2. **Resolve external domains**
   ```bash
   dig google.com @127.0.0.153 +short
   ```
---

## Full Configuration Details
* Configure `docker-dns` through CLI arguments or configuration file
* The `systemd` setup uses an INI-style configuration file at `/etc/docker-dns/docker-dns.conf`

### `--ip` (INI file variable: `IP`)
- IP address the DNS server listens on (default: `127.0.0.153`)
- Must be a valid local IP address
- Change if conflicting with existing services

### `--ttl` (INI file variable: `TTL`)
- Time-to-live (seconds) for cached DNS records (default: `300`)
- Higher values improve performance, lower values increase freshness

### `--tld` (INI file variable: `TLD`)
- Top-level domain for container resolution (default: `docker`)
- Example: `mycontainer.docker` resolves to container IP

### `--default-resolver` (INI file variable: `DEFAULT_RESOLVER`)
- Comma-separated fallback DNS servers (default: `8.8.8.8,1.1.1.1,8.8.4.4`)
- Used for non-Docker queries
- Supports multiple servers for redundancy

---

## Build/Run from Source

### Download pre-built binaries:
- Visit [Releases](https://github.com/MedUnes/docker-dns/releases)

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

AGPL License - see [LICENSE](./LICENSE)

---

*Simplify Docker networking with automatic DNS resolution for modern Linux systems!*

