//go:build integration
// +build integration

package main

import (
	"context"
	"flag" // Import flag package
	"fmt"  // Import fmt package
	"log"
	"strings" // Import strings package
	"testing"
	"time"

	// Import the main Docker types package
	// Import the container types explicitly
	"github.com/docker/docker/api/types/container"
	// Import image specific types as ImagePullOptions might be here
	"github.com/docker/docker/api/types/image" // Added import
	"github.com/docker/docker/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMain removed from here to avoid conflict with main_test.go

func TestDockerIntegration(t *testing.T) {
	// Parse flags for this test to initialize *tld, *ip etc.
	// Ensure this doesn't conflict if TestMain also parses flags.
	if !flag.Parsed() {
		flag.Parse() // Initialize flags like *tld
	}

	// Create a real Docker client
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	require.NoError(t, err, "Failed to create Docker client")
	// Wrap the real client in our interface implementation for fetchIPsFromDocker
	dockerAPIClient := &RealDockerClient{Client: cli}
	// Defer closing the client connection
	defer func() {
		if err := dockerAPIClient.Close(); err != nil {
			t.Logf("Error closing docker client: %v", err)
		}
	}()

	// Define container details
	containerName := "test-nginx-integration"
	imageName := "nginx:alpine"

	// Ensure the image exists locally
	log.Printf("Pulling image %s if not present...", imageName)
	ctx := context.Background() // Define ctx once for reuse
	_, _, err = cli.ImageInspectWithRaw(ctx, imageName)
	if err != nil {
		// Use client.IsErrNotFound for checking image existence
		if client.IsErrNotFound(err) {
			log.Printf("Image %s not found locally, pulling...", imageName)

			// Changed: Try using image.PullOptions from the specific sub-package
			reader, errPull := cli.ImagePull(ctx, imageName, image.PullOptions{}) // Using image.PullOptions

			require.NoError(t, errPull, "Failed to pull image %s", imageName)
			// It's better to read the stream to confirm pull completion,
			// but for simplicity in this test, a sleep might suffice.
			// Consider io.Copy(io.Discard, reader) for a more robust wait.
			defer reader.Close()
			// io.Copy(io.Discard, reader) // Uncomment for robust pull wait
			time.Sleep(15 * time.Second) // Simple wait, adjust as needed or use io.Copy
			log.Printf("Image %s pulled successfully.", imageName)
		} else {
			// Fail on other image inspection errors
			require.NoError(t, err, "Failed to inspect image %s", imageName)
		}
	} else {
		log.Printf("Image %s found locally.", imageName)
	}


	// --- Setup test container ---
	log.Printf("Creating container '%s' from image '%s'...", containerName, imageName)
	// Use the imported container types
	resp, err := cli.ContainerCreate(ctx,
		&container.Config{ // Use container.Config
			Image: imageName,
		}, nil, nil, nil, containerName) // Pass container name here
	// Handle potential conflicts if container already exists from a previous failed run
	if err != nil {
		// Use strings.Contains for error checking
		if strings.Contains(err.Error(), "already in use") {
			log.Printf("Container '%s' already exists, attempting to remove...", containerName)
			// Attempt removal (forceful)
			// Use container.RemoveOptions
			removeErr := cli.ContainerRemove(ctx, containerName, container.RemoveOptions{Force: true})
			require.NoError(t, removeErr, "Failed to remove existing conflicting container '%s'", containerName)
			// Retry creation
			resp, err = cli.ContainerCreate(ctx, &container.Config{Image: imageName}, nil, nil, nil, containerName)
			require.NoError(t, err, "Failed to create container '%s' after removing conflict", containerName)
		} else {
			// Fail on other creation errors
			require.NoError(t, err, "Failed to create container '%s'", containerName)
		}
	}
	log.Printf("Container '%s' created with ID: %s", containerName, resp.ID)

	// Ensure container is removed even if test fails
	t.Cleanup(func() {
		log.Printf("Cleaning up container '%s' (ID: %s)...", containerName, resp.ID)
		// Use container.RemoveOptions
		// Use a fresh context for cleanup
		cleanupCtx := context.Background()
		removeErr := cli.ContainerRemove(cleanupCtx, resp.ID, container.RemoveOptions{Force: true})
		if removeErr != nil {
			// Log cleanup errors but don't fail the test for cleanup issues
			t.Logf("Error removing container %s during cleanup: %v", resp.ID, removeErr)
		} else {
			log.Printf("Container '%s' removed successfully.", containerName)
		}
	})
	// --- Container Setup Done ---

	// --- Start container ---
	log.Printf("Starting container '%s'...", containerName)
	// Use container.StartOptions
	err = cli.ContainerStart(ctx, resp.ID, container.StartOptions{})
	require.NoError(t, err, "Failed to start container '%s'", containerName)
	log.Printf("Container '%s' started successfully.", containerName)

	// Brief pause to allow container networking to settle (optional but can help)
	time.Sleep(2 * time.Second)

	// --- Test DNS resolution ---
	t.Run("ResolveContainerIP", func(t *testing.T) {
		// Construct FQDN using the *tld flag and fmt.Sprintf
		queryDomain := fmt.Sprintf("%s.%s.", containerName, *tld)
		log.Printf("Attempting to resolve IP for domain: %s", queryDomain)

		var ips []string
		var fetchErr error

		// Retry mechanism for fetching IPs, as container inspection might take a moment
		maxRetries := 5
		retryDelay := 1 * time.Second
		for i := 0; i < maxRetries; i++ {
			// Correct call to fetchIPsFromDocker: pass domain string and client interface
			// The context is handled internally by fetchIPsFromDocker now.
			ips, fetchErr = fetchIPsFromDocker(queryDomain, dockerAPIClient) // Corrected arguments
			if fetchErr == nil && len(ips) > 0 {
				log.Printf("Successfully resolved IPs on attempt %d: %v", i+1, ips)
				break // Success
			}
			log.Printf("Attempt %d to resolve IPs failed: err=%v, ips=%v. Retrying in %v...", i+1, fetchErr, ips, retryDelay)
			time.Sleep(retryDelay)
		}

		// Assert after retries
		require.NoError(t, fetchErr, "fetchIPsFromDocker failed after retries")
		require.NotEmpty(t, ips, "Resolved IP list should not be empty")

		// Optionally, inspect the container directly to verify the IP matches
		inspectCtx := context.Background() // Use a fresh context
		inspectData, inspectErr := cli.ContainerInspect(inspectCtx, resp.ID)
		require.NoError(t, inspectErr, "Failed to inspect running container for verification")
		foundMatch := false
		if inspectData.NetworkSettings != nil && inspectData.NetworkSettings.Networks != nil {
			for netName, netw := range inspectData.NetworkSettings.Networks { // Iterate through networks
				if netw != nil && netw.IPAddress != "" {                     // Check IP is valid
					log.Printf("Container inspect shows IP %s for network %s", netw.IPAddress, netName)
					for _, resolvedIP := range ips {
						if netw.IPAddress == resolvedIP {
							log.Printf("Verified resolved IP %s matches container network IP on network %s", resolvedIP, netName)
							foundMatch = true
							break // Found a match for this resolved IP
						}
					}
				}
				if foundMatch {
					break // Found a match in one of the networks
				}
			}
		} else {
			t.Log("Container inspect data did not contain NetworkSettings or Networks map.")
		}
		assert.True(t, foundMatch, "Resolved IP(s) %v should contain an IP listed in container inspect's networks", ips)
		log.Printf("IP resolution test successful for %s.", queryDomain)
	})
}
