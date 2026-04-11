package server

import "sync/atomic"

// Metrics holds atomic counters for all server events.
// All fields are safe for concurrent access.
type Metrics struct {
	QueriesTotal  atomic.Uint64
	CacheHits     atomic.Uint64
	CacheMisses   atomic.Uint64
	DockerLookups atomic.Uint64
	DockerErrors  atomic.Uint64
	ForwardQueries atomic.Uint64
	ForwardErrors  atomic.Uint64
	RateLimited   atomic.Uint64
}

func newMetrics() *Metrics {
	return &Metrics{}
}
