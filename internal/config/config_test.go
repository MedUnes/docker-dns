package config

import (
	"testing"
	"time"
)

func TestValidate(t *testing.T) {
	base := func() *Config {
		return &Config{
			ListenIP:       "127.0.0.1",
			TLDs:           []string{"docker"},
			TTL:            300 * time.Second,
			Resolvers:      []string{"8.8.8.8"},
			LogLevel:       "info",
			RateLimit:      100,
			RateBurst:      50,
			DockerTimeout:  5 * time.Second,
			ForwardTimeout: 2 * time.Second,
		}
	}

	tests := []struct {
		name    string
		mutate  func(*Config)
		wantErr bool
	}{
		{"valid baseline", func(c *Config) {}, false},
		{"multiple TLDs", func(c *Config) { c.TLDs = []string{"docker", "local"} }, false},
		{"invalid listen IP", func(c *Config) { c.ListenIP = "not-an-ip" }, true},
		{"no TLDs", func(c *Config) { c.TLDs = nil }, true},
		{"empty TLD in list", func(c *Config) { c.TLDs = []string{""} }, true},
		{"TLD with dot", func(c *Config) { c.TLDs = []string{"local.docker"} }, true},
		{"zero TTL", func(c *Config) { c.TTL = 0 }, true},
		{"no resolvers", func(c *Config) { c.Resolvers = nil }, true},
		{"invalid resolver IP", func(c *Config) { c.Resolvers = []string{"not-an-ip"} }, true},
		{"negative rate limit", func(c *Config) { c.RateLimit = -1 }, true},
		{"zero rate burst", func(c *Config) { c.RateBurst = 0 }, true},
		{"invalid log level", func(c *Config) { c.LogLevel = "verbose" }, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := base()
			tt.mutate(cfg)
			err := cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestLocalDomainSuffixes(t *testing.T) {
	cfg := &Config{TLDs: []string{"docker", "local"}}
	want := []string{".docker.", ".local."}
	got := cfg.LocalDomainSuffixes()
	if len(got) != len(want) {
		t.Fatalf("LocalDomainSuffixes() length = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("LocalDomainSuffixes()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestMatchLocalSuffix(t *testing.T) {
	cfg := &Config{TLDs: []string{"docker", "local"}}

	tests := []struct {
		domain string
		want   string
	}{
		{"myapp.docker.", ".docker."},
		{"myapp.local.", ".local."},
		{"example.com.", ""},
	}
	for _, tt := range tests {
		got := cfg.MatchLocalSuffix(tt.domain)
		if got != tt.want {
			t.Errorf("MatchLocalSuffix(%q) = %q, want %q", tt.domain, got, tt.want)
		}
	}
}
