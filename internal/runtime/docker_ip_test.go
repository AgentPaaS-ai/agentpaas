package runtime

import (
	"context"
	"errors"
	"testing"
)

func TestInspectContainerIP_InvalidID(t *testing.T) {
	d := &DockerRuntime{}
	_, err := d.InspectContainerIP(context.Background(), "", "network-internal")
	if !errors.Is(err, ErrContainerNotFound) {
		t.Errorf("InspectContainerIP(empty) error = %v, want ErrContainerNotFound", err)
	}
}

func TestInspectContainerIP_DelegatesToDriver(t *testing.T) {
	mock := &mockRuntimeDriver{
		inspectContainerIPFunc: func(_ context.Context, id ContainerID, networkID string) (string, error) {
			if id != "gateway-1" {
				t.Errorf("id = %q, want gateway-1", id)
			}
			if networkID != "net-internal" {
				t.Errorf("networkID = %q, want net-internal", networkID)
			}
			return "10.0.0.2", nil
		},
	}
	d := NewDockerRuntimeWithDriver(mock)
	ip, err := d.InspectContainerIP(context.Background(), "gateway-1", "net-internal")
	if err != nil {
		t.Fatalf("InspectContainerIP() error = %v", err)
	}
	if ip != "10.0.0.2" {
		t.Errorf("InspectContainerIP() = %q, want 10.0.0.2", ip)
	}
}

func TestInspectContainerIP(t *testing.T) {
	networks := []ContainerNetworkInfo{
		{ID: "network-internal", Name: "agentpaas-net-internal-run-1", IPAddress: "172.18.0.5"},
		{ID: "network-egress", Name: "agentpaas-net-egress-run-1", IPAddress: "172.19.0.3"},
	}
	mock := &mockRuntimeDriver{
		inspectContainerNetFunc: func(_ context.Context, id ContainerID) ([]ContainerNetworkInfo, error) {
			if id != "gateway-1" {
				t.Fatalf("id = %q, want gateway-1", id)
			}
			return networks, nil
		},
	}
	d := NewDockerRuntimeWithDriver(mock)

	tests := []struct {
		name      string
		networkID string
		wantIP    string
	}{
		{name: "match by network ID", networkID: "network-internal", wantIP: "172.18.0.5"},
		{name: "match by network name", networkID: "agentpaas-net-egress-run-1", wantIP: "172.19.0.3"},
		{name: "no match", networkID: "unknown-net", wantIP: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ip, err := d.InspectContainerIP(context.Background(), "gateway-1", tt.networkID)
			if err != nil {
				t.Fatalf("InspectContainerIP() error = %v", err)
			}
			if ip != tt.wantIP {
				t.Errorf("InspectContainerIP() = %q, want %q", ip, tt.wantIP)
			}
		})
	}
}