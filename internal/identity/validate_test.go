package identity

import (
	"strings"
	"testing"
)

func TestValidateAgentName(t *testing.T) {
	tests := []struct {
		name    string
		wantErr bool
	}{
		{name: "", wantErr: true},
		{name: "agent..name", wantErr: true},
		{name: "../agent", wantErr: true},
		{name: "agent/name", wantErr: true},
		{name: `agent\name`, wantErr: true},
		{name: "agent\x00name", wantErr: true},
		{name: strings.Repeat("a", 254), wantErr: true},
		{name: "agent-a", wantErr: false},
		{name: "worker_1", wantErr: false},
		{name: "gateway.east", wantErr: false},
		{name: strings.Repeat("a", 253), wantErr: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateAgentName(tt.name)
			if tt.wantErr && err == nil {
				t.Fatalf("ValidateAgentName(%q): expected error, got nil", tt.name)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("ValidateAgentName(%q): unexpected error: %v", tt.name, err)
			}
		})
	}
}
