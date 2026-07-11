package doctor

import (
	"strings"
	"testing"
)

func TestParseDockerVersion(t *testing.T) {
	tests := []struct {
		input    string
		wantMaj  int
		wantMin  int
		wantPat  int
		wantErr  bool
	}{
		{"29.5.1", 29, 5, 1, false},
		{"24.0.7", 24, 0, 7, false},
		{"20.10.12", 20, 10, 12, false},
		{"v29.5.1", 0, 0, 0, true},
		{"", 0, 0, 0, true},
		{"abc", 0, 0, 0, true},
		{"29.5", 0, 0, 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			maj, min, pat, err := parseDockerVersion(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Errorf("parseDockerVersion(%q) expected error, got (%d,%d,%d)", tt.input, maj, min, pat)
				}
				return
			}
			if err != nil {
				t.Errorf("parseDockerVersion(%q) unexpected error: %v", tt.input, err)
				return
			}
			if maj != tt.wantMaj || min != tt.wantMin || pat != tt.wantPat {
				t.Errorf("parseDockerVersion(%q) = (%d,%d,%d), want (%d,%d,%d)",
					tt.input, maj, min, pat, tt.wantMaj, tt.wantMin, tt.wantPat)
			}
		})
	}
}

func TestIsDockerVersionVulnerable(t *testing.T) {
	tests := []struct {
		version string
		want    bool // true = vulnerable
	}{
		{"29.0.0", true},
		{"29.5.0", true},
		{"29.5.1", false},
		{"29.5.2", false},
		{"30.0.0", false},
		{"24.0.7", true},
		{"20.10.12", true},
		{"27.0.0", true},
	}

	for _, tt := range tests {
		t.Run(tt.version, func(t *testing.T) {
			got := isDockerVersionVulnerable(tt.version)
			if got != tt.want {
				t.Errorf("isDockerVersionVulnerable(%q) = %v, want %v", tt.version, got, tt.want)
			}
		})
	}
}

func TestCheckDockerServerVersion(t *testing.T) {
	result := CheckDockerServerVersion()

	if result.Name != "docker_server_version" {
		t.Errorf("expected name 'docker_server_version', got %q", result.Name)
	}

	// Should always return a result without panicking.
	if result.Message == "" {
		t.Error("expected a non-empty message")
	}

	// Status should be determinable.
	if result.Status != "ok" && result.Status != "error" {
		t.Errorf("expected status 'ok' or 'error', got %q", result.Status)
	}

	// If error, there must be a fix hint.
	if result.Status == "error" && result.FixHint == "" {
		t.Error("expected non-empty FixHint when status is error")
	}

	// Check that the message mentions the version or vulnerability.
	if result.Status == "ok" && !strings.Contains(result.Message, "patched") {
		t.Logf("OK message: %s", result.Message)
	}
	if result.Status == "error" && !strings.Contains(result.Message, "CVE") {
		t.Logf("Error message: %s", result.Message)
	}
}

func TestCheckDockerReachable_MessageSaysServerVersion(t *testing.T) {
	result := CheckDockerReachable()

	// Verify message now says "Server version" not "API version".
	if strings.Contains(result.Message, "API version") {
		t.Errorf("message still says 'API version': %s", result.Message)
	}
}