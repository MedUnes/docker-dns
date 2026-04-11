package docker

import (
	"context"
	"testing"
)

// MockClient implements docker.Client for unit tests.
type MockClient struct {
	IPsFunc func(ctx context.Context, name string) ([]string, error)
}

func (m *MockClient) ContainerIPs(ctx context.Context, name string) ([]string, error) {
	return m.IPsFunc(ctx, name)
}

func (m *MockClient) Close() error { return nil }

// Ensure MockClient satisfies the interface at compile time.
var _ Client = (*MockClient)(nil)

func TestMockClientSatisfiesInterface(t *testing.T) {
	mock := &MockClient{
		IPsFunc: func(_ context.Context, name string) ([]string, error) {
			if name == "mycontainer" {
				return []string{"172.17.0.2"}, nil
			}
			return nil, nil
		},
	}

	ips, err := mock.ContainerIPs(context.Background(), "mycontainer")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ips) != 1 || ips[0] != "172.17.0.2" {
		t.Errorf("unexpected IPs: %v", ips)
	}

	// Not-found should return nil slice, nil error.
	ips, err = mock.ContainerIPs(context.Background(), "unknown")
	if err != nil {
		t.Fatalf("unexpected error for missing container: %v", err)
	}
	if ips != nil {
		t.Errorf("expected nil for missing container, got %v", ips)
	}
}
