package server

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/miekg/dns"
)

func TestHandleLocal_ARecord(t *testing.T) {
	dc := &mockDockerClient{
		ipsFunc: func(_ context.Context, name string) ([]string, error) {
			if name == "myapp" {
				return []string{"172.17.0.2"}, nil
			}
			return nil, nil
		},
	}
	addr := startTestDNSServer(t, dc, nil)

	resp := queryDNS(t, addr, "myapp.docker.", dns.TypeA)

	if resp.Rcode != dns.RcodeSuccess {
		t.Fatalf("expected NOERROR, got %s", dns.RcodeToString[resp.Rcode])
	}
	if !resp.Authoritative {
		t.Error("local domain must be answered authoritatively")
	}
	if len(resp.Answer) != 1 {
		t.Fatalf("expected 1 answer, got %d", len(resp.Answer))
	}
	a, ok := resp.Answer[0].(*dns.A)
	if !ok {
		t.Fatalf("expected *dns.A, got %T", resp.Answer[0])
	}
	if got := a.A.String(); got != "172.17.0.2" {
		t.Errorf("IP: got %s, want 172.17.0.2", got)
	}
}

func TestHandleLocal_ContainerNotFound_NXDOMAIN(t *testing.T) {
	dc := &mockDockerClient{
		ipsFunc: func(_ context.Context, _ string) ([]string, error) {
			return nil, nil // not found
		},
	}
	addr := startTestDNSServer(t, dc, nil)
	resp := queryDNS(t, addr, "ghost.docker.", dns.TypeA)

	if resp.Rcode != dns.RcodeNameError {
		t.Errorf("expected NXDOMAIN, got %s", dns.RcodeToString[resp.Rcode])
	}
	if !resp.Authoritative {
		t.Error("NXDOMAIN must be authoritative for managed TLD")
	}
}

func TestHandleLocal_DockerError_SERVFAIL(t *testing.T) {
	dc := &mockDockerClient{
		ipsFunc: func(_ context.Context, _ string) ([]string, error) {
			return nil, fmt.Errorf("daemon unreachable")
		},
	}
	addr := startTestDNSServer(t, dc, nil)
	resp := queryDNS(t, addr, "broken.docker.", dns.TypeA)

	if resp.Rcode != dns.RcodeServerFailure {
		t.Errorf("expected SERVFAIL, got %s", dns.RcodeToString[resp.Rcode])
	}
}

func TestHandleLocal_UnsupportedType_NOTIMP(t *testing.T) {
	dc := &mockDockerClient{ipsFunc: func(_ context.Context, _ string) ([]string, error) {
		return nil, nil
	}}
	addr := startTestDNSServer(t, dc, nil)

	resp := queryDNS(t, addr, "myapp.docker.", dns.TypeMX)
	if resp.Rcode != dns.RcodeNotImplemented {
		t.Errorf("expected NOTIMP, got %s", dns.RcodeToString[resp.Rcode])
	}
}

func TestHandleLocal_AAAA_EmptyNoError(t *testing.T) {
	dc := &mockDockerClient{
		ipsFunc: func(_ context.Context, _ string) ([]string, error) {
			return []string{"10.0.0.1"}, nil
		},
	}
	addr := startTestDNSServer(t, dc, nil)

	// IPv4-only container → NOERROR with empty answer section for AAAA.
	resp := queryDNS(t, addr, "myapp.docker.", dns.TypeAAAA)
	if resp.Rcode != dns.RcodeSuccess {
		t.Errorf("expected NOERROR for AAAA, got %s", dns.RcodeToString[resp.Rcode])
	}
	if len(resp.Answer) != 0 {
		t.Errorf("expected 0 AAAA answers, got %d", len(resp.Answer))
	}
}

func TestHandleLocal_MultipleIPs(t *testing.T) {
	dc := &mockDockerClient{
		ipsFunc: func(_ context.Context, _ string) ([]string, error) {
			return []string{"10.0.0.1", "10.0.0.2", "10.0.0.3"}, nil
		},
	}
	addr := startTestDNSServer(t, dc, nil)
	resp := queryDNS(t, addr, "multi.docker.", dns.TypeA)

	if len(resp.Answer) != 3 {
		t.Errorf("expected 3 A records, got %d", len(resp.Answer))
	}
}

