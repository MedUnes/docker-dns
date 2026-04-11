// Package server wires together the DNS handler, forwarder, cache, and Docker
// client into a runnable service with graceful shutdown.
package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/medunes/docker-dns/internal/cache"
	"github.com/medunes/docker-dns/internal/config"
	"github.com/medunes/docker-dns/internal/docker"
	"github.com/miekg/dns"
	"golang.org/x/sync/singleflight"
)

// Server is the top-level DNS service.
type Server struct {
	cfg       *config.Config
	cache     *cache.Cache
	docker    docker.Client
	log       *slog.Logger
	metrics   *Metrics
	sfGroup   singleflight.Group
	forwarder *Forwarder
	rateLim   *RateLimiter
}

// New constructs a Server. All arguments are required.
func New(cfg *config.Config, c *cache.Cache, dc docker.Client, log *slog.Logger) *Server {
	s := &Server{
		cfg:     cfg,
		cache:   c,
		docker:  dc,
		log:     log,
		metrics: newMetrics(),
	}
	s.forwarder = newForwarder(cfg.Resolvers, cfg.ForwardTimeout, log, s.metrics)
	if cfg.RateLimit > 0 {
		s.rateLim = newRateLimiter(cfg.RateLimit, cfg.RateBurst, log)
	}
	return s
}

// Run starts the DNS servers (UDP + TCP) and the optional HTTP server.
// It blocks until ctx is cancelled, then performs a graceful drain.
func (s *Server) Run(ctx context.Context) error {
	mux := dns.NewServeMux()
	mux.HandleFunc(".", s.handleQuery)

	addr := fmt.Sprintf("%s:53", s.cfg.ListenIP)
	udpSrv := &dns.Server{Addr: addr, Net: "udp", Handler: mux}
	tcpSrv := &dns.Server{Addr: addr, Net: "tcp", Handler: mux}

	s.log.Info("starting DNS server",
		"addr", addr,
		"tlds", s.cfg.TLDs,
		"ttl", s.cfg.TTL,
		"resolvers", s.cfg.Resolvers,
	)

	errCh := make(chan error, 3)
	var wg sync.WaitGroup

	launch := func(srv *dns.Server, label string) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := srv.ListenAndServe(); err != nil {
				errCh <- fmt.Errorf("%s: %w", label, err)
			}
		}()
	}
	launch(udpSrv, "udp dns")
	launch(tcpSrv, "tcp dns")

	var httpSrv *http.Server
	if s.cfg.HTTPAddr != "" {
		httpSrv = s.newHTTPServer()
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.log.Info("starting HTTP server", "addr", s.cfg.HTTPAddr)
			if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				errCh <- fmt.Errorf("http: %w", err)
			}
		}()
	}

	if s.rateLim != nil {
		go s.rateLim.cleanupLoop(ctx)
	}

	select {
	case <-ctx.Done():
		s.log.Info("shutdown signal received")
	case err := <-errCh:
		s.log.Error("server error", "error", err)
		return err
	}

	shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if httpSrv != nil {
		_ = httpSrv.Shutdown(shutCtx)
	}
	_ = udpSrv.ShutdownContext(shutCtx)
	_ = tcpSrv.ShutdownContext(shutCtx)
	wg.Wait()

	s.log.Info("all servers stopped cleanly")
	return nil
}

func (s *Server) newHTTPServer() *http.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.httpHealth)
	mux.HandleFunc("/metrics", s.httpMetrics)
	return &http.Server{
		Addr:         s.cfg.HTTPAddr,
		Handler:      mux,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
	}
}

func (s *Server) httpHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}

func (s *Server) httpMetrics(w http.ResponseWriter, _ *http.Request) {
	cs := s.cache.Stats()
	payload := map[string]any{
		"queries_total":   s.metrics.QueriesTotal.Load(),
		"cache_hits":      s.metrics.CacheHits.Load(),
		"cache_misses":    s.metrics.CacheMisses.Load(),
		"cache_entries":   cs.Entries,
		"docker_lookups":  s.metrics.DockerLookups.Load(),
		"docker_errors":   s.metrics.DockerErrors.Load(),
		"forward_queries": s.metrics.ForwardQueries.Load(),
		"forward_errors":  s.metrics.ForwardErrors.Load(),
		"rate_limited":    s.metrics.RateLimited.Load(),
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(payload)
}
