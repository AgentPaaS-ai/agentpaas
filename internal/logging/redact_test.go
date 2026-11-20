package logging

import (
	"strings"
	"testing"
)

// sentinel values that must always be redacted
const (
	sentinelAPIKey       = "sk-test-api-key-1234567890abcdef"
	sentinelGitHubToken  = "ghp_abcdefghijklmnopqrstuvwxyz0123456789"
	sentinelJWT          = "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIiwibmFtZSI6IkpvaG4gRG9lIiwiaWF0IjoxNTE2MjM5MDIyfQ.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c"
	sentinelAWSKey       = "AKIAIOSFODNN7EXAMPLE"
	sentinelPrivateKey   = "-----BEGIN RSA PRIVATE KEY-----\nMIIEpAIBAAKCAQEA..."
	sentinelBase64High   = "dGhpcyBpcyBhIHZlcnkgbG9uZyBiYXNlNjQgc3RyaW5nIHRoYXQgc2hvdWxkIGJlIHJlZGFjdGVkIGJlY2F1c2UgaXQgaGFzIGhpZ2ggZW50cm9weQ=="
	sentinelHexLong      = "a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3f4a5b6c7d8e9f0a1b2c3d4"
	sentinelPasswordLike = "mySecretP@ssw0rd!"
)

// TestRedactAPIKey verifies that API key patterns (sk-, key-, token-) are redacted.
func TestRedactAPIKey(t *testing.T) {
	result := Redact(sentinelAPIKey)
	if strings.Contains(result, sentinelAPIKey) {
		t.Errorf("API key was not redacted: got %q", result)
	}
	if !strings.Contains(result, "[REDACTED]") {
		t.Errorf("expected [REDACTED] in result, got %q", result)
	}
}

// TestRedactGitHubToken verifies that GitHub tokens (ghp_, gho_, ghs_) are redacted.
func TestRedactGitHubToken(t *testing.T) {
	result := Redact(sentinelGitHubToken)
	if strings.Contains(result, "ghp_") {
		t.Errorf("GitHub token was not redacted: got %q", result)
	}
	if !strings.Contains(result, "[REDACTED]") {
		t.Errorf("expected [REDACTED] in result, got %q", result)
	}
}

// TestRedactJWT verifies that JWT tokens (eyJ...) are redacted.
func TestRedactJWT(t *testing.T) {
	result := Redact(sentinelJWT)
	if strings.Contains(result, "eyJhbGci") {
		t.Errorf("JWT token was not redacted: got %q", result)
	}
	if !strings.Contains(result, "[REDACTED]") {
		t.Errorf("expected [REDACTED] in result, got %q", result)
	}
}

// TestRedactAWSKey verifies that AWS keys (AKIA...) are redacted.
func TestRedactAWSKey(t *testing.T) {
	result := Redact(sentinelAWSKey)
	if strings.Contains(result, "AKIAIOSFODNN7EXAMPLE") {
		t.Errorf("AWS key was not redacted: got %q", result)
	}
	if !strings.Contains(result, "[REDACTED]") {
		t.Errorf("expected [REDACTED] in result, got %q", result)
	}
}

// TestRedactPrivateKey verifies that private key blocks are redacted.
func TestRedactPrivateKey(t *testing.T) {
	result := Redact(sentinelPrivateKey)
	if strings.Contains(result, "-----BEGIN") {
		t.Errorf("private key was not redacted: got %q", result)
	}
	if !strings.Contains(result, "[REDACTED]") {
		t.Errorf("expected [REDACTED] in result, got %q", result)
	}
}

// TestRedactHighEntropyBase64 verifies that high-entropy base64 strings (40+ chars) are redacted.
func TestRedactHighEntropyBase64(t *testing.T) {
	result := Redact(sentinelBase64High)
	if strings.Contains(result, sentinelBase64High) {
		t.Errorf("high-entropy base64 was not redacted: got %q", result)
	}
	if !strings.Contains(result, "[REDACTED]") {
		t.Errorf("expected [REDACTED] in result, got %q", result)
	}
}

// TestRedactHexString verifies that hex strings (32+ chars) are redacted.
func TestRedactHexString(t *testing.T) {
	result := Redact(sentinelHexLong)
	if strings.Contains(result, sentinelHexLong[0:32]) {
		t.Errorf("hex string was not redacted: got %q", result)
	}
	if !strings.Contains(result, "[REDACTED]") {
		t.Errorf("expected [REDACTED] in result, got %q", result)
	}
}

// TestRedactPasswordLike verifies that password-like values are redacted.
func TestRedactPasswordLike(t *testing.T) {
	result := Redact(sentinelPasswordLike)
	if strings.Contains(result, sentinelPasswordLike) {
		t.Errorf("password-like value was not redacted: got %q", result)
	}
	if !strings.Contains(result, "[REDACTED]") {
		t.Errorf("expected [REDACTED] in result, got %q", result)
	}
}

// TestRedactNormalString verifies that normal strings (paths, names, messages) are NOT redacted.
func TestRedactNormalString(t *testing.T) {
	normal := "/var/log/app.log"
	result := Redact(normal)
	if !strings.Contains(result, normal) {
		t.Errorf("normal string was incorrectly redacted: got %q", result)
	}
	if strings.Contains(result, "[REDACTED]") {
		t.Errorf("normal string should not contain [REDACTED]: got %q", result)
	}
}

// TestRedactNormalName verifies that a normal name string is not redacted.
func TestRedactNormalName(t *testing.T) {
	name := "Alice"
	result := Redact(name)
	if result != name {
		t.Errorf("normal name should not be redacted: got %q, want %q", result, name)
	}
}

// TestRedactMultipleValues verifies that multiple redacted values in one string are all redacted.
func TestRedactMultipleValues(t *testing.T) {
	input := "api key: " + sentinelAPIKey + " and token: " + sentinelGitHubToken
	result := Redact(input)
	if strings.Contains(result, sentinelAPIKey) {
		t.Errorf("first API key was not redacted")
	}
	if strings.Contains(result, "ghp_") {
		t.Errorf("GitHub token was not redacted")
	}
	// Count occurrences of [REDACTED]
	count := strings.Count(result, "[REDACTED]")
	if count < 2 {
		t.Errorf("expected at least 2 [REDACTED] markers, got %d in %q", count, result)
	}
}

// TestRedactAllSentinels ensures none of the sentinel values appear in output.
func TestRedactAllSentinels(t *testing.T) {
	sentinels := []string{
		sentinelAPIKey,
		sentinelGitHubToken,
		sentinelJWT,
		sentinelAWSKey,
		sentinelPrivateKey,
		sentinelBase64High,
		sentinelHexLong,
	}

	for _, s := range sentinels {
		result := Redact(s)
		if strings.Contains(result, s) {
			t.Errorf("sentinel value was not redacted: got %q", result)
		}
	}
}