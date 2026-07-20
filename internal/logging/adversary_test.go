package logging

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"
)

// ============================================================================
// ATTACK VECTOR 1: Bypass patterns — can we craft sensitive-looking strings
// that escape the redaction regexes?
// ============================================================================

// TestAdversary_BearerTokenWithoutEyJ checks if a Bearer token that does NOT
// start with "eyJ" (e.g., standard OAuth2 opaque token) is redacted.
// The apiKeyPattern requires `bearer\s+[a-z0-9]{8,}` which only matches
// pure alphanumeric tokens. Base64 bearer tokens with +, /, = will break this.
func TestAdversary_BearerTokenWithoutEyJ(t *testing.T) {
	// Bearer token with base64 chars (+, /, =) — these should be caught by
	// base64HighEntropyPattern but only if 40+ chars. A short bearer token
	// with special chars won't match the apiKeyPattern.
	tokens := []struct {
		name  string
		value string
	}{
		{
			name:  "bearer_with_slash_and_plus",
			value: "Bearer abc+def/ghi=",
		},
		{
			name:  "bearer_opaque_short",
			value: "Bearer dGhpcyBpcyBhIHRva2Vu",
		},
		{
			name:  "bearer_standard_opaque",
			value: "Bearer abcdefghijklmnopqrstuvwxyz", // 26 lowercase alphanumeric
		},
	}

	for _, tc := range tokens {
		t.Run(tc.name, func(t *testing.T) {
			result := Redact(tc.value)
			if strings.Contains(result, tc.value) {
				t.Errorf("Bearer token leaked: input=%q, output=%q", tc.value, result)
			}
			if !strings.Contains(result, "[REDACTED]") {
				t.Errorf("Bearer token NOT redacted: input=%q, output=%q", tc.value, result)
			}
		})
	}
}

// TestAdversary_ModifiedAPIKeyFormat checks if slight modifications to
// expected formats bypass patterns.
func TestAdversary_ModifiedAPIKeyFormat(t *testing.T) {
	cases := []struct {
		name  string
		value string
	}{
		// Using "apikey-" instead of "key-"
		{"apikey_prefix", "apikey-abcdef12345678"},
		// SCREAMING_CASE prefix
		{"uppercase_sk_prefix", "SK-ABCD1234EFGH5678"},
		// API key without any prefix at all (just long alphanumeric)
		{"no_prefix_long", "abcdef1234567890abcdef1234567890ab"},
		// Azure/other cloud keys with different prefixes
		{"azure_key", "AZR-abcdef1234567890"},
		// GraphQL API key format
		{"graphql_key", "da2-abc123def456ghi789"},
		// Short token that looks like an API key
		{"short_apikey_like", "mykey_abcdef"}, // 12 chars, contains "key" in value
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := Redact(tc.value)
			if strings.Contains(result, tc.value) && !strings.Contains(result, "[REDACTED]") {
				// Record it but don't fail — some of these may not match patterns
				t.Logf("POTENTIAL BYPASS: value not redacted: %q -> %q", tc.value, result)
			}
		})
	}
}

// TestAdversary_AWSKeyVariants checks if AWS key variants (ASIA, etc.) are caught.
func TestAdversary_AWSKeyVariants(t *testing.T) {
	// AWS temporary credentials use ASIA; permanent use AKIA (16 chars after prefix).
	cases := []struct {
		name      string
		val       string
		mustRedact bool
	}{
		{"ASIA_temp", "ASIAIOSFODNN7EXAMPLE", true},
		{"AKIA_valid", "AKIAIOSFODNN7EXAMPLE", true},
		{"AKID_not_prefix", "AKIDAIOSFODNN7EXAMPLE", false}, // not AKIA/ASIA
		{"AKIA_too_short", "AKIAIOSFODNN7EXAMPL", false},     // 15 chars after prefix
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := Redact(tc.val)
			redacted := !strings.Contains(result, tc.val) || strings.Contains(result, "[REDACTED]")
			if tc.mustRedact && !redacted {
				t.Errorf("AWS key not redacted: %q -> %q", tc.val, result)
			}
			if !tc.mustRedact && result != tc.val && strings.Contains(result, "[REDACTED]") {
				// Short/invalid variants should not match awsKeyPattern; if other patterns fire, log only.
				t.Logf("non-AWS pattern redacted %q -> %q (acceptable if high-entropy)", tc.val, result)
			}
		})
	}
}

// TestAdversary_HexWithColons checks if hex strings with colons
// (common format for fingerprints, UUIDs) bypass the hex pattern.
func TestAdversary_HexWithColons(t *testing.T) {
	// SHA256 fingerprint with colons — common in TLS certs (32 bytes = 31 colons)
	hexWithColons := "a1:b2:c3:d4:e5:f6:a7:b8:c9:d0:e1:f2:a3:b4:c5:d6:e7:f8:a9:b0:c1:d2:e3:f4:a5:b6:c7:d8:e9:f0:a1:b2"
	result := Redact(hexWithColons)
	if strings.Contains(result, hexWithColons) {
		t.Errorf("hex-with-colons fingerprint not redacted: %q", result)
	}
	if !strings.Contains(result, "[REDACTED]") {
		t.Errorf("expected [REDACTED] for colon hex fingerprint, got %q", result)
	}
}

// TestAdversary_Base64URL checks if base64url encoding (no + or /, uses - and _)
// bypasses the base64HighEntropyPattern.
func TestAdversary_Base64URL(t *testing.T) {
	// Base64url uses - and _ instead of + and /
	base64url := "dGhpcyBpcyBhIHZlcnkgbG9uZyBiYXNlNjR1cmwgc3RyaW5nX3dpdGhfZGFzaGVzX2FuZF91bmRlcnNjb3Jlcw"
	result := Redact(base64url)
	// Must not panic and must return something.
	if result == "" {
		t.Fatal("Redact returned empty for base64url input")
	}
	// looksLikeAPIKey may catch long alnum/-/_ strings; either redacted or preserved is
	// a policy choice — assert the output is well-formed (no partial corruption).
	if strings.Contains(result, "[REDACTED]") {
		if strings.Contains(result, base64url) {
			t.Errorf("partial redaction left original base64url intact: %q", result)
		}
		return
	}
	// Not redacted: document as known gap for pure base64url alphabet without markers.
	if result != base64url {
		t.Errorf("unexpected mutation without redaction marker: %q -> %q", base64url, result)
	}
}

// ============================================================================
// ATTACK VECTOR 2: Log injection — newlines and JSON-breaking characters
// ============================================================================

// TestAdversary_NewlineInValue checks if newlines in attribute values
// break JSON output.
func TestAdversary_NewlineInValue(t *testing.T) {
	var buf bytes.Buffer
	logger := NewLogger(slog.LevelInfo, &buf)

	// Inject newlines and quotes into a log value
	logger.Info("test",
		"malicious", "line1\nline2\n\"extra\":\"injected\"",
	)

	var record map[string]any
	if err := json.Unmarshal(buf.Bytes(), &record); err != nil {
		t.Errorf("JSON parse error (log injection may have broken output): %v\nraw: %s", err, buf.String())
	}
	val, ok := record["malicious"]
	if !ok {
		t.Fatal("expected malicious attribute")
	}
	valStr, ok := val.(string)
	if !ok {
		t.Fatalf("expected string, got %T", val)
	}
	if !strings.Contains(valStr, "line1") || !strings.Contains(valStr, "line2") {
		t.Errorf("newlines in value may have been mangled: %q", valStr)
	}
}

