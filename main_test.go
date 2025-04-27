package main

import (
	"context"
	"fmt"
	"log"
	"flag"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/network"
	"github.com/miekg/dns"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require" // Using require for setup checks
	// Import client explicitly if IsErrNotFound is used directly (it's often better to use errors.Is with a specific error type if available)
	// dockerClient "github.com/docker/docker/client" // Example if needed, but we use the interface
)

// MockDockerClient for testing
type MockDockerClient struct {
	mock.Mock
}

// Implement the DockerClient interface
func (m *MockDockerClient) ContainerInspect(ctx context.Context, containerID string) (types.ContainerJSON, error) {
	// Log the call received by the mock
	log.Printf("MockDockerClient: ContainerInspect called with containerID='%s'", containerID)
	args := m.Called(ctx, containerID)
	// Log the result being returned
	// Check if the first argument is indeed types.ContainerJSON before logging detailed fields
	var resultJSON types.ContainerJSON
	if arg0, ok := args.Get(0).(types.ContainerJSON); ok {
		resultJSON = arg0
	}
	log.Printf("MockDockerClient: ContainerInspect returning: [JSON], %v", args.Error(1)) // Avoid logging potentially large JSON struct directly
	return resultJSON, args.Error(1)
}

func (m *MockDockerClient) Close() error {
	log.Println("MockDockerClient: Close called")
	args := m.Called()
	return args.Error(0)
}

// --- Test Main ---
func TestMain(m *testing.M) {
	// Set default logger for tests to see log output easily
	log.SetOutput(os.Stdout)
	log.SetFlags(log.Ltime | log.Lshortfile) // Add timestamp and file/line

	// Run all tests
	exitCode := m.Run()

	os.Exit(exitCode)
}

// --- Cache Tests ---
func TestCacheOperations(t *testing.T) {
	// Reset cache before tests
	dnsCache.Lock()
	dnsCache.m = make(map[string][]string)
	dnsCache.t = make(map[string]time.Time)
	dnsCache.Unlock()

	// Set a reasonable TTL for testing
	originalTTL := *ttl
	testTTL := 60
	*ttl = testTTL // Use a 60 second TTL for cache tests
	t.Cleanup(func() {
		*ttl = originalTTL // Restore original TTL
	})

	t.Run("TestCacheStoreAndRetrieve", func(t *testing.T) {
		testDomain := "test.container.docker."
		testIPs := []string{"172.17.0.2", "192.168.1.100"} // Test with multiple IPs

		cacheIPs(testDomain, testIPs)
		ips, found := getCachedIPs(testDomain)

		require.True(t, found, "Should find the cached entry")
		assert.Equal(t, testIPs, ips, "Cached IPs should match stored IPs")

		// Test retrieval again to ensure it persists
		ips2, found2 := getCachedIPs(testDomain)
		require.True(t, found2, "Should find the cached entry again")
		assert.Equal(t, testIPs, ips2, "Cached IPs should match on second retrieval")
	})

	t.Run("TestCacheExpiration", func(t *testing.T) {
		testDomain := "expired.container.docker."
		testIPs := []string{"172.17.0.3"}
		// Use the testTTL set for the parent test
		cacheDuration := time.Duration(-(testTTL + 1)) * time.Second // Ensure it's expired

		cacheIPs(testDomain, testIPs) // Cache it

		// Manually set the timestamp in the past to simulate expiration
		dnsCache.Lock()
		// Check if entry exists before trying to modify time
		if _, ok := dnsCache.m[testDomain]; ok {
			dnsCache.t[testDomain] = time.Now().Add(cacheDuration)
			log.Printf("Manually set cache time for %s to %s (expired)", testDomain, dnsCache.t[testDomain])
		} else {
			// This case should ideally not happen if cacheIPs worked
			t.Fatalf("Cache entry for %s not found after cacheIPs call, cannot test expiration", testDomain)
		}
		dnsCache.Unlock()

		ips, found := getCachedIPs(testDomain)
		assert.False(t, found, "Cache entry should be expired (found=false)")
		assert.Nil(t, ips, "IPs should be nil for expired entry")
	})

	t.Run("TestCacheEmptyResult", func(t *testing.T) {
		testDomain := "empty.container.docker."
		testIPs := []string{} // Empty slice

		// Current implementation doesn't cache empty results
		cacheIPs(testDomain, testIPs)
		_, found := getCachedIPs(testDomain)
		assert.False(t, found, "Empty result should not be cached")
	})
}

