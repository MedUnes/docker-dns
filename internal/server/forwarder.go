package server

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/miekg/dns"
)

// Forwarder dispatches DNS queries to a set of upstream resolvers in parallel
// and returns the first successful response.
type Forwarder struct {
	resolvers []string // host IPs (port 53 appended at call time)
	timeout   time.Duration
	log       *slog.Logger
	metrics   *Metrics
}

func newForwarder(resolvers []string, timeout time.Duration, log *slog.Logger, m *Metrics) *Forwarder {
	return &Forwarder{
		resolvers: resolvers,
		timeout:   timeout,
		log:       log,
		metrics:   m,
	}
}

// forwardResult carries the outcome of a single resolver attempt.
type forwardResult struct {
	resp *dns.Msg
	err  error
}

// Forward fans out req to all configured resolvers in parallel and returns the
// first non-error response with a successful Rcode. If all resolvers fail it
// returns an aggregated error.
func (f *Forwarder) Forward(ctx context.Context, req *dns.Msg) (*dns.Msg, error) {
	if len(f.resolvers) == 0 {
		return nil, fmt.Errorf("no resolvers configured")
	}

	// Build the upstream query from the original request.
	m := new(dns.Msg)
	if len(req.Question) > 0 {
		q := req.Question[0]
		m.SetQuestion(q.Name, q.Qtype)
	} else {
		m = req.Copy()
	}
	m.RecursionDesired = true

	// Copy EDNS0 from original so upstream honours the correct buffer size.
	if opt := req.IsEdns0(); opt != nil {
		m.SetEdns0(opt.UDPSize(), opt.Do())
	}

	resultCh := make(chan forwardResult, len(f.resolvers))
	var wg sync.WaitGroup

	for _, resolver := range f.resolvers {
		wg.Add(1)
		go func(addr string) {
			defer wg.Done()
			// Append default DNS port only when the resolver has no explicit port.
			if _, _, err := net.SplitHostPort(addr); err != nil {
				addr = net.JoinHostPort(addr, "53")
			}
			resp, err := f.queryResolver(ctx, m, addr)
			resultCh <- forwardResult{resp: resp, err: err}
		}(resolver)
	}

	// Close resultCh once all goroutines have sent their results.
	go func() {
		wg.Wait()
		close(resultCh)
	}()

	// Collect results: return the first success; keep the best non-success
	// response (e.g. NXDOMAIN) to pass through if all resolvers fail.
	var (
		lastErr      error
		bestResponse *dns.Msg
	)
	for res := range resultCh {
		if res.err == nil && res.resp != nil {
			if res.resp.Rcode == dns.RcodeSuccess {
				return res.resp, nil
			}
			// Prefer NXDOMAIN over SERVFAIL when choosing a fallback to surface.
			if bestResponse == nil || res.resp.Rcode == dns.RcodeNameError {
				bestResponse = res.resp
			}
			lastErr = fmt.Errorf("upstream rcode %s", dns.RcodeToString[res.resp.Rcode])
		} else if res.err != nil {
			lastErr = res.err
		}
	}

	// Pass through canonical upstream answers (e.g. NXDOMAIN) rather than
	// synthesising a SERVFAIL when the name genuinely doesn't exist.
	if bestResponse != nil {
		return bestResponse, nil
	}
	if lastErr != nil {
		return nil, fmt.Errorf("all %d resolvers failed: %w", len(f.resolvers), lastErr)
	}
	return nil, fmt.Errorf("no response from any resolver")
}

// queryResolver performs a single DNS exchange with addr, respecting ctx deadline.
func (f *Forwarder) queryResolver(ctx context.Context, m *dns.Msg, addr string) (*dns.Msg, error) {
	c := &dns.Client{
		Timeout: f.timeout,
	}

	f.log.Debug("querying resolver", "addr", addr, "domain", m.Question[0].Name)
	resp, _, err := c.ExchangeContext(ctx, m, addr)
	if err != nil {
		f.log.Debug("resolver error", "addr", addr, "error", err)
		return nil, fmt.Errorf("resolver %s: %w", addr, err)
	}

	f.log.Debug("resolver responded",
		"addr", addr,
		"rcode", dns.RcodeToString[resp.Rcode],
		"answers", len(resp.Answer),
	)
	return resp, nil
}