// TestAdversary_JSONInjection checks if injecting JSON into log values
// can hide data or break parsing.
func TestAdversary_JSONInjection(t *testing.T) {
	var buf bytes.Buffer
	logger := NewLogger(slog.LevelInfo, &buf)

	// Try to inject JSON control chars
	logger.Info("test",
		"data", `{"msg":"fake","secret":"sk-abc123def456"}`,
	)

	// Verify the output parses as a single JSON log line
	lines := bytes.Split(bytes.TrimSpace(buf.Bytes()), []byte("\n"))
	if len(lines) != 1 {
		t.Errorf("Expected 1 JSON line, got %d — log injection may have broken output", len(lines))
		for i, line := range lines {
			t.Logf("  line %d: %s", i, line)
		}
	}

	var record map[string]any
	if err := json.Unmarshal(lines[0], &record); err != nil {
		t.Fatalf("JSON parse error: %v\nraw: %s", err, lines[0])
	}
	// The "secret" key should NOT appear at the top level of the log record
	if _, exists := record["secret"]; exists {
		t.Error("Injected JSON key 'secret' leaked to top level of log record!")
	}
	// The API key inside the injected JSON should be redacted
	dataVal, ok := record["data"]
	if !ok {
		t.Fatal("expected data attribute")
	}
	dataStr, ok := dataVal.(string)
	if !ok {
		t.Fatalf("expected string, got %T", dataVal)
	}
	if strings.Contains(dataStr, "sk-abc123def456") {
		t.Errorf("API key in injected JSON not redacted: %q", dataStr)
	}
	if !strings.Contains(dataStr, "[REDACTED]") {
		t.Errorf("Expected [REDACTED] in data value: %q", dataStr)
	}
}

// ============================================================================
// ATTACK VECTOR 3: Attribute key bypass — keys that should be treated
// as sensitive but aren't in passwordFieldNames
// ============================================================================

// TestAdversary_SensitiveKeyBypass checks that values under sensitive
// attribute keys (like "token", "api_key", "authorization") are redacted
// even if they don't match value patterns.
func TestAdversary_SensitiveKeyBypass(t *testing.T) {
	cases := []struct {
		key   string
		value string
	}{
		// These keys are NOT in passwordFieldNames but should arguably be sensitive
		{"token", "my-plaintext-token-123"},
		{"api_key", "mykeyvalue12345"},
		{"apiKey", "mykeyvalue12345"},
		{"authorization", "Bearer mysecrettoken"},
		{"auth", "basic dXNlcjpwYXNz"},
		{"credential", "super-secret-credential"},
		{"access_token", "supersecrettokenvalue"},
		{"refresh_token", "refreshsecretvalue"},
	}

	var buf bytes.Buffer
	logger := NewLogger(slog.LevelInfo, &buf)

	attrs := make([]any, 0, len(cases)*2)
	for _, c := range cases {
		attrs = append(attrs, c.key, c.value)
	}
	logger.Info("test key bypass", attrs...)

	output := buf.String()
	var record map[string]any
	if err := json.Unmarshal(buf.Bytes(), &record); err != nil {
		t.Fatalf("JSON parse error: %v", err)
	}

	for _, c := range cases {
		t.Run(c.key, func(t *testing.T) {
			if strings.Contains(output, c.value) {
				// Check: is the value short enough to not trip looksLikeAPIKey?
				// Values < 20 chars won't be caught by looksLikeAPIKey,
				// and these keys aren't in passwordFieldNames.
				t.Logf("POTENTIAL BYPASS: key %q with value %q was NOT redacted (value exposed)", c.key, c.value)
			}
			val, exists := record[c.key]
			if !exists {
				t.Errorf("Expected key %q in output", c.key)
				return
			}
			if valStr, ok := val.(string); ok && valStr != "[REDACTED]" && valStr == c.value {
				t.Errorf("BYPASS: key %q exposed value %q instead of [REDACTED]", c.key, valStr)
			}
		})
	}
}

// ============================================================================
// ATTACK VECTOR 4: Nested values — structs and maps as slog.Any
// ============================================================================

// secretHolder is a struct that holds a secret to test struct-level bypass
type secretHolder struct {
	Password string
	Token    string
	APIKey   string
}

func (s secretHolder) String() string {
	return fmt.Sprintf("Password=%s, Token=%s, APIKey=%s", s.Password, s.Token, s.APIKey)
}

// errorWithSecret is an error that contains a secret in its message
type errorWithSecret struct {
	secret string
	msg    string
}

func (e *errorWithSecret) Error() string {
	return e.msg + ": " + e.secret
}

