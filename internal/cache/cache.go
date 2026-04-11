// Package cache provides a thread-safe, TTL-bounded, size-limited DNS record cache
// with background eviction and hit/miss telemetry.
package cache

import (
	"sync"
	"sync/atomic"
	"time"
)

// entry is a single cached record.
type entry struct {
	values []string
	expiry time.Time
}

// Stats is a snapshot of cache telemetry.
type Stats struct {
	Hits    uint64
	Misses  uint64
	Entries int
}

// Cache is a concurrency-safe, TTL-backed cache for DNS records.
// Expired entries are removed by a background goroutine; when maxSize is
// exceeded the oldest entry is evicted immediately.
type Cache struct {
	mu      sync.RWMutex
	items   map[string]entry
	ttl     time.Duration
	maxSize int

	hits   atomic.Uint64
	misses atomic.Uint64

	stop chan struct{}
	done chan struct{}
}

// New creates a Cache and starts its background eviction loop.
// Call Stop to release resources.
func New(ttl time.Duration, maxSize int) *Cache {
	c := &Cache{
		items:   make(map[string]entry),
		ttl:     ttl,
		maxSize: maxSize,
		stop:    make(chan struct{}),
		done:    make(chan struct{}),
	}
	go c.evictLoop()
	return c
}

// Get returns the cached values for key and whether it was a valid (non-expired) hit.
func (c *Cache) Get(key string) ([]string, bool) {
	c.mu.RLock()
	e, ok := c.items[key]
	c.mu.RUnlock()

	if !ok {
		c.misses.Add(1)
		return nil, false
	}
	if time.Now().After(e.expiry) {
		// Lazy delete on read so we do not promote the RLock to a write lock
		// in the hot path; the eviction loop will clean this up.
		c.misses.Add(1)
		return nil, false
	}

	c.hits.Add(1)
	// Return a copy so callers cannot mutate cached state.
	cp := make([]string, len(e.values))
	copy(cp, e.values)
	return cp, true
}

// Set stores values for key, overwriting any existing entry.
// If maxSize > 0 and the cache is full, the oldest entry is evicted first.
func (c *Cache) Set(key string, values []string) {
	if len(values) == 0 {
		return // do not cache empty results
	}

	cp := make([]string, len(values))
	copy(cp, values)

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.maxSize > 0 && len(c.items) >= c.maxSize {
		c.evictOldestLocked()
	}
	c.items[key] = entry{values: cp, expiry: time.Now().Add(c.ttl)}
}

// Delete removes a specific key from the cache.
func (c *Cache) Delete(key string) {
	c.mu.Lock()
	delete(c.items, key)
	c.mu.Unlock()
}

// Stats returns a point-in-time snapshot of cache metrics.
func (c *Cache) Stats() Stats {
	c.mu.RLock()
	n := len(c.items)
	c.mu.RUnlock()
	return Stats{
		Hits:    c.hits.Load(),
		Misses:  c.misses.Load(),
		Entries: n,
	}
}

// Stop gracefully shuts down the background eviction goroutine.
func (c *Cache) Stop() {
	close(c.stop)
	<-c.done
}

// evictLoop runs in a goroutine and removes expired entries at half the TTL interval.
func (c *Cache) evictLoop() {
	defer close(c.done)

	interval := c.ttl / 2
	if interval < time.Second {
		interval = time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-c.stop:
			return
		case <-ticker.C:
			c.evictExpired()
		}
	}
}

func (c *Cache) evictExpired() {
	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()
	for k, e := range c.items {
		if now.After(e.expiry) {
			delete(c.items, k)
		}
	}
}

// evictOldestLocked removes the entry with the earliest expiry.
// Must be called with c.mu held for writing.
func (c *Cache) evictOldestLocked() {
	var (
		oldestKey    string
		oldestExpiry time.Time
	)
	first := true
	for k, e := range c.items {
		if first || e.expiry.Before(oldestExpiry) {
			oldestKey = k
			oldestExpiry = e.expiry
			first = false
		}
	}
	if oldestKey != "" {
		delete(c.items, oldestKey)
	}
}
