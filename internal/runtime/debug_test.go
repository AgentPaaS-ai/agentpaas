package runtime

import (
	"testing"
)

func TestSanitizeDebugOutput_Empty(t *testing.T) {
	if got := SanitizeDebugOutput(""); got != "" {
		t.Errorf("SanitizeDebugOutput('') = %q, want ''", got)
	}
}

func TestSanitizeDebugOutput_CleanOutput(t *testing.T) {
	clean := "everything is fine, nothing sensitive here"
	got := SanitizeDebugOutput(clean)
	if got != clean {
		t.Errorf("SanitizeDebugOutput should not modify clean output: got %q", got)
	}
}

func TestSanitizeDebugOutput_APIKeyRedaction(t *testing.T) {
	input := "Using API key sk-proj-abc123def456 for authentication"
	got := SanitizeDebugOutput(input)
	if HasSecretLeak(got) {
		t.Errorf("API key pattern still present after sanitization: %q", got)
	}
}

func TestSanitizeDebugOutput_GitHubToken(t *testing.T) {
	input := "token=ghp_abc123def456ghi789"
	got := SanitizeDebugOutput(input)
	if HasSecretLeak(got) {
		t.Errorf("GitHub token still present after sanitization: %q", got)
	}
}

func TestSanitizeDebugOutput_JWT(t *testing.T) {
	input := `token is "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.abc123def456" in output`
	got := SanitizeDebugOutput(input)
	if HasSecretLeak(got) {
		t.Errorf("JWT still present after sanitization: %q", got)
	}
}

func TestSanitizeDebugOutput_AWSKey(t *testing.T) {
	input := "AKIAIOSFODNN7EXAMPLE"
	got := SanitizeDebugOutput(input)
	if HasSecretLeak(got) {
		t.Errorf("AWS key still present after sanitization: %q", got)
	}
}

func TestSanitizeDebugOutput_PEMPrivateKey(t *testing.T) {
	input := "-----BEGIN PRIVATE KEY-----\nMIIEvQIBADANBgkqhkiG9w0BAQEFAASCBKcwggSjAgEAAoIBAQ=="
	got := SanitizeDebugOutput(input)
	if HasSecretLeak(got) {
		t.Errorf("PEM private key marker still present after sanitization: %q", got)
	}
}

func TestSanitizeDebugOutput_HexString(t *testing.T) {
	input := "Container hash: abcdef0123456789abcdef0123456789abcdef01"
	got := SanitizeDebugOutput(input)
	// 32+ hex chars should be redacted
	if HasSecretLeak(got) {
		t.Errorf("Long hex string still present after sanitization: %q", got)
	}
}

func TestSanitizeDebugOutput_ShortHexUnchanged(t *testing.T) {
	short := "Container ID: a1b2c3d4e5f6"
	got := SanitizeDebugOutput(short)
	if got != short {
		t.Errorf("Short hex (under 32 chars) should not be redacted: got %q", got)
	}
}

func TestSanitizeDebugOutput_SentinelSecret(t *testing.T) {
	input := "sentinel-secret-value-abc123 should be redacted"
	got := SanitizeDebugOutput(input)
	if HasSecretLeak(got) {
		t.Errorf("Sentinel secret still present after sanitization: %q", got)
	}
}

func TestHasSecretLeak_EmptyString(t *testing.T) {
	if HasSecretLeak("") {
		t.Error("HasSecretLeak('') should return false")
	}
}

func TestHasSecretLeak_NoSecrets(t *testing.T) {
	if HasSecretLeak("just regular output without secrets") {
		t.Error("HasSecretLeak should return false for clean text")
	}
}

func TestHasSecretLeak_WithSecrets(t *testing.T) {
	if !HasSecretLeak("my secret is sk-test-key-abc123") {
		t.Error("HasSecretLeak should return true for text with API key")
	}
}

func TestSanitizeDockerInspect_RedactsEnvSecrets(t *testing.T) {
	input := `{
  "Config": {
    "Env": [
      "PATH=/usr/bin:/bin",
      "API_KEY=sk-test-abc123def456",
      "DB_PASSWORD=supersecret",
      "NONSECURE=hello"
    ],
    "Labels": {
      "maintainer": "test"
    }
  }
}`
	got := SanitizeDockerInspect(input)

	// Should redact API_KEY value
	if HasSecretLeak(got) {
		t.Errorf("Docker inspect still has secrets after sanitization: %q", got)
	}

	// Non-secure values should be unchanged
	if !contains(got, "NONSECURE=hello") {
		t.Errorf("Non-secret env var 'NONSECURE=hello' was incorrectly modified")
	}
	if !contains(got, "PATH=/usr/bin:/bin") {
		t.Errorf("Non-secret env var 'PATH' was incorrectly modified")
	}
}

func TestSanitizeDockerInspect_RedactsLabelsWithSecrets(t *testing.T) {
	input := `{
  "Config": {
    "Labels": {
      "agentpaas.managed-by": "agentpaas",
      "agentpaas.secret-key": "sk-test-abc123"
    }
  }
}`
	got := SanitizeDockerInspect(input)
	if HasSecretLeak(got) {
		t.Errorf("Docker inspect labels still have secrets after sanitization: %q", got)
	}
}

func TestSanitizeDockerInspect_CleanOutputUnchanged(t *testing.T) {
	input := `{"Config":{"Env":["PATH=/usr/bin"],"Labels":{"version":"1.0"}}}`
	got := SanitizeDockerInspect(input)
	if got != input {
		t.Errorf("Clean Docker inspect was modified: got %q", got)
	}
}

func TestSanitizeDockerInspect_RedactsJWTInLogs(t *testing.T) {
	input := `{"log":"eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.signature is the auth token"}`
	got := SanitizeDockerInspect(input)
	if HasSecretLeak(got) {
		t.Errorf("JWT in Docker inspect log still present after sanitization: %q", got)
	}
}

// contains returns true if the string contains the substring.
func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

// searchString is a simple substring search helper.
func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestSanitizeDockerInspect_RedactsFromConfigDumps(t *testing.T) {
	// Simulate network config with embedded secret
	input := `{
  "Name": "agentpaas-net-internal",
  "Labels": {
    "agentpaas.auth-token": "ghp_abc123def456ghi789"
  }
}`
	got := SanitizeDockerInspect(input)
	if HasSecretLeak(got) {
		t.Errorf("Config dump still has secrets after sanitization: %q", got)
	}
}

func TestSanitizeDebugOutput_BinaryNonCharData(t *testing.T) {
	input := string([]byte{0x00, 0x01, 0x02}) + "api key: sk-test-123"
	got := SanitizeDebugOutput(input)
	if HasSecretLeak(got) {
		t.Errorf("Secret in binary data still present after sanitization: %q", got)
	}
}