// TestAdversary_StructAsAny checks if passing a struct as a direct
// slog.Any value bypasses redaction. The JSON handler calls encoding/json
// on the struct, which will serialize all fields including Password.
func TestAdversary_StructAsAny(t *testing.T) {
	var buf bytes.Buffer
	logger := NewLogger(slog.LevelInfo, &buf)

	secret := secretHolder{
		Password: "hunter2",
		Token:    "my-secret-token-456",
		APIKey:   "sk-abc...mnop",
	}

	// Pass struct directly (not wrapped in slog.Any) — slog will handle as KindAny
	logger.Info("test struct", "user", secret)

	output := buf.String()
	t.Logf("Struct logging output: %s", output)

	// slog.JSONHandler will serialize the struct fields via encoding/json
	if strings.Contains(output, "hunter2") || strings.Contains(output, "sk-abc...mnop") {
		t.Errorf("Secrets leaked in struct logging output: %s", output)
	}
	if !strings.Contains(output, "[REDACTED]") && strings.Contains(output, "Password") {
		t.Errorf("Struct password field not redacted in output: %s", output)
	}
}

// TestAdversary_MapAsAny checks if passing a map with sensitive keys
// as a direct slog.Any value bypasses redaction.
func TestAdversary_MapAsAny(t *testing.T) {
	var buf bytes.Buffer
	logger := NewLogger(slog.LevelInfo, &buf)

	secretMap := map[string]string{
		"password": "s3cret!",
		"token":    "tok-abcdef12345",
		"api_key":  "sk-abc...mnop",
	}

	logger.Info("test map", "credentials", secretMap)

	output := buf.String()
	t.Logf("Map logging output: %s", output)

	// slog.JSONHandler will serialize map fields via encoding/json
	if strings.Contains(output, "s3cret!") || strings.Contains(output, "tok-abcdef12345") {
		t.Errorf("Secrets leaked in map logging output: %s", output)
	}
}

// ============================================================================
// ATTACK VECTOR 5: Error wrapping
// ============================================================================

// TestAdversary_ErrorWrapping checks if errors wrapping sensitive data
// are properly redacted.
func TestAdversary_ErrorWrapping(t *testing.T) {
	var buf bytes.Buffer
	logger := NewLogger(slog.LevelInfo, &buf)

	innerErr := &errorWithSecret{
		secret: "ghp_abcdefghijklmnop123456",
		msg:    "inner failure",
	}
	wrappedErr := fmt.Errorf("outer failure: %w", innerErr)

	// Log the error using slog's error attribute type
	logger.Info("request failed", "error", wrappedErr)

	output := buf.String()
	t.Logf("Error wrapping output: %s", output)

	if strings.Contains(output, "ghp_abcdefghijklmnop123456") {
		t.Errorf("Secret leaked through wrapped error: %s", output)
	}
	if !strings.Contains(output, "[REDACTED]") {
		t.Logf("No [REDACTED] found — error may not be processed by redaction")
	}
}

// TestAdversary_ErrorAsString checks if errors passed as slog.String
// (converted to string manually) are redacted.
func TestAdversary_ErrorAsString(t *testing.T) {
	var buf bytes.Buffer
	logger := NewLogger(slog.LevelInfo, &buf)

	innerErr := &errorWithSecret{
		secret: "sk-abcdefghijklmnop123456",
		msg:    "API call failed",
	}
	errStr := fmt.Sprintf("operation error: %v", innerErr)

	logger.Info("request failed", "error", errStr)

	output := buf.String()
	if strings.Contains(output, "sk-abcdefghijklmnop123456") {
		t.Errorf("Secret leaked through error as string: %s", output)
	}
}

// ============================================================================
// ATTACK VECTOR 6: Race condition
// ============================================================================