// --- DNS Handler Test ---
func TestDNSHandler(t *testing.T) {
	// Parse flags for this test to set *tld, *ip etc. to defaults or command-line overrides
	if !flag.Parsed() {
		// Set default values manually if not parsed, avoids global parsing side effects
		// Or call flag.Parse() if defaults are sufficient and side effects are managed.
		// Let's assume defaults are okay here.
		flag.Parse()
	}

	// Store original global docker client and restore it after the test
	originalGlobalClient := dockerClient
	t.Cleanup(func() {
		log.Println("Restoring original global docker client")
		dockerClient = originalGlobalClient
	})

	// Create a new mock client for this test
	mockClient := new(MockDockerClient)
	// Set the *global* dockerClient to our mock for handleDNSQuery to use
	dockerClient = mockClient

	// --- Subtest: Local Resolution Success ---
	t.Run("TestLocalResolutionSuccess", func(t *testing.T) {
		containerName := "valid-container"
		queryDomain := fmt.Sprintf("%s.%s.", containerName, *tld) // e.g., valid-container.docker.
		expectedIP := "172.17.0.2"

		// Setup mock expectation for *this specific subtest*
		mockClient.On("ContainerInspect", mock.Anything, containerName).
			Return(types.ContainerJSON{
				NetworkSettings: &types.NetworkSettings{
					Networks: map[string]*network.EndpointSettings{
						"bridge": {IPAddress: expectedIP},
					},
				},
				ContainerJSONBase: &types.ContainerJSONBase{
					ID:    "test-container-id",
					Name:  "/" + containerName,
					State: &types.ContainerState{Status: "running"},
				},
			}, nil).Once()

		// --- Start Test DNS Server ---
		serverAddr := "127.0.0.1:5553" // Use a specific port for testing
		server := &dns.Server{Addr: serverAddr, Net: "udp", Handler: dns.HandlerFunc(handleDNSQuery)} // Ensure handler is set
		ready := make(chan bool)                                                                      // Channel to signal server readiness

		server.NotifyStartedFunc = func() {
			log.Printf("Test DNS Server started on %s", serverAddr)
			close(ready) // Signal that the server is ready
		}

		// Run server in a goroutine
		go func() {
			log.Println("Starting Test DNS Server goroutine...")
			err := server.ListenAndServe()
			// Log error only if it's not the expected shutdown error (string check)
			// *** REMOVED check for dns.ErrServerClosed ***
			if err != nil && !strings.Contains(err.Error(), "use of closed network connection") {
				// Use t.Logf or t.Errorf carefully from goroutines
				// Consider signaling the error back to the main test goroutine via a channel if needed
				log.Printf("Test DNS Server ListenAndServe error: %v", err)
			}
			log.Println("Test DNS Server goroutine finished.")
		}()

		// Use t.Cleanup for reliable server shutdown
		t.Cleanup(func() {
			log.Println("Shutting down test DNS server...")
			if err := server.Shutdown(); err != nil {
				t.Logf("Error shutting down test DNS server: %v", err)
			}
			log.Println("Test DNS server shutdown complete.")
			// Verify mock expectations *after* server shutdown ensures all calls were processed
			// mockClient.AssertExpectations(t) // Moved verification after client exchange
		})

		// Wait for server to be ready or timeout
		select {
		case <-ready:
			log.Println("Test DNS Server ready signal received.")
		case <-time.After(3 * time.Second): // Increased timeout slightly
			t.Fatal("DNS server failed to start within timeout") // Fatal stops this subtest
		}
		// --- Server Started ---

		// --- Send DNS Query ---
		client := new(dns.Client)
		client.Timeout = 2 * time.Second // Client timeout
		msg := new(dns.Msg)
		msg.SetQuestion(queryDomain, dns.TypeA) // Query for A record
		msg.RecursionDesired = false           // We don't need recursion for local test

		log.Printf("Sending DNS query for %s (A) to %s", queryDomain, serverAddr)

		var r *dns.Msg
		var rtt time.Duration
		var err error

		// Retry mechanism
		maxRetries := 3
		for i := 0; i < maxRetries; i++ {
			r, rtt, err = client.Exchange(msg, serverAddr)
			// Check for success: no error, non-nil response, and NOERROR rcode
			if err == nil && r != nil && r.Rcode == dns.RcodeSuccess {
				log.Printf("DNS query successful on attempt %d (RTT: %v)", i+1, rtt)
				break // Success
			}
			// Log details on failure before retry
			var rcodeStr string = "N/A"
			if r != nil {
				rcodeStr = dns.RcodeToString[r.Rcode]
			}
			log.Printf("DNS query attempt %d failed: err=%v, rcode=%s. Retrying...",
				i+1, err, rcodeStr)
			time.Sleep(time.Duration(100+i*100) * time.Millisecond) // Simple backoff
		}
		// --- Query Sent ---

		// --- Assertions ---
		require.NoError(t, err, "DNS client exchange should succeed")
		require.NotNil(t, r, "DNS response should not be nil")

		log.Printf("Received DNS response: RCODE=%s, Answers=%d", dns.RcodeToString[r.Rcode], len(r.Answer))
		for i, ans := range r.Answer {
			log.Printf("Answer %d: %s", i, ans.String())
		}

		require.Equal(t, dns.RcodeSuccess, r.Rcode, "DNS response RCODE should be NOERROR")
		require.NotEmpty(t, r.Answer, "DNS response should contain at least one answer")
		assert.Len(t, r.Answer, 1, "Should be exactly one A record in the answer")

		if len(r.Answer) > 0 {
			aRecord, ok := r.Answer[0].(*dns.A)
			require.True(t, ok, "Answer record should be of type *dns.A")
			assert.Equal(t, queryDomain, aRecord.Hdr.Name, "Answer record name should match query")
			assert.Equal(t, dns.TypeA, aRecord.Hdr.Rrtype, "Answer record type should be A")
			assert.Equal(t, uint32(*ttl), aRecord.Hdr.Ttl, "Answer record TTL should match config")
			assert.Equal(t, expectedIP, aRecord.A.String(), "Returned IP address mismatch")
		}

		// Verify mock interactions after the operation
		mockClient.AssertExpectations(t)
	})

	// --- Subtest: Local Resolution Not Found ---
	t.Run("TestLocalResolutionNotFound", func(t *testing.T) {
		containerName := "not-a-real-container"
		queryDomain := fmt.Sprintf("%s.%s.", containerName, *tld)

		// Setup mock to return a "not found" error simulation
		// Note: The actual Docker client might return an error struct that client.IsErrNotFound can check.
		// Here we simulate the error message behavior seen in fetchIPsFromDocker's handling.
		// A more robust mock might return a specific error type if the interface allowed.
		mockClient.On("ContainerInspect", mock.Anything, containerName).
			// Return an empty JSON and an error that IsErrNotFound would catch (or our logic handles)
			// We need an error type that client.IsErrNotFound recognizes, or simulate its effect.
			// Let's return a generic error that our fetch function handles.
			// The fetchIPsFromDocker function now checks client.IsErrNotFound(err)
			// We need to return an error that makes this true, or adjust fetchIPsFromDocker.
			// Let's return a simple error and assume fetchIPsFromDocker handles it.
			// UPDATE: fetchIPsFromDocker returns []string{}, nil for NotFound. So mock should return that.
			// Return(types.ContainerJSON{}, fmt.Errorf("simulated not found error")).Once()
			// Correction based on fetchIPsFromDocker logic: It expects an error for which client.IsErrNotFound is true.
			// Let's simulate that by returning a specific error type if possible, or just the error message.
			// Mocking IsErrNotFound is tricky. Let's stick to returning an error and ensure fetchIPsFromDocker handles it.
			// Re-checking fetchIPsFromDocker: it now logs the error and returns empty list + nil error for NotFound.
			// So the mock should return an error that IsErrNotFound recognizes. How to mock that?
			// Easiest: Return the specific error string Docker API usually returns.
			Return(types.ContainerJSON{}, fmt.Errorf("Error response from daemon: No such container: %s", containerName)).Once()

		// --- Start Test DNS Server ---
		serverAddr := "127.0.0.1:5554" // Use a different port
		server := &dns.Server{Addr: serverAddr, Net: "udp", Handler: dns.HandlerFunc(handleDNSQuery)}
		ready := make(chan bool)
		server.NotifyStartedFunc = func() { close(ready) }
		go func() {
			err := server.ListenAndServe()
			// *** REMOVED check for dns.ErrServerClosed ***
			if err != nil && !strings.Contains(err.Error(), "use of closed network connection") {
				log.Printf("Server error (NotFound test): %v", err)
			}
		}()
		t.Cleanup(func() { _ = server.Shutdown() }) // Use t.Cleanup
		select {
		case <-ready:
		case <-time.After(2 * time.Second):
			t.Fatal("DNS server (NotFound test) failed to start")
		}
		// --- Server Started ---

		// --- Send DNS Query ---
		client := new(dns.Client)
		client.Timeout = 1 * time.Second
		msg := new(dns.Msg)
		msg.SetQuestion(queryDomain, dns.TypeA)
		r, _, err := client.Exchange(msg, serverAddr)
		// --- Query Sent ---

		// --- Assertions ---
		require.NoError(t, err, "DNS client exchange should succeed even if container not found")
		require.NotNil(t, r, "DNS response should not be nil")
		// fetchIPsFromDocker now returns empty slice and nil error for "not found"
		// handleDNSQuery should then result in a NOERROR response with an empty answer section.
		assert.Equal(t, dns.RcodeSuccess, r.Rcode, "RCODE should be NOERROR for handled 'not found'")
		assert.Empty(t, r.Answer, "Answer section should be empty for 'not found' container")

		mockClient.AssertExpectations(t)
	})

	// TODO: Add TestForwarding if needed
}

