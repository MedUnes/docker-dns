package server

import (
	"context"
	"net"
	"strings"
	"time"

	"github.com/miekg/dns"
)

// HandleQuery is the main DNS handler. It is invoked in a goroutine per query
// by the miekg/dns server. Exported so it can be registered on a custom mux
// in tests.
func (s *Server) HandleQuery(w dns.ResponseWriter, req *dns.Msg) {
	s.handleQuery(w, req)
}

func (s *Server) handleQuery(w dns.ResponseWriter, req *dns.Msg) {
	s.metrics.QueriesTotal.Add(1)

	// --- Rate limiting ---
	if s.rateLim != nil {
		clientIP, _, _ := net.SplitHostPort(w.RemoteAddr().String())
		if !s.rateLim.Allow(clientIP) {
			s.metrics.RateLimited.Add(1)
			s.log.Debug("rate limited", "client", clientIP)
			refuseMsg := new(dns.Msg)
			refuseMsg.SetRcode(req, dns.RcodeRefused)
			_ = w.WriteMsg(refuseMsg)
			return
		}
	}

	// --- Build base response ---
	resp := new(dns.Msg)
	resp.SetReply(req)

	// Copy EDNS0 options; track the advertised UDP size for truncation checks.
	var edns0UDPSize uint16 = dns.DefaultMsgSize
	if opt := req.IsEdns0(); opt != nil {
		edns0UDPSize = opt.UDPSize()
		resp.SetEdns0(edns0UDPSize, opt.Do())
	}

	// RFC 1035 §4.1.2: a request with no questions is a format error.
	if len(req.Question) == 0 {
		s.log.Debug("received query with no questions")
		resp.SetRcode(req, dns.RcodeFormatError)
		s.writeResponse(w, resp, edns0UDPSize)
		return
	}

	// Only the first question is processed (standard practice per RFC 1035).
	q := req.Question[0]
	domain := strings.ToLower(q.Name)

	s.log.Debug("query received", "domain", domain, "type", dns.TypeToString[q.Qtype])

	if suffix := s.cfg.MatchLocalSuffix(domain); suffix != "" {
		s.handleLocal(w, req, resp, q, domain, suffix, edns0UDPSize)
	} else {
		s.handleForward(w, req, resp, q, edns0UDPSize)
	}
}

// handleLocal resolves queries for our managed TLDs from cache or Docker.
func (s *Server) handleLocal(
	w dns.ResponseWriter,
	req *dns.Msg,
	resp *dns.Msg,
	q dns.Question,
	domain string,
	suffix string,
	udpSize uint16,
) {
	// We only handle A and AAAA for container resolution.
	if q.Qtype != dns.TypeA && q.Qtype != dns.TypeAAAA {
		resp.SetRcode(req, dns.RcodeNotImplemented)
		s.writeResponse(w, resp, udpSize)
		return
	}

	// Authoritative only for our own TLD.
	resp.Authoritative = true

	ips, ok := s.cache.Get(domain)
	if ok {
		s.metrics.CacheHits.Add(1)
		s.log.Debug("cache hit", "domain", domain, "ips", ips)
	} else {
		s.metrics.CacheMisses.Add(1)
		s.log.Debug("cache miss", "domain", domain)

		var err error
		ips, err = s.fetchFromDocker(domain, suffix)
		if err != nil {
			s.log.Error("docker lookup failed", "domain", domain, "error", err)
			s.metrics.DockerErrors.Add(1)
			resp.SetRcode(req, dns.RcodeServerFailure)
			s.writeResponse(w, resp, udpSize)
			return
		}

		if len(ips) > 0 {
			s.cache.Set(domain, ips)
		}
	}

	if len(ips) == 0 {
		// Authoritative NXDOMAIN: we own this TLD and the name is unknown.
		s.log.Debug("NXDOMAIN", "domain", domain)
		resp.SetRcode(req, dns.RcodeNameError)
		s.writeResponse(w, resp, udpSize)
		return
	}

	// Populate A records. AAAA queries receive an empty authoritative NOERROR
	// because the docker client currently only extracts IPv4 addresses.
	if q.Qtype == dns.TypeA {
		for _, ipStr := range ips {
			ip := net.ParseIP(ipStr)
			if ip == nil || ip.To4() == nil {
				s.log.Debug("skipping non-IPv4 address", "ip", ipStr, "domain", domain)
				continue
			}
			resp.Answer = append(resp.Answer, &dns.A{
				Hdr: dns.RR_Header{
					Name:   q.Name,
					Rrtype: dns.TypeA,
					Class:  dns.ClassINET,
					Ttl:    uint32(s.cfg.TTL.Seconds()),
				},
				A: ip.To4(),
			})
		}
	}

	s.log.Debug("local query answered", "domain", domain, "answers", len(resp.Answer))
	s.writeResponse(w, resp, udpSize)
}