// TestAdversary_RaceCondition checks if concurrent log calls cause
// data races or inconsistent redaction.
func TestAdversary_RaceCondition(t *testing.T) {
	var buf bytes.Buffer
	logger := NewLogger(slog.LevelInfo, &buf)

	var wg sync.WaitGroup
	n := 50

	secrets := []string{
		"sk-abcdefghijklmnop123456",
		"ghp_abcdefghijklmnop123456",
		"AKIAIOSFODNN7EXAMPLE",
		"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgNqPnd9",
		"a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3f4a5b6c7d8e9f0a1b2c3d4",
		"mySecretP@ssw0rd!",
	}

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for _, secret := range secrets {
				logger.Info("race test",
					"id", id,
					"secret_key", secret,
					"message", "value: "+secret,
				)
			}
		}(i)
	}
	wg.Wait()

	output := buf.String()
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) < n {
		t.Errorf("Expected at least %d log lines, got %d", n, len(lines))
	}

	// Check that no secrets leaked
	for _, line := range lines {
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			continue
		}
		// Check secret_key attr
		if val, ok := record["secret_key"]; ok {
			if valStr, ok := val.(string); ok && valStr != "[REDACTED]" {
				for _, secret := range secrets {
					if strings.Contains(valStr, secret) {
						t.Errorf("Secret leaked in secret_key: %q", secret)
					}
				}
			}
		}
		// Check message attr
		if val, ok := record["message"]; ok {
			if valStr, ok := val.(string); ok {
				for _, secret := range secrets {
					if strings.Contains(valStr, secret) {
						t.Errorf("Secret leaked in message: %q in %q", secret, valStr)
					}
				}
			}
		}
	}
}

// ============================================================================
// ATTACK VECTOR 7: ReDoS — catastrophic backtracking
// ============================================================================

// TestAdversary_ReDoS checks if regex patterns cause excessive runtime
// on malicious inputs.
func TestAdversary_ReDoS(t *testing.T) {
	// Create a long string with many "a" chars that might cause backtracking
	// in the base64 pattern since [A-Za-z0-9+/] matches a-z
	longInput := strings.Repeat("a", 10000)
	
	timeout := 2 * time.Second
	done := make(chan bool, 1)

	go func() {
		_ = Redact(longInput + "sk-test")
		done <- true
	}()

	select {
	case <-done:
		// OK — completed in time
	case <-time.After(timeout):
		t.Errorf("ReDoS: Redact() took more than %v on long input", timeout)
	}

	// Also test with backtracking-prone input near the base64 boundary
	// The base64 pattern: \b[A-Za-z0-9+/]{40,}={0,2}\b
	// A string that is 39 chars of base64 followed by "=" then more chars
	// could cause issues
	edgeInput := strings.Repeat("a", 39) + "=b"
	_ = Redact(edgeInput)

	// Test with alternating character classes
	evil := "a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6q7r8s9t0u1v2w3x4y5z6" +
		strings.Repeat("a", 1000) + "sk-test"
	_ = Redact(evil)
}

// ============================================================================
// ATTACK VECTOR 8: Unicode homoglyphs
// ============================================================================

// TestAdversary_UnicodeHomoglyphs checks if Unicode characters that look
// like ASCII can bypass pattern matching.
func TestAdversary_UnicodeHomoglyphs(t *testing.T) {
	// Cyrillic small letter 'а' (U+0430) looks like 'a' but is different
	// Latin 'e' vs Cyrillic 'е' (U+0435)
	// Latin 'o' vs Cyrillic 'о' (U+043E)
	cases := []struct {
		name  string
		value string
	}{
		{
			name:  "cyrillic_in_password",
			value: "P@sswоrd", // Uses Cyrillic 'о' (U+043E) instead of Latin 'o'
		},
		{
			name:  "cyrillic_sk_prefix",
			value: "sk-аbсdеfgh", // Uses Cyrillic letters
		},
		{
			name:  "cyrillic_ghp_prefix",
			value: "ghp_аbсdеfgh", // Cyrillic in token body
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := Redact(tc.value)
			if strings.Contains(result, tc.value) && !strings.Contains(result, "[REDACTED]") {
				t.Logf("Unicode value not redacted (may be expected): %q -> %q", tc.value, result)
			}
		})
	}
}

// ============================================================================
// ATTACK VECTOR 9: False positives
// ============================================================================

