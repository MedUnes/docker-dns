package server

import (
	"context"
	"log/slog"
	"net"
	"os"
	"testing"
	"time"

	"github.com/medunes/docker-dns/internal/cache"
	"github.com/medunes/docker-dns/internal/config"
	"github.com/miekg/dns"
)

// ----------------------------------------------------------------------------
// mockDockerClient
// ----------------------------------------------------------------------------

// mockDockerClient implements docker.Client for unit tests without a real daemon.
type mockDockerClient struct {
	ipsFunc func(ctx context.Context, name string) ([]string, error)
}

func (m *mockDockerClient) ContainerIPs(ctx context.Context, name string) ([]string, error) {
	return m.ipsFunc(ctx, name)
}
func (m *mockDockerClient) Close() error { return nil }

// Compile-time assertion (requires the docker package's Client interface).
// We keep this assertion in client_test.go to avoid an import cycle; the
// check here is implicit through the usage in server.New.

// ----------------------------------------------------------------------------
// testServer helpers
// ----------------------------------------------------------------------------

// defaultTestConfig returns a minimal valid config for tests.
func defaultTestConfig() *config.Config {
	return &config.Config{
		ListenIP:       "127.0.0.1",
		TLDs:           []string{"docker"},
		TTL:            10 * time.Second,
		Resolvers:      []string{"8.8.8.8"},
		LogLevel:       "debug",
		RateLimit:      0,
		RateBurst:      10,
		MaxCacheSize:   100,
		DockerTimeout:  2 * time.Second,
		ForwardTimeout: 2 * time.Second,
	}
}

// startTestDNSServer spins up a UDP-only DNS server on a random OS-assigned
// port (no TOCTOU: the PacketConn is kept open until the server shuts down).
// It returns the server address and registers a t.Cleanup shutdown.
func startTestDNSServer(t *testing.T, dc *mockDockerClient, resolvers []string) string {
	t.Helper()

	cfg := defaultTestConfig()
	if len(resolvers) > 0 {
		cfg.Resolvers = resolvers
	}

	return startTestDNSServerWithConfig(t, dc, cfg)
}

// startTestDNSServerWithConfig is like startTestDNSServer but accepts a full
// config, useful for tests that need non-default settings (e.g. multiple TLDs).
func startTestDNSServerWithConfig(t *testing.T, dc *mockDockerClient, cfg *config.Config) string {
	t.Helper()

	c := cache.New(cfg.TTL, cfg.MaxCacheSize)
	t.Cleanup(c.Stop)

	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	srv := New(cfg, c, dc, log)

	// Bind once; hand the conn to dns.Server to avoid releasing it between bind and use.
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("bind: %v", err)
	}
	addr := pc.LocalAddr().String()

	mux := dns.NewServeMux()
	mux.HandleFunc(".", srv.HandleQuery)

	dnsSrv := &dns.Server{
		PacketConn: pc,
		Net:        "udp",
		Handler:    mux,
	}

	started := make(chan struct{})
	dnsSrv.NotifyStartedFunc = func() { close(started) }

	go func() { _ = dnsSrv.ActivateAndServe() }()
	<-started
	t.Cleanup(func() { _ = dnsSrv.Shutdown() })

	return addr
}

// startFakeUpstream starts a minimal DNS server that answers every query with
// the given rcode and, on success, the provided IP.
func startFakeUpstream(t *testing.T, answerIP string, rcode int) string {
	t.Helper()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("bind fake upstream: %v", err)
	}
	addr := pc.LocalAddr().String()

	mux := dns.NewServeMux()
	mux.HandleFunc(".", func(w dns.ResponseWriter, req *dns.Msg) {
		resp := new(dns.Msg)
		resp.SetReply(req)
		resp.Rcode = rcode
		if rcode == dns.RcodeSuccess && answerIP != "" && len(req.Question) > 0 {
			resp.Answer = append(resp.Answer, &dns.A{
				Hdr: dns.RR_Header{
					Name:   req.Question[0].Name,
					Rrtype: dns.TypeA,
					Class:  dns.ClassINET,
					Ttl:    60,
				},
				A: net.ParseIP(answerIP).To4(),
			})
		}
		_ = w.WriteMsg(resp)
	})

	srv := &dns.Server{PacketConn: pc, Net: "udp", Handler: mux}
	started := make(chan struct{})
	srv.NotifyStartedFunc = func() { close(started) }
	go func() { _ = srv.ActivateAndServe() }()
	<-started
	t.Cleanup(func() { _ = srv.Shutdown() })

	return addr
}

// queryDNS sends a single DNS query and returns the response.
func queryDNS(t *testing.T, addr, domain string, qtype uint16) *dns.Msg {
	t.Helper()
	c := &dns.Client{Timeout: 3 * time.Second}
	m := new(dns.Msg)
	m.SetQuestion(dns.Fqdn(domain), qtype)
	m.RecursionDesired = true
	resp, _, err := c.Exchange(m, addr)
	if err != nil {
		t.Fatalf("DNS exchange: %v", err)
	}
	return resp
}
