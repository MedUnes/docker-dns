package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/docker/docker/client"
	"github.com/miekg/dns"
)

var (
	ip        = flag.String("ip", "127.0.0.153", "IP address on which the DNS server will listen")
	tld       = flag.String("tld", "docker", "Top-level domain for container DNS resolution")
	ttl       = flag.Int("ttl", 300, "Time-to-live for DNS cache entries in seconds")
	help      = flag.Bool("help", false, "Display help and usage information")
	resolvers = flag.String("default-resolver", "8.8.8.8,1.1.1.1,8.8.4.4", "Comma-separated list of fallback DNS resolvers")
)

var (
	fallbackDNS []string
	dnsCache    = struct {
		sync.RWMutex
		m map[string][]string
		t map[string]time.Time
	}{m: make(map[string][]string), t: make(map[string]time.Time)}
)

func handleDNSQuery(w dns.ResponseWriter, r *dns.Msg) {
	msg := dns.Msg{}
	msg.SetReply(r)
	msg.Authoritative = true

	if opt := r.IsEdns0(); opt != nil {
		msg.SetEdns0(opt.UDPSize(), opt.Do())
	}

	domain := msg.Question[0].Name

	if strings.HasSuffix(domain, fmt.Sprintf(".%s.", *tld)) {
		ips, found := getCachedIPs(domain)
		if !found {
			var err error
			ips, err = fetchIPsFromDocker(domain)
			if err != nil {
				log.Printf("Error fetching Docker DNS records: %s\n", err)
				return
			}
			cacheIPs(domain, ips)
		}

		for _, ip := range ips {
			msg.Answer = append(msg.Answer, &dns.A{
				Hdr: dns.RR_Header{Name: domain, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: uint32(*ttl)},
				A:   net.ParseIP(ip),
			})
		}
	} else {
		forwardQueryToExternalDNS(&msg, domain)
	}

	err := w.WriteMsg(&msg)
	if err != nil {
		return
	}
}

func forwardQueryToExternalDNS(msg *dns.Msg, domain string) {
	c := new(dns.Client)
	for _, server := range fallbackDNS {
		m := new(dns.Msg)
		m.SetQuestion(domain, dns.TypeA)
		r, _, err := c.Exchange(m, server+":53")
		if err == nil && len(r.Answer) > 0 {
			msg.Answer = append(msg.Answer, r.Answer...)
			return
		}
	}
	log.Printf("No valid response from any external DNS servers for %s\n", domain)
}

func main() {
	flag.Parse()

	if *help || len(flag.Args()) > 0 {
		fmt.Println("Usage of DNS resolver:")
		flag.PrintDefaults()
		return
	}

	fallbackDNS = strings.Split(*resolvers, ",")
	log.Printf("Using fallback DNS resolvers: %v", fallbackDNS)

	dns.HandleFunc(".", handleDNSQuery)
	server := &dns.Server{Addr: fmt.Sprintf("%s:53", *ip), Net: "udp"}

	log.Printf("Starting DNS server on ip %s with TLD %s and TTL %d seconds", *ip, *tld, *ttl)
	err := server.ListenAndServe()
	if err != nil {
		log.Fatalf("Failed to start server: %s\n", err)
	}
	defer func(server *dns.Server) {
		err := server.Shutdown()
		if err != nil {

		}
	}(server)
}

func getCachedIPs(fqdn string) ([]string, bool) {
	dnsCache.RLock()
	defer dnsCache.RUnlock()
	ips, found := dnsCache.m[fqdn]
	if found && time.Since(dnsCache.t[fqdn]) < time.Duration(*ttl)*time.Second {
		return ips, true
	}
	return nil, false
}

func cacheIPs(fqdn string, ips []string) {
	dnsCache.Lock()
	dnsCache.m[fqdn] = ips
	dnsCache.t[fqdn] = time.Now()
	dnsCache.Unlock()
}

func fetchIPsFromDocker(fqdn string) ([]string, error) {
	containerName := strings.TrimSuffix(strings.TrimSuffix(fqdn, fmt.Sprintf(".%s.", *tld)), ".")
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("failed to create Docker client: %v", err)
	}
	defer func(cli *client.Client) {
		err := cli.Close()
		if err != nil {

		}
	}(cli)

	containerJSON, err := cli.ContainerInspect(context.Background(), containerName)
	if err != nil {
		return nil, fmt.Errorf("failed to inspect container: %v", err)
	}

	var ipList []string
	for _, network := range containerJSON.NetworkSettings.Networks {
		ipList = append(ipList, network.IPAddress)
	}
	return ipList, nil
}
