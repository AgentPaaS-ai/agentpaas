package openshellrt

import (
	"net"
	"os"
	"strings"
	"testing"
)

func TestWriteGatewayConfigDarwinGRPCEndpoint(t *testing.T) {
	orig := gatewayGOOS
	t.Cleanup(func() { gatewayGOOS = orig })

	_, port, err := net.SplitHostPort(GatewayAddr)
	if err != nil {
		t.Fatalf("GatewayAddr %q: %v", GatewayAddr, err)
	}
	if port == "" {
		t.Fatalf("GatewayAddr %q has no port", GatewayAddr)
	}
	wantEndpoint := "http://host.docker.internal:" + port
	const wantBind = `bind_address = "127.0.0.1:17670"`

	for _, tc := range []struct {
		goos string
		want bool
	}{
		{goos: "darwin", want: true},
		{goos: "linux", want: false},
	} {
		t.Run(tc.goos, func(t *testing.T) {
			gatewayGOOS = tc.goos
			path, err := writeGatewayConfig(t.TempDir(), "/var/run/docker.sock")
			if err != nil {
				t.Fatal(err)
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			text := string(raw)
			if !strings.Contains(text, wantBind) {
				t.Fatalf("bind_address must stay %s:\n%s", wantBind, text)
			}
			if strings.Contains(text, `bind_address = "0.0.0.0:`) {
				t.Fatalf("bind_address must not leave loopback:\n%s", text)
			}
			hasEndpoint := strings.Contains(text, "grpc_endpoint") && strings.Contains(text, "host.docker.internal")
			if tc.want {
				if !hasEndpoint {
					t.Fatalf("darwin config missing grpc_endpoint host.docker.internal:\n%s", text)
				}
				if !strings.Contains(text, wantEndpoint) {
					t.Fatalf("darwin grpc_endpoint must use GatewayAddr port %q:\n%s", port, text)
				}
				dockerAt := strings.Index(text, "[openshell.drivers.docker]")
				endpointAt := strings.Index(text, "grpc_endpoint")
				if dockerAt < 0 || endpointAt < dockerAt {
					t.Fatalf("grpc_endpoint must sit in the docker driver table:\n%s", text)
				}
				return
			}
			if strings.Contains(text, "grpc_endpoint") || strings.Contains(text, "host.docker.internal") {
				t.Fatalf("non-darwin config must leave grpc_endpoint unset:\n%s", text)
			}
		})
	}
}