func TestHandleLocal_CachePreventsExtraDockerCalls(t *testing.T) {
	var mu sync.Mutex
	callCount := 0
	dc := &mockDockerClient{
		ipsFunc: func(_ context.Context, _ string) ([]string, error) {
			mu.Lock()
			callCount++
			mu.Unlock()
			return []string{"10.0.0.1"}, nil
		},
	}
	addr := startTestDNSServer(t, dc, nil)

	// Two identical queries; the second must be served from cache.
	queryDNS(t, addr, "cached.docker.", dns.TypeA)
	queryDNS(t, addr, "cached.docker.", dns.TypeA)

	mu.Lock()
	got := callCount
	mu.Unlock()
	if got > 1 {
		t.Errorf("expected ≤1 Docker call, got %d", got)
	}
}

func TestHandleLocal_ThunderingHerdCollapsed(t *testing.T) {
	var mu sync.Mutex
	callCount := 0

	dc := &mockDockerClient{
		ipsFunc: func(_ context.Context, _ string) ([]string, error) {
			mu.Lock()
			callCount++
			mu.Unlock()
			return []string{"10.1.2.3"}, nil
		},
	}
	addr := startTestDNSServer(t, dc, nil)

	// Simulate a burst of 20 concurrent queries for the same uncached domain.
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp := queryDNS(t, addr, "burst.docker.", dns.TypeA)
			if resp.Rcode != dns.RcodeSuccess {
				t.Errorf("expected NOERROR, got %s", dns.RcodeToString[resp.Rcode])
			}
		}()
	}
	wg.Wait()

	mu.Lock()
	got := callCount
	mu.Unlock()
	// singleflight should have collapsed most calls into 1 or very few.
	if got > 3 {
		t.Errorf("thundering herd: expected ≤3 Docker calls, got %d", got)
	}
}

func TestHandleLocal_MultipleTLDs(t *testing.T) {
	dc := &mockDockerClient{
		ipsFunc: func(_ context.Context, name string) ([]string, error) {
			switch name {
			case "webapp":
				return []string{"172.17.0.2"}, nil
			case "api":
				return []string{"172.17.0.3"}, nil
			default:
				return nil, nil
			}
		},
	}

	cfg := defaultTestConfig()
	cfg.TLDs = []string{"docker", "local"}

	addr := startTestDNSServerWithConfig(t, dc, cfg)

	// Query via first TLD.
	resp := queryDNS(t, addr, "webapp.docker.", dns.TypeA)
	if resp.Rcode != dns.RcodeSuccess {
		t.Fatalf("expected NOERROR for .docker, got %s", dns.RcodeToString[resp.Rcode])
	}
	if len(resp.Answer) != 1 {
		t.Fatalf("expected 1 answer for .docker, got %d", len(resp.Answer))
	}
	if a := resp.Answer[0].(*dns.A); a.A.String() != "172.17.0.2" {
		t.Errorf("IP for .docker: got %s, want 172.17.0.2", a.A)
	}

	// Query via second TLD.
	resp = queryDNS(t, addr, "api.local.", dns.TypeA)
	if resp.Rcode != dns.RcodeSuccess {
		t.Fatalf("expected NOERROR for .local, got %s", dns.RcodeToString[resp.Rcode])
	}
	if len(resp.Answer) != 1 {
		t.Fatalf("expected 1 answer for .local, got %d", len(resp.Answer))
	}
	if a := resp.Answer[0].(*dns.A); a.A.String() != "172.17.0.3" {
		t.Errorf("IP for .local: got %s, want 172.17.0.3", a.A)
	}

	// NXDOMAIN for unknown container on second TLD.
	resp = queryDNS(t, addr, "ghost.local.", dns.TypeA)
	if resp.Rcode != dns.RcodeNameError {
		t.Errorf("expected NXDOMAIN for ghost.local, got %s", dns.RcodeToString[resp.Rcode])
	}

	// Non-managed TLD should be forwarded (not authoritative).
	resp = queryDNS(t, addr, "example.com.", dns.TypeA)
	if resp.Authoritative {
		t.Error("non-managed TLD must not be authoritative")
	}
}

func TestExtractContainerName(t *testing.T) {
	cases := []struct {
		fqdn   string
		suffix string
		want   string
	}{
		{"myapp.docker.", ".docker.", "myapp"},
		{"my-service.docker.", ".docker.", "my-service"},
		{"web.docker.", ".docker.", "web"},
	}
	for _, tc := range cases {
		got := ExtractContainerName(tc.fqdn, tc.suffix)
		if got != tc.want {
			t.Errorf("ExtractContainerName(%q, %q) = %q, want %q",
				tc.fqdn, tc.suffix, got, tc.want)
		}
	}
}
