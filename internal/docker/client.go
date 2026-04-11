// Package docker wraps the Docker API client, presenting a minimal interface
// focused on what the DNS resolver actually needs: container IP lookup.
package docker

import (
	"context"
	"fmt"
	"net"

	dockerclient "github.com/docker/docker/client"
	"github.com/docker/docker/api/types"
)

// Client is the interface the DNS server uses to query Docker.
// Keeping it narrow makes mocking trivial in tests.
type Client interface {
	// ContainerIPs returns all IPv4 addresses assigned to the named container
	// across all its networks. Returns an empty slice (not an error) if the
	// container does not exist.
	ContainerIPs(ctx context.Context, containerName string) ([]string, error)
	// Close releases underlying resources.
	Close() error
}

// RealClient wraps the official Docker SDK client.
type RealClient struct {
	cli *dockerclient.Client
}

// NewClient creates a RealClient. If host is empty the DOCKER_HOST environment
// variable (or the platform socket default) is used.
func NewClient(host string) (*RealClient, error) {
	opts := []dockerclient.Opt{
		dockerclient.WithAPIVersionNegotiation(),
	}
	if host != "" {
		opts = append(opts, dockerclient.WithHost(host))
	} else {
		opts = append(opts, dockerclient.FromEnv)
	}

	cli, err := dockerclient.NewClientWithOpts(opts...)
	if err != nil {
		return nil, fmt.Errorf("creating docker client: %w", err)
	}
	return &RealClient{cli: cli}, nil
}

// ContainerIPs implements Client. It uses the context deadline (set by the
// caller) to bound the Docker API call.
func (r *RealClient) ContainerIPs(ctx context.Context, containerName string) ([]string, error) {
	info, err := r.cli.ContainerInspect(ctx, containerName)
	if err != nil {
		if dockerclient.IsErrNotFound(err) {
			return nil, nil // not found is not an error; return empty
		}
		return nil, fmt.Errorf("inspecting container %q: %w", containerName, err)
	}
	return extractIPs(info), nil
}

// Close implements Client.
func (r *RealClient) Close() error {
	return r.cli.Close()
}

// extractIPs collects all non-empty IPv4 addresses from a container's network
// settings. IPv6 addresses are intentionally excluded because the DNS handler
// only populates A records today; AAAA support can be added separately.
func extractIPs(info types.ContainerJSON) []string {
	if info.NetworkSettings == nil || info.NetworkSettings.Networks == nil {
		return nil
	}
	var ips []string
	for _, network := range info.NetworkSettings.Networks {
		if network == nil || network.IPAddress == "" {
			continue
		}
		ip := net.ParseIP(network.IPAddress)
		if ip == nil || ip.To4() == nil {
			continue // skip IPv6 or unparseable entries
		}
		ips = append(ips, network.IPAddress)
	}
	return ips
}