// TestAdversary_FalsePositives checks if common normal strings are
// incorrectly redacted.
func TestAdversary_FalsePositives(t *testing.T) {
	cases := []struct {
		name  string
		value string
	}{
		// Strings containing "pass" that are NOT passwords
		{"passenger", "passenger清单"},
		{"compass", "compass bearing 270"},
		{"passed", "passed all tests"},
		{"passage", "passage of time"},
		{"passport", "passport number ABC123"},
		{"password_hint", "What is your pet's name?"}, // key has "password", value is safe
		// Strings containing "secret" that are NOT secrets
		{"secretary", "secretary@company.com"},
		{"secret_sauce", "The secret sauce is love"}, // This IS arguably sensitive
		// Strings containing "pwd" that are NOT passwords
		{"pwd_manager", "pwd_manager is a great app"},
		// Long hex-like strings that are NOT secrets
		{"commit_hash", "a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6"}, // 32 hex chars — this IS a commit hash
		{"git_short_hash", "a1b2c3d4e5f6"}, // 12 hex chars — short enough
		// Git commit hash that looks like hex
		{"git_hash_40", "a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0"},
		// UUID-like strings
		{"uuid_like", "550e8400-e29b-41d4-a716-446655440000"},
		// Normal numeric IDs
		{"long_number", "1234567890123456789012345678901234567890"}, // 40 digits
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := Redact(tc.value)
			if result == "[REDACTED]" || strings.Contains(result, "[REDACTED]") {
				t.Logf("FALSE POSITIVE: %q -> %q", tc.value, result)
			}
		})
	}
}

// ============================================================================
// ATTACK VECTOR 10: Bearer token in HTTP header format
// ============================================================================

// TestAdversary_BearerHeader checks if 'Authorization: Bearer eyJ...'
// is properly redacted.
func TestAdversary_BearerHeader(t *testing.T) {
	cases := []struct {
		name  string
		value string
	}{
		{
			name:  "bearer_jwt_header",
			value: "Authorization: Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgNqPnd9y8a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3f4a5b6c7d8e9f",
		},
		{
			name:  "bearer_opaque_full_header",
			value: "Authorization: Bearer dGhpcyBpcyBhIGNvbXBsZXRlbHkgZmFrZSBiZWFyZXIgdG9rZW4gZm9yIHRlc3Rpbmc=",
		},
		{
			name:  "only_bearer_token",
			value: "Bearer eyJ0eXAiOiJKV1QiLCJhbGciOiJIUzI1NiJ9.eyJpc3MiOiJPbmxpbmUgSldUIEJ1aWxkZXIiLCJpYXQiOjE3NDUwMjc1OTEsImV4cCI6MTc3NjU2MzU5MSwiYXVkIjoid3d3LmV4YW1wbGUuY29tIiwic3ViIjoianJvY2tldEBleGFtcGxlLmNvbSJ9.BqnOJk_cnzU4v1cP0e-W1jG4JwWqJp6xvO8dSfBpHMA",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := Redact(tc.value)
			if strings.Contains(result, tc.value) {
				t.Errorf("Bearer token leaked: output contains full input")
			}
			if !strings.Contains(result, "[REDACTED]") {
				t.Errorf("Bearer token NOT redacted: %q", result)
			}
		})
	}
}

// TestAdversary_AuthorizationHeader checks if the full HTTP Authorization
// header format is redacted, including through the logger.
func TestAdversary_AuthorizationHeader(t *testing.T) {
	var buf bytes.Buffer
	logger := NewLogger(slog.LevelInfo, &buf)

	// Simulate logging an HTTP request header
	authHeader := "Authorization: Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgNqPnd9y8a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3f4a5b6c7d8e9f"

	logger.Info("incoming request",
		"header", authHeader,
		"method", "GET",
		"path", "/api/v1/users",
	)

	output := buf.String()
	var record map[string]any
	if err := json.Unmarshal(buf.Bytes(), &record); err != nil {
		t.Fatalf("JSON parse error: %v", err)
	}

	// Check that the full auth header doesn't appear in raw output
	if strings.Contains(output, "eyJhbGciOiJIUzI1NiI") {
		t.Errorf("JWT token leaked in raw output")
	}

	// Check the header attribute was redacted
	val, ok := record["header"]
	if !ok {
		t.Fatal("expected header attribute")
	}
	valStr, ok := val.(string)
	if !ok {
		t.Fatalf("expected string, got %T", val)
	}
	if valStr != "[REDACTED]" && strings.Contains(valStr, "Authorization") {
		t.Errorf("Authorization header leaked: %q", valStr)
	}
	if !strings.Contains(valStr, "[REDACTED]") {
		t.Errorf("Expected [REDACTED] for Authorization header, got: %q", valStr)
	}
}

