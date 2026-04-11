package cache

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestGetSetBasic(t *testing.T) {
	c := New(5*time.Second, 0)
	defer c.Stop()

	_, hit := c.Get("missing.docker.")
	if hit {
		t.Fatal("expected miss for uncached key")
	}

	c.Set("foo.docker.", []string{"10.0.0.1", "10.0.0.2"})
	vals, hit := c.Get("foo.docker.")
	if !hit {
		t.Fatal("expected hit after Set")
	}
	if len(vals) != 2 || vals[0] != "10.0.0.1" {
		t.Errorf("unexpected values: %v", vals)
	}
}

func TestExpiry(t *testing.T) {
	c := New(50*time.Millisecond, 0)
	defer c.Stop()

	c.Set("expiring.docker.", []string{"1.2.3.4"})

	// Should hit immediately.
	if _, hit := c.Get("expiring.docker."); !hit {
		t.Fatal("expected hit before expiry")
	}

	time.Sleep(100 * time.Millisecond)

	// Should miss after expiry.
	if _, hit := c.Get("expiring.docker."); hit {
		t.Fatal("expected miss after TTL elapsed")
	}
}

func TestNoCacheEmptyValues(t *testing.T) {
	c := New(5*time.Second, 0)
	defer c.Stop()

	c.Set("empty.docker.", []string{})
	if _, hit := c.Get("empty.docker."); hit {
		t.Fatal("should not cache empty value slice")
	}
}

func TestMaxSizeEviction(t *testing.T) {
	c := New(10*time.Second, 3)
	defer c.Stop()

	c.Set("a.docker.", []string{"1.1.1.1"})
	c.Set("b.docker.", []string{"2.2.2.2"})
	c.Set("c.docker.", []string{"3.3.3.3"})

	// Adding a 4th entry should evict the oldest.
	c.Set("d.docker.", []string{"4.4.4.4"})

	stats := c.Stats()
	if stats.Entries > 3 {
		t.Errorf("cache size %d exceeds maxSize 3", stats.Entries)
	}
}

func TestDelete(t *testing.T) {
	c := New(10*time.Second, 0)
	defer c.Stop()

	c.Set("del.docker.", []string{"9.9.9.9"})
	c.Delete("del.docker.")

	if _, hit := c.Get("del.docker."); hit {
		t.Fatal("expected miss after Delete")
	}
}

func TestReturnsCopy(t *testing.T) {
	c := New(10*time.Second, 0)
	defer c.Stop()

	original := []string{"10.0.0.1"}
	c.Set("copy.docker.", original)

	vals, _ := c.Get("copy.docker.")
	vals[0] = "mutated"

	// A second Get should still return the original value.
	vals2, _ := c.Get("copy.docker.")
	if vals2[0] != "10.0.0.1" {
		t.Errorf("cache returned mutated value: %s", vals2[0])
	}
}

func TestConcurrentAccess(t *testing.T) {
	c := New(10*time.Second, 100)
	defer c.Stop()

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		key := fmt.Sprintf("host%d.docker.", i)
		go func(k string) {
			defer wg.Done()
			c.Set(k, []string{"10.0.0.1"})
		}(key)
		go func(k string) {
			defer wg.Done()
			c.Get(k)
		}(key)
	}
	wg.Wait()
}

func TestStats(t *testing.T) {
	c := New(10*time.Second, 0)
	defer c.Stop()

	c.Get("miss1.docker.")
	c.Get("miss2.docker.")
	c.Set("hit.docker.", []string{"1.2.3.4"})
	c.Get("hit.docker.")

	stats := c.Stats()
	if stats.Misses != 2 {
		t.Errorf("expected 2 misses, got %d", stats.Misses)
	}
	if stats.Hits != 1 {
		t.Errorf("expected 1 hit, got %d", stats.Hits)
	}
	if stats.Entries != 1 {
		t.Errorf("expected 1 entry, got %d", stats.Entries)
	}
}
