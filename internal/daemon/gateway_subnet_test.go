package daemon

import "testing"

func TestGatewaySubnetFromIP(t *testing.T) {
	tests := []struct {
		ip   string
		want string
	}{
		{"172.18.0.2", "172.18.0.0/16"},
		{"172.20.0.2", "172.20.0.0/16"},
		{"10.0.0.5", "10.0.0.0/16"},
		{"192.168.1.100", "192.168.0.0/16"},
		{"", ""},
		{"invalid", ""},
		{"::1", ""},         // IPv6 returns empty
		{"2001:db8::1", ""}, // IPv6 returns empty
	}
	for _, tt := range tests {
		got := gatewaySubnetFromIP(tt.ip)
		if got != tt.want {
			t.Errorf("gatewaySubnetFromIP(%q) = %q, want %q", tt.ip, got, tt.want)
		}
	}
}
