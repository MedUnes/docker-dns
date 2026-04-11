// Package config handles CLI flag parsing, env-based config, and validation.
package config

import (
	"flag"
	"fmt"
	"net"
	"strings"
	"time"
)

// Config holds the fully-validated runtime configuration.
type Config struct {
	// ListenIP is the IP address the DNS server binds to.
	ListenIP string
	// TLDs is the list of managed top-level domains (e.g. ["docker", "local"]).
	TLDs []string
	// TTL is the cache and DNS response time-to-live.
	TTL time.Duration
	// Resolvers is the ordered list of fallback DNS resolver IPs.
	Resolvers []string
	// DockerHost overrides DOCKER_HOST when non-empty.
	DockerHost string
	// LogLevel controls verbosity: debug, info, warn, error.
	LogLevel string
	// RateLimit is the max queries per second per client IP (0 = disabled).
	RateLimit float64
	// RateBurst is the burst size for per-IP rate limiting.
	RateBurst int
	// MaxCacheSize caps the number of cache entries (0 = unlimited).
	MaxCacheSize int
	// HTTPAddr is the address of the health/metrics HTTP server ("" = disabled).
	HTTPAddr string
	// DockerTimeout is the timeout for Docker API calls.
	DockerTimeout time.Duration
	// ForwardTimeout is the per-resolver timeout for forwarded DNS queries.
	ForwardTimeout time.Duration
}

// Load parses flags and returns a validated Config.
func Load() (*Config, error) {
	var (
		listenIP       = flag.String("ip", "127.0.0.153", "IP address the DNS server listens on")
		tld            = flag.String("tld", "docker", "Comma-separated managed top-level domains for container resolution (e.g. docker,local)")
		ttl            = flag.Int("ttl", 300, "TTL in seconds for cache entries and DNS responses")
		resolvers      = flag.String("resolvers", "8.8.8.8,1.1.1.1,8.8.4.4", "Comma-separated fallback DNS resolver IPs")
		dockerHost     = flag.String("docker-host", "", "Docker host override (empty = use DOCKER_HOST env / socket default)")
		logLevel       = flag.String("log-level", "info", "Log level: debug | info | warn | error")
		rateLimit      = flag.Float64("rate-limit", 100, "Max queries/sec per client IP; 0 disables rate limiting")
		rateBurst      = flag.Int("rate-burst", 50, "Burst allowance for per-IP rate limiting")
		maxCache       = flag.Int("max-cache-size", 10_000, "Max DNS cache entries; 0 = unlimited")
		httpAddr       = flag.String("http-addr", ":8080", "Address for the health/metrics HTTP server; empty to disable")
		dockerTimeout  = flag.Duration("docker-timeout", 5*time.Second, "Timeout for Docker API calls")
		forwardTimeout = flag.Duration("forward-timeout", 2*time.Second, "Per-resolver timeout for forwarded DNS queries")
	)
	flag.Parse()

	cfg := &Config{
		ListenIP:       *listenIP,
		TTL:            time.Duration(*ttl) * time.Second,
		DockerHost:     *dockerHost,
		LogLevel:       *logLevel,
		RateLimit:      *rateLimit,
		RateBurst:      *rateBurst,
		MaxCacheSize:   *maxCache,
		HTTPAddr:       *httpAddr,
		DockerTimeout:  *dockerTimeout,
		ForwardTimeout: *forwardTimeout,
	}

	for _, t := range strings.Split(*tld, ",") {
		if t = strings.ToLower(strings.Trim(strings.TrimSpace(t), ".")); t != "" {
			cfg.TLDs = append(cfg.TLDs, t)
		}
	}

	for _, r := range strings.Split(*resolvers, ",") {
		if r = strings.TrimSpace(r); r != "" {
			cfg.Resolvers = append(cfg.Resolvers, r)
		}
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// Validate checks all fields for correctness.
func (c *Config) Validate() error {
	if net.ParseIP(c.ListenIP) == nil {
		return fmt.Errorf("invalid listen IP %q", c.ListenIP)
	}
	if len(c.TLDs) == 0 {
		return fmt.Errorf("at least one TLD must be specified")
	}
	for _, tld := range c.TLDs {
		if tld == "" {
			return fmt.Errorf("TLD cannot be empty")
		}
		if strings.Contains(tld, ".") {
			return fmt.Errorf("TLD %q must not contain dots; use a single label like \"docker\"", tld)
		}
	}
	if c.TTL <= 0 {
		return fmt.Errorf("TTL must be a positive duration")
	}
	if len(c.Resolvers) == 0 {
		return fmt.Errorf("at least one fallback resolver must be specified")
	}
	for _, r := range c.Resolvers {
		if net.ParseIP(r) == nil {
			return fmt.Errorf("invalid resolver IP %q", r)
		}
	}
	if c.RateLimit < 0 {
		return fmt.Errorf("rate-limit must be >= 0")
	}
	if c.RateBurst < 1 {
		return fmt.Errorf("rate-burst must be >= 1")
	}
	validLevels := map[string]bool{"debug": true, "info": true, "warn": true, "error": true}
	if !validLevels[c.LogLevel] {
		return fmt.Errorf("invalid log-level %q; must be one of: debug, info, warn, error", c.LogLevel)
	}
	return nil
}

// LocalDomainSuffixes returns the FQDN suffixes for all managed TLDs (e.g. [".docker.", ".local."]).
func (c *Config) LocalDomainSuffixes() []string {
	suffixes := make([]string, len(c.TLDs))
	for i, tld := range c.TLDs {
		suffixes[i] = "." + tld + "."
	}
	return suffixes
}

// MatchLocalSuffix returns the matching TLD suffix for the given domain, or ""
// if the domain does not belong to any managed TLD.
func (c *Config) MatchLocalSuffix(domain string) string {
	for _, s := range c.LocalDomainSuffixes() {
		if strings.HasSuffix(domain, s) {
			return s
		}
	}
	return ""
}
