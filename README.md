# Docker DNS Resolver

 [![Tests](https://github.com/MedUnes/docker-dns/actions/workflows/test.yml/badge.svg)](https://github.com/MedUnes/docker-dns/actions/workflows/test.yml) 
 
 [![Release](https://github.com/MedUnes/docker-dns/actions/workflows/release.yml/badge.svg)](https://github.com/MedUnes/docker-dns/actions/workflows/release.yml)
 
  [![Go Report Card](https://goreportcard.com/badge/github.com/medunes/docker-dns)](https://goreportcard.com/report/github.com/medunes/docker-dns) [![License](https://img.shields.io/github/license/medunes/docker-dns)](LICENSE) [![Go Reference](https://pkg.go.dev/badge/github.com/medunes/docker-dns.svg)](https://pkg.go.dev/github.com/medunes/docker-dns)
 
A lightweight DNS server that resolves Docker container names to their IP addresses, enabling seamless networking between containers and the host.

## Features

- **Automatic DNS Resolution**: Resolve Docker container names with a custom TLD to their respective IP addresses.
- **Caching Mechanism**: Configurable TTL for DNS cache entries to improve performance.
- **Simple Configuration**: Minimal setup with command-line options.
- **Lightweight and Fast**: Built with Go for high performance.

## Installation

### Prerequisites

- **Go** (version 1.16 or higher)
- **Docker** installed and running

### Build from Source

```bash
git clone https://github.com/medunes/docker-dns.git
cd docker-dns
go build -o dockerdns main.go
```

### Download Binary

Download the latest binary from the [Releases](https://github.com/medunes/docker-dns/releases) page and add it to your `$PATH`.

## Usage

Start the DNS server with default settings:

```bash
./dockerdns
```

By default, the server listens on port `5335` and uses the TLD `.docker`.

### Resolving Container Names

Assuming you have a running container named `mycontainer`, you can resolve its IP using:

```bash
dig @localhost -p 5335 mycontainer.docker
```

This will return the IP address assigned to `mycontainer`.

## Command-Line Options

```plaintext
Usage of DNS resolver:
  -help
        Display help and usage information
  -port string
        Port on which the DNS server will listen (default "5335")
  -tld string
        Top-level domain for container DNS resolution (default "docker")
  -ttl int
        Time-to-live for DNS cache entries in seconds (default 300)
```

### Examples

- **Custom Port and TLD**

  ```bash
  ./dockerdns -port 5353 -tld local
  ```

  Resolves containers like `mycontainer.local` on port `5353`.

- **Custom TTL**

  ```bash
  ./dockerdns -ttl 600
  ```

  Sets DNS cache entries to expire after 600 seconds.

## Configuration

To integrate the DNS resolver with your system:

1. **Update DNS Settings**

   Modify your system's DNS settings to include `localhost` on the port you're using (e.g., `127.0.0.1:5335`).

2. **Modify `/etc/resolv.conf`**

   Add the following line:

   ```plaintext
   nameserver 127.0.0.1
   ```

   *Note*: Be cautious when modifying system files.

## Contributing

Contributions are welcome!

1. Fork the repository.
2. Create a feature branch (`git checkout -b feature/YourFeature`).
3. Commit your changes (`git commit -am 'Add YourFeature'`).
4. Push to the branch (`git push origin feature/YourFeature`).
5. Open a Pull Request.

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

## Contact

- **Author**: medunes
- **Email**: [medunes@protonmail.com](mailto:medunes@protonmail.com)


---

*Empower your Docker networking with easy-to-use DNS resolution!*