// --- Config Validation Tests ---
func TestConfigValidation(t *testing.T) {
	// Store original values and restore them after each subtest
	originalIP := *ip
	originalTLD := *tld
	originalResolvers := *resolvers
	t.Cleanup(func() {
		*ip = originalIP
		*tld = originalTLD
		*resolvers = originalResolvers
	})

	tests := []struct {
		name        string
		ip          string
		tld         string
		resolvers   string
		expectError bool
		errorMsg    string // Optional: check for specific error message part
	}{
		{"ValidConfig", "127.0.0.1", "docker", "8.8.8.8", false, ""},
		{"ValidConfigMultipleResolvers", "10.0.0.1", "internal", "1.1.1.1, 8.8.4.4", false, ""},
		{"InvalidIP", "invalid.ip", "docker", "8.8.8.8", true, "invalid IP address"},
		{"EmptyTLD", "127.0.0.1", "", "8.8.8.8", true, "TLD cannot be empty"},
		{"TLDWithDots", "192.168.1.1", "my.domain", "8.8.8.8", false, ""}, // Allowed, but logged as warning
		{"EmptyResolvers", "127.0.0.1", "docker", "", true, "at least one fallback resolver"},
		{"InvalidResolverIP", "127.0.0.1", "docker", "8.8.8.8,bad-ip", true, "invalid fallback resolver IP"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set flags for this specific test case
			*ip = tt.ip
			*tld = tt.tld
			*resolvers = tt.resolvers

			err := validateConfig()

			if tt.expectError {
				assert.Error(t, err, "Expected an error for config: %+v", tt)
				if tt.errorMsg != "" && err != nil {
					assert.Contains(t, err.Error(), tt.errorMsg, "Error message mismatch")
				}
			} else {
				assert.NoError(t, err, "Expected no error for config: %+v", tt)
			}
		})
	}
}

