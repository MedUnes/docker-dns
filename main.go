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

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/client"
	"github.com/miekg/dns"
)

// DockerClient interface for dependency injection
type DockerClient interface {
	ContainerInspect(ctx context.Context, containerID string) (types.ContainerJSON, error)
	Close() error
}

// RealDockerClient implements DockerClient with actual Docker client
type RealDockerClient struct {
	*client.Client
}

// Ensure RealDockerClient implements DockerClient
var _ DockerClient = (*RealDockerClient)(nil)

var (
	ip        = flag.String("ip", "127.0.0.153", "IP address on which the DNS server will listen")
	tld       = flag.String("tld", "docker", "Top-level domain for container DNS resolution")
	ttl       = flag.Int("ttl", 300, "Time-to-live for DNS cache entries in seconds")
	help      = flag.Bool("help", false, "Display help and usage information")
	resolvers = flag.String("default-resolver", "8.8.8.8,1.1.1.1,8.8.4.4", "Comma-separated list of fallback DNS resolvers")

	// Initialize dockerClient to nil initially. It will be set in main or replaced by mock in tests.
	dockerClient DockerClient
	dnsCache     = struct {
		sync.RWMutex
		m map[string][]string
		t map[string]time.Time
	}{m: make(map[string][]string), t: make(map[string]time.Time)}
	fallbackDNS []string
)

func handleDNSQuery(w dns.ResponseWriter, r *dns.Msg) {
	msg := dns.Msg{}
	msg.SetReply(r)
	msg.Authoritative = true // We are authoritative for our TLD

	// Handle EDNS0 options if present
	if opt := r.IsEdns0(); opt != nil {
		msg.SetEdns0(opt.UDPSize(), opt.Do())
	}

	// Ensure there's a question
	if len(r.Question) == 0 {
		log.Println("Received query with no questions")
		// Send SERVFAIL or similar? For now, just return.
		// msg.SetRcode(r, dns.RcodeServerFailure)
		// w.WriteMsg(&msg)
		return
	}

	question := r.Question[0] // Use the first question
	domain := question.Name

	// Only handle A and AAAA queries for now
	if question.Qtype != dns.TypeA && question.Qtype != dns.TypeAAAA {
		log.Printf("Received unsupported query type %s for %s", dns.TypeToString[question.Qtype], domain)
		// Optionally forward or return NXDOMAIN/NotImplemented
		msg.SetRcode(r, dns.RcodeNotImplemented)
		if err := w.WriteMsg(&msg); err != nil {
			log.Printf("Failed to write NotImplement DNS response: %v\n", err)
		}
		return
	}

	// Check if it's a query for our managed TLD
	localDomainSuffix := fmt.Sprintf(".%s.", *tld)
	if strings.HasSuffix(domain, localDomainSuffix) {
		log.Printf("Handling local query for: %s", domain)
		ips, found := getCachedIPs(domain)
		if !found {
			log.Printf("Cache miss for: %s. Fetching from Docker.", domain)
			var err error
			// Ensure dockerClient is initialized before using it
			if dockerClient == nil {
				log.Println("Error: Docker client is not initialized in handleDNSQuery")
				msg.SetRcode(r, dns.RcodeServerFailure) // Indicate internal server error
				// No IPs found, don't add answer records
			} else {
				ips, err = fetchIPsFromDocker(domain, dockerClient)
				if err != nil {
					log.Printf("Error fetching Docker DNS records for %s: %v\n", domain, err)
					// Decide how to respond on error, e.g., NXDOMAIN or SERVFAIL
					// For now, we just won't add any answer records.
					// msg.SetRcode(r, dns.RcodeServerFailure) // Or NXDomain if appropriate
				} else if len(ips) > 0 {
					log.Printf("Successfully fetched IPs for %s: %v. Caching.", domain, ips)
					cacheIPs(domain, ips)
				} else {
					log.Printf("No IPs found in Docker for %s.", domain)
					// Respond with NXDOMAIN or empty answer? Empty answer seems more appropriate.
					// msg.SetRcode(r, dns.RcodeNameError) // NXDOMAIN
				}
			}
		} else {
			log.Printf("Cache hit for: %s. Using cached IPs: %v", domain, ips)
		}

		// Add A records if IPs were found (either from cache or fetch)
		// Only add A records if the query was for A records
		if question.Qtype == dns.TypeA {
			for _, ipStr := range ips {
				ip := net.ParseIP(ipStr)
				if ip != nil && ip.To4() != nil { // Ensure it's a valid IPv4 address
					msg.Answer = append(msg.Answer, &dns.A{
						Hdr: dns.RR_Header{Name: domain, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: uint32(*ttl)},
						A:   ip,
					})
				} else {
					log.Printf("Skipping invalid or non-IPv4 address '%s' for domain %s", ipStr, domain)
				}
			}
		}
		// Handle AAAA records similarly if needed (currently fetchIPsFromDocker only gets IPv4)

		// If no IPs were found/added, the answer section remains empty.
		// The default Rcode is NOERROR. If fetch failed or no IPs, maybe set NXDOMAIN?
		if len(msg.Answer) == 0 {
			// Set NXDOMAIN if we are authoritative and the name truly doesn't exist
			// Or just return NOERROR with empty answer if that's preferred.
			// Let's try returning NOERROR with empty answer first.
			log.Printf("No answer records generated for local query: %s", domain)
		}

	} else {
		// Not a local domain, forward it
		log.Printf("Forwarding non-local query for: %s", domain)
		forwardQueryToExternalDNS(&msg, r) // Pass original request for question details
	}

	// Write the final response
	if err := w.WriteMsg(&msg); err != nil {
		log.Printf("Failed to write DNS response: %v\n", err)
	} else {
		log.Printf("Successfully sent DNS response for %s with RCODE %s and %d answers.", domain, dns.RcodeToString[msg.Rcode], len(msg.Answer))
	}
}