// handleForward proxies non-local queries to upstream resolvers.
func (s *Server) handleForward(
	w dns.ResponseWriter,
	req *dns.Msg,
	resp *dns.Msg,
	q dns.Question,
	udpSize uint16,
) {
	resp.Authoritative = false

	s.metrics.ForwardQueries.Add(1)
	s.log.Debug("forwarding query", "domain", q.Name, "type", dns.TypeToString[q.Qtype])

	// Allow the forwarder enough time to try all resolvers in parallel.
	totalTimeout := s.cfg.ForwardTimeout + 500*time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), totalTimeout)
	defer cancel()

	upstream, err := s.forwarder.Forward(ctx, req)
	if err != nil {
		s.log.Warn("all forwarders failed", "domain", q.Name, "error", err)
		s.metrics.ForwardErrors.Add(1)
		resp.SetRcode(req, dns.RcodeServerFailure)
		s.writeResponse(w, resp, udpSize)
		return
	}

	resp.Answer = upstream.Answer
	resp.Ns = upstream.Ns
	resp.Extra = upstream.Extra
	resp.Rcode = upstream.Rcode
	resp.RecursionAvailable = upstream.RecursionAvailable

	s.writeResponse(w, resp, udpSize)
}

// fetchFromDocker uses singleflight to coalesce concurrent cache misses for
// the same domain into a single Docker API call (thundering-herd prevention).
func (s *Server) fetchFromDocker(domain, suffix string) ([]string, error) {
	containerName := extractContainerName(domain, suffix)

	result, err, _ := s.sfGroup.Do(domain, func() (any, error) {
		s.metrics.DockerLookups.Add(1)
		s.log.Debug("docker inspect", "container", containerName)

		ctx, cancel := context.WithTimeout(context.Background(), s.cfg.DockerTimeout)
		defer cancel()

		return s.docker.ContainerIPs(ctx, containerName)
	})
	if err != nil {
		return nil, err
	}

	ips, _ := result.([]string)
	return ips, nil
}

// ExtractContainerName strips the managed TLD suffix from the FQDN to derive
// the bare container name used in Docker inspect. Exported for testing.
func ExtractContainerName(fqdn, suffix string) string {
	return extractContainerName(fqdn, suffix)
}

// extractContainerName strips the managed TLD suffix from the FQDN to derive
// the bare container name used in Docker inspect.
func extractContainerName(fqdn, suffix string) string {
	name := strings.TrimSuffix(fqdn, suffix)
	return strings.TrimSuffix(name, ".")
}

// writeResponse writes a DNS response, enforcing EDNS0 UDP payload limits and
// setting the TC (truncation) bit when the message exceeds the UDP budget.
// TCP connections are written without size constraints.
func (s *Server) writeResponse(w dns.ResponseWriter, msg *dns.Msg, maxUDPSize uint16) {
	if _, isTCP := w.RemoteAddr().(*net.TCPAddr); isTCP {
		if err := w.WriteMsg(msg); err != nil {
			s.log.Error("tcp write failed", "error", err)
		}
		return
	}

	packed, err := msg.Pack()
	if err != nil {
		s.log.Error("failed to pack DNS message", "error", err)
		return
	}

	if uint16(len(packed)) > maxUDPSize {
		// Strip payload sections and set TC so the client retries over TCP.
		msg.Truncated = true
		msg.Answer = nil
		msg.Ns = nil
		msg.Extra = nil
		s.log.Debug("response truncated", "size", len(packed), "limit", maxUDPSize)
	}

	if err := w.WriteMsg(msg); err != nil {
		s.log.Error("udp write failed", "error", err)
	}
}