// ============================================================================
// ATTACK VECTOR 6b: Handler race with shared state
// ============================================================================

// TestAdversary_HandlerRace checks concurrent Handle calls on the same
// handler instance for data races.
func TestAdversary_HandlerRace(t *testing.T) {
	var mu sync.Mutex
	var buf bytes.Buffer
	// Protect buf: slog handlers may write concurrently under -race.
	w := writerFunc(func(p []byte) (int, error) {
		mu.Lock()
		defer mu.Unlock()
		return buf.Write(p)
	})
	handler := NewRedactingHandler(slog.NewJSONHandler(w, nil))

	var wg sync.WaitGroup
	var handleErrs int
	var errMu sync.Mutex
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			record := slog.NewRecord(time.Now(), slog.LevelInfo, "test message", 0)
			record.AddAttrs(
				slog.String("password", "hunter2"),
				slog.String("token", "sk-abcdefghijklmnopqrstuvwxyz"),
				slog.Int("id", id),
			)
			if err := handler.Handle(context.Background(), record); err != nil {
				errMu.Lock()
				handleErrs++
				errMu.Unlock()
			}
		}(i)
	}
	wg.Wait()
	if handleErrs != 0 {
		t.Errorf("Handle returned error %d times under concurrency", handleErrs)
	}
	mu.Lock()
	out := buf.String()
	mu.Unlock()
	if strings.Contains(out, "hunter2") {
		t.Error("password leaked under concurrent Handle")
	}
	if strings.Contains(out, "sk-abcdefghijklmnopqrstuvwxyz") {
		t.Error("token leaked under concurrent Handle")
	}
}

// writerFunc adapts a function to io.Writer for concurrent-safe test buffers.
type writerFunc func(p []byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) { return f(p) }

// TestAdversary_RedactConcurrent checks concurrent calls to Redact function.
func TestAdversary_RedactConcurrent(t *testing.T) {
	var wg sync.WaitGroup
	secrets := []struct {
		in         string
		mustRedact bool
	}{
		{"sk-abcdefghijklmnopqrstuvwxyz012345", true},
		{"ghp_abcdefghijklmnopqrstuvwxyz012345", true},
		{"AKIAIOSFODNN7EXAMPLE", true},
		{"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PlFUP0THsR8U", true},
		{"a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3f4a5b6c7d8e9f0a1b2c3d4", true},
		{"mySecretP@ssw0rd!", true},
		{"normal string", false},
		{"/var/log/app.log", false},
	}

	errCh := make(chan string, 100*len(secrets))
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for _, s := range secrets {
				got := Redact(s.in)
				if s.mustRedact {
					if got == s.in || !strings.Contains(got, "[REDACTED]") {
						errCh <- "not redacted: " + s.in
					}
				} else if got != s.in {
					errCh <- "false positive: " + s.in + " -> " + got
				}
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for msg := range errCh {
		t.Error(msg)
	}
}

// TestAdversary_RepeatedlyParsesLogOutput checks that redacted values
// in log output are consistently replaced even with high concurrency.
func TestAdversary_RepeatedlyParsesLogOutput(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping long concurrency test")
	}

	var buf bytes.Buffer
	logger := NewLogger(slog.LevelInfo, &buf)

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				logger.Info("concurrent log",
					"id", id*1000+j,
					"api_key", "sk-abcdefghijklmnop",
					"token_value", "ghp_abcdefghijklmnop",
				)
			}
		}(i)
	}
	wg.Wait()

	output := buf.String()
	lines := strings.Split(strings.TrimSpace(output), "\n")
	for i, line := range lines {
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			continue
		}
		if val, ok := record["api_key"]; ok {
			if valStr, ok := val.(string); ok && valStr != "[REDACTED]" {
				t.Errorf("Line %d: api_key not redacted: %q", i, valStr)
			}
		}
		if val, ok := record["token_value"]; ok {
			if valStr, ok := val.(string); ok && valStr != "[REDACTED]" {
				t.Errorf("Line %d: token_value not redacted: %q", i, valStr)
			}
		}
	}
}