// forwardQueryToExternalDNS forwards the query in 'req' to fallback resolvers
// and adds answers to 'msg'.
func forwardQueryToExternalDNS(msg *dns.Msg, req *dns.Msg) {
	if len(req.Question) == 0 {
		return // Should not happen based on caller checks
	}
	question := req.Question[0]
	domain := question.Name

	c := new(dns.Client)
	// Use the original request message to forward, preserving ID etc.
	// But only set the question we care about (miekg/dns client Exchange does this)
	m := new(dns.Msg)
	m.SetQuestion(question.Name, question.Qtype)
	m.RecursionDesired = true // Ask upstream resolver to recurse

	for _, server := range fallbackDNS {
		serverAddr := net.JoinHostPort(server, "53")
		log.Printf("Forwarding query for %s (%s) to %s", domain, dns.TypeToString[question.Qtype], serverAddr)
		r, _, err := c.Exchange(m, serverAddr)
		if err != nil {
			log.Printf("Error forwarding query to %s: %v", serverAddr, err)
			continue // Try next resolver
		}

		// Check Rcode and if we got any answers
		if r.Rcode == dns.RcodeSuccess && len(r.Answer) > 0 {
			log.Printf("Received successful response with %d answers from %s for %s", len(r.Answer), serverAddr, domain)
			msg.Answer = append(msg.Answer, r.Answer...)
			// Copy other relevant sections if needed (e.g., Authority, Additional)
			msg.Rcode = r.Rcode // Use Rcode from successful upstream response
			return             // Stop after first successful response
		} else {
			log.Printf("Received response from %s for %s with RCODE %s and %d answers.", serverAddr, domain, dns.RcodeToString[r.Rcode], len(r.Answer))
			// Store the Rcode in case all forwards fail, maybe return the last non-successful Rcode?
			msg.Rcode = r.Rcode
		}
	}

	// If loop finishes without success
	log.Printf("No valid response received from any fallback DNS servers for %s", domain)
	// msg.Rcode might already be set to the last error Rcode (e.g., NXDOMAIN, SERVFAIL)
	// If no server was reachable, Rcode might still be NOERROR, maybe set SERVFAIL?
	if len(msg.Answer) == 0 && msg.Rcode == dns.RcodeSuccess {
		msg.Rcode = dns.RcodeServerFailure // Indicate failure to resolve externally
	}
}

func main() {
	flag.Parse()

	if *help {
		fmt.Println("Usage of DNS resolver:")
		flag.PrintDefaults()
		return
	}

	if err := validateConfig(); err != nil {
		log.Fatalf("Invalid configuration: %v", err)
	}

	// Initialize the real Docker client for the main application
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		log.Fatalf("Failed to create Docker client: %v", err)
	}
	// Assign the real client to the global interface variable
	dockerClient = &RealDockerClient{Client: cli}
	// Defer closing the client connection
	defer func() {
		if dockerClient != nil {
			if err := dockerClient.Close(); err != nil {
				log.Printf("Error closing docker client: %v", err)
			}
		}
	}()

	fallbackDNS = strings.Split(*resolvers, ",")
	// Trim whitespace from resolver IPs
	for i := range fallbackDNS {
		fallbackDNS[i] = strings.TrimSpace(fallbackDNS[i])
	}
	log.Printf("Using fallback DNS resolvers: %v", fallbackDNS)

	dns.HandleFunc(".", handleDNSQuery) // Handle all queries
	serverAddr := fmt.Sprintf("%s:53", *ip)
	server := &dns.Server{Addr: serverAddr, Net: "udp"}

	log.Printf("Starting DNS server on %s (UDP) with TLD '%s' and TTL %d seconds", serverAddr, *tld, *ttl)
	if err := server.ListenAndServe(); err != nil {
		log.Fatalf("Failed to start server: %s\n", err)
	}
	// Shutdown is usually handled by OS signals, but a defer is good practice if main returns
	defer server.Shutdown()
}

