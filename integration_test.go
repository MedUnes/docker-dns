//go:build integration
// +build integration

package main

import (
	"context"
	"testing"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/client"
	"github.com/stretchr/testify/assert"
)

func TestDockerIntegration(t *testing.T) {
	cli, err := client.NewClientWithOpts(client.FromEnv)
	assert.NoError(t, err)

	// Setup test container
	ctx := context.Background()
	resp, err := cli.ContainerCreate(ctx,
		&types.ContainerConfig{
			Image: "nginx:alpine",
		}, nil, nil, nil, "test-nginx")
	assert.NoError(t, err)

	defer cli.ContainerRemove(ctx, resp.ID, types.ContainerRemoveOptions{Force: true})

	// Start container
	err = cli.ContainerStart(ctx, resp.ID, types.ContainerStartOptions{})
	assert.NoError(t, err)

	// Test DNS resolution
	t.Run("ResolveContainerIP", func(t *testing.T) {
		ips, err := fetchIPsFromDocker("test-nginx.docker.")
		assert.NoError(t, err)
		assert.NotEmpty(t, ips)
	})
}
