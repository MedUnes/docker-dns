package server

import (
	"context"
	"testing"
	"time"

	"github.com/miekg/dns"
)

func TestForwarder_SuccessfulUpstream(t *testing.T) {
	upstream := startFakeUpstream(t, "1.2.3.4", dns.RcodeSuccess)
	addr := startTestDNSServer(t, noopDocker(), []string{upstream})

	resp := queryDNS(t, addr, "example.com.", dns.TypeA)

	if resp.Rcode != dns.RcodeSuccess {
		t.Fatalf("expected NOERROR, got %s", dns.RcodeToString[resp.Rcode])
	}
	if len(resp.Answer) != 1 {
		t.Fatalf("expected 1 answer, got %d", len(resp.Answer))
	}
	a, ok := resp.Answer[0].(*dns.A)
	if !ok {
		t.Fatalf("expected A record")
	}
	if a.A.String() != "1.2.3.4" {
		t.Errorf("IP: got %s, want 1.2.3.4", a.A)
	}
	if resp.Authoritative {
		t.Error("forwarded response must not be authoritative")
	}
}

func TestForwarder_NXDOMAINPassthrough(t *testing.T) {
	upstream := startFakeUpstream(t, "", dns.RcodeNameError)
	addr := startTestDNSServer(t, noopDocker(), []string{upstream})

	resp := queryDNS(t, addr, "notexist.example.com.", dns.TypeA)
	if resp.Rcode != dns.RcodeNameError {
		t.Errorf("expected NXDOMAIN passthrough, got %s", dns.RcodeToString[resp.Rcode])
	}
}

func TestForwarder_ParallelFallback(t *testing.T) {
	// First resolver fails; second succeeds. Parallel fan-out should succeed.
	badUpstream := startFakeUpstream(t, "", dns.RcodeServerFailure)
	goodUpstream := startFakeUpstream(t, "9.8.7.6", dns.RcodeSuccess)

	addr := startTestDNSServer(t, noopDocker(), []string{badUpstream, goodUpstream})

	resp := queryDNS(t, addr, "example.com.", dns.TypeA)
	if resp.Rcode != dns.RcodeSuccess {
		t.Fatalf("expected NOERROR via good resolver, got %s", dns.RcodeToString[resp.Rcode])
	}
}

func TestForwarder_AllDown_SERVFAIL(t *testing.T) {
	// Use unroutable addresses so all fail.
	addr := startTestDNSServer(t, noopDocker(), []string{"192.0.2.1", "192.0.2.2"})

	c := &dns.Client{Timeout: 6 * time.Second}
	m := new(dns.Msg)
	m.SetQuestion("example.com.", dns.TypeA)
	resp, _, err := c.Exchange(m, addr)
	if err != nil {
		t.Fatalf("exchange: %v", err)
	}
	if resp.Rcode != dns.RcodeServerFailure {
		t.Errorf("expected SERVFAIL, got %s", dns.RcodeToString[resp.Rcode])
	}
}

// noopDocker is a helper that returns a mock Docker client which always
// reports containers as not found – used in forwarding-focused tests.
func noopDocker() *mockDockerClient {
	return &mockDockerClient{
		ipsFunc: func(_ context.Context, _ string) ([]string, error) {
			return nil, nil
		},
	}
}