func validateConfig() error {
	if net.ParseIP(*ip) == nil {
		return fmt.Errorf("invalid IP address: %s", *ip)
	}
	if *tld == "" {
		return fmt.Errorf("TLD cannot be empty")
	}
	// Optional: Validate TLD format (e.g., no dots)
	if strings.Contains(*tld, ".") {
		log.Printf("Warning: TLD '%s' contains dots. Ensure this is intended.", *tld)
	}
	// Validate resolvers
	resolverList := strings.Split(*resolvers, ",")
	if len(resolverList) == 0 || (len(resolverList) == 1 && resolverList[0] == "") {
		return fmt.Errorf("at least one fallback resolver must be specified")
	}
	for _, r := range resolverList {
		trimmed := strings.TrimSpace(r)
		if net.ParseIP(trimmed) == nil {
			return fmt.Errorf("invalid fallback resolver IP address: %s", trimmed)
		}
	}
	return nil
}

// --- Cache Functions ---

func getCachedIPs(fqdn string) ([]string, bool) {
	dnsCache.RLock()
	defer dnsCache.RUnlock()
	ips, found := dnsCache.m[fqdn]
	if !found {
		return nil, false // Not in cache map
	}
	// Check expiration
	expiryTime := dnsCache.t[fqdn]
	if time.Since(expiryTime) >= time.Duration(*ttl)*time.Second {
		log.Printf("Cache expired for %s (cached at %s, TTL %ds)", fqdn, expiryTime, *ttl)
		// Don't delete here, let the caller overwrite if fetch is successful
		return nil, false // Expired
	}
	// Found and not expired
	return ips, true
}

func cacheIPs(fqdn string, ips []string) {
	// Don't cache empty results? Or cache them with a short TTL?
	// For now, only cache non-empty results.
	if len(ips) == 0 {
		log.Printf("Not caching empty result for %s", fqdn)
		return
	}
	dnsCache.Lock()
	defer dnsCache.Unlock()
	dnsCache.m[fqdn] = ips
	dnsCache.t[fqdn] = time.Now()
	log.Printf("Cached %d IPs for %s", len(ips), fqdn)
}

// --- Docker Interaction ---

func fetchIPsFromDocker(fqdn string, cli DockerClient) ([]string, error) {
	// Extract container name: remove ".<tld>." suffix, then remove trailing "."
	localDomainSuffix := fmt.Sprintf(".%s.", *tld)
	if !strings.HasSuffix(fqdn, localDomainSuffix) {
		return nil, fmt.Errorf("FQDN '%s' does not end with expected suffix '%s'", fqdn, localDomainSuffix)
	}
	// Trim suffix ".tld." -> leaves "containername."
	containerNameWithDot := strings.TrimSuffix(fqdn, localDomainSuffix)
	// Trim trailing "." -> leaves "containername"
	containerName := strings.TrimSuffix(containerNameWithDot, ".")

	// Log the exact name being used for inspection
	log.Printf("Attempting to inspect container: '%s' (derived from FQDN: '%s')", containerName, fqdn)

	// Ensure context is passed correctly
	ctx := context.Background()
	containerJSON, err := cli.ContainerInspect(ctx, containerName)
	if err != nil {
		// Log the specific error from Docker client
		log.Printf("Docker client error inspecting container '%s': %v", containerName, err)
		// Check if it's a "not found" error specifically
		if client.IsErrNotFound(err) {
			log.Printf("Container '%s' not found via Docker API.", containerName)
			// Return empty list and no error, or a specific error?
			// For DNS, returning empty list (leading to NXDOMAIN or empty NOERROR) seems appropriate.
			return []string{}, nil // Indicate not found, but not an internal error
		}
		// For other errors, return them
		return nil, fmt.Errorf("failed to inspect container '%s': %w", containerName, err)
	}

	log.Printf("Successfully inspected container: '%s'. Extracting IPs.", containerName)
	var ipList []string
	// Perform nil checks for safety
	if containerJSON.NetworkSettings != nil && containerJSON.NetworkSettings.Networks != nil {
		for networkName, network := range containerJSON.NetworkSettings.Networks {
			if network != nil && network.IPAddress != "" {
				// Log found IP and network name
				log.Printf("Found IP '%s' for network '%s' in container '%s'", network.IPAddress, networkName, containerName)
				ipList = append(ipList, network.IPAddress)
			}
		}
	} else {
		log.Printf("NetworkSettings or Networks map was nil/empty for container '%s'", containerName)
	}

	if len(ipList) == 0 {
		log.Printf("No IPs found in NetworkSettings for container '%s'", containerName)
	}

	return ipList, nil
}

