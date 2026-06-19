package logging

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
)

// TestLoggerRedactsAttrs verifies that redaction works in slog attributes.
func TestLoggerRedactsAttrs(t *testing.T) {
	var buf bytes.Buffer
	logger := NewLogger(slog.LevelInfo, &buf)

	logger.Info("test message", "api_key", sentinelAPIKey)

	var record map[string]any
	if err := json.Unmarshal(buf.Bytes(), &record); err != nil {
		t.Fatalf("failed to parse JSON log output: %v", err)
	}

	val, ok := record["api_key"]
	if !ok {
		t.Fatal("expected api_key attribute in log output")
	}
	valStr, ok := val.(string)
	if !ok {
		t.Fatalf("expected string value, got %T", val)
	}
	if strings.Contains(valStr, sentinelAPIKey) {
		t.Errorf("sentinel API key found in log output: %q", valStr)
	}
	if valStr != "[REDACTED]" {
		t.Errorf("expected [REDACTED], got %q", valStr)
	}
}

// TestLoggerRedactsMessage verifies that redaction works in log messages.
func TestLoggerRedactsMessage(t *testing.T) {
	var buf bytes.Buffer
	logger := NewLogger(slog.LevelInfo, &buf)

	logger.Info("token is " + sentinelGitHubToken)

	var record map[string]any
	if err := json.Unmarshal(buf.Bytes(), &record); err != nil {
		t.Fatalf("failed to parse JSON log output: %v", err)
	}

	msg, ok := record["msg"]
	if !ok {
		t.Fatal("expected msg field in log output")
	}
	msgStr, ok := msg.(string)
	if !ok {
		t.Fatalf("expected string msg, got %T", msg)
	}
	if strings.Contains(msgStr, "ghp_") {
		t.Errorf("GitHub token found in log message: %q", msgStr)
	}
	if !strings.Contains(msgStr, "[REDACTED]") {
		t.Errorf("expected [REDACTED] in log message, got %q", msgStr)
	}
}

// TestLoggerRedactsError verifies that redaction works in error strings.
func TestLoggerRedactsError(t *testing.T) {
	var buf bytes.Buffer
	logger := NewLogger(slog.LevelInfo, &buf)

	logger.Info("request failed", "error", "invalid API key: "+sentinelAWSKey)

	var record map[string]any
	if err := json.Unmarshal(buf.Bytes(), &record); err != nil {
		t.Fatalf("failed to parse JSON log output: %v", err)
	}

	errStr, ok := record["error"]
	if !ok {
		t.Fatal("expected error attribute in log output")
	}
	errStrStr, ok := errStr.(string)
	if !ok {
		t.Fatalf("expected string error, got %T", errStr)
	}
	if strings.Contains(errStrStr, "AKIAIO...MPLE") {
		t.Errorf("AWS key found in error string: %q", errStrStr)
	}
	if !strings.Contains(errStrStr, "[REDACTED]") {
		t.Errorf("expected [REDACTED] in error string, got %q", errStrStr)
	}
}

// TestLoggerOutputsValidJSON verifies that the logger outputs valid JSON.
func TestLoggerOutputsValidJSON(t *testing.T) {
	var buf bytes.Buffer
	logger := NewLogger(slog.LevelInfo, &buf)

	logger.Info("hello world")
	logger.Info("some log", "key", "value")

	lines := bytes.Split(bytes.TrimSpace(buf.Bytes()), []byte("\n"))
	for i, line := range lines {
		var record map[string]any
		if err := json.Unmarshal(line, &record); err != nil {
			t.Errorf("line %d is not valid JSON: %v\nraw: %s", i, err, line)
		}
		if record["msg"] == nil {
			t.Errorf("line %d missing msg field: %s", i, line)
		}
	}
}

// TestLoggerLevelFiltering verifies that Debug messages don't appear at Info level.
func TestLoggerLevelFiltering(t *testing.T) {
	var buf bytes.Buffer
	logger := NewLogger(slog.LevelInfo, &buf)

	logger.Debug("this should not appear")
	logger.Info("this should appear")

	lines := bytes.Split(bytes.TrimSpace(buf.Bytes()), []byte("\n"))

	// Should only have one line (the Info one)
	if len(lines) != 1 {
		t.Errorf("expected 1 log line (Info), got %d lines", len(lines))
	}

	var record map[string]any
	_ = json.Unmarshal(lines[0], &record)
	if record["msg"] != "this should appear" {
		t.Errorf("expected msg 'this should appear', got %v", record["msg"])
	}
}

// TestLoggerMultipleRedactedValues verifies that multiple redacted values in one log entry are all redacted.
func TestLoggerMultipleRedactedValues(t *testing.T) {
	var buf bytes.Buffer
	logger := NewLogger(slog.LevelInfo, &buf)

	logger.Info("test",
		"api_key", sentinelAPIKey,
		"github_token", sentinelGitHubToken,
		"jwt", sentinelJWT,
	)

	var record map[string]any
	if err := json.Unmarshal(buf.Bytes(), &record); err != nil {
		t.Fatalf("failed to parse JSON log output: %v", err)
	}

	for _, key := range []string{"api_key", "github_token", "jwt"} {
		val, ok := record[key]
		if !ok {
			t.Errorf("expected key %q in log output", key)
			continue
		}
		valStr, ok := val.(string)
		if !ok {
			t.Errorf("expected string for key %q, got %T", key, val)
			continue
		}
		if valStr != "[REDACTED]" {
			t.Errorf("key %q expected [REDACTED], got %q", key, valStr)
		}
	}
}

// TestLoggerAllSentinelsInAttrs ensures that all planted sentinel values never appear in log output.
func TestLoggerAllSentinelsInAttrs(t *testing.T) {
	sentinels := map[string]string{
		"api_key":      sentinelAPIKey,
		"github_token": sentinelGitHubToken,
		"jwt":          sentinelJWT,
		"aws_key":      sentinelAWSKey,
		"base64":       sentinelBase64High,
		"hex":          sentinelHexLong,
	}

	var buf bytes.Buffer
	logger := NewLogger(slog.LevelInfo, &buf)

	attrs := make([]any, 0, len(sentinels)*2)
	for k, v := range sentinels {
		attrs = append(attrs, k, v)
	}
	logger.Info("test all sentinels", attrs...)

	output := buf.String()
	for name, val := range sentinels {
		if strings.Contains(output, val) {
			t.Errorf("sentinel %q value found in log output", name)
		}
	}
	// Should contain exactly one full private key sentinel check — but the private key
	// is multiline and was passed as an attr value too. Let's check it separately.
	if strings.Contains(output, "-----BEGIN") {
		t.Error("private key marker found in log output")
	}
}

// TestLoggerPrivateKeyInAttrs verifies that private key blocks are redacted in attributes.
func TestLoggerPrivateKeyInAttrs(t *testing.T) {
	var buf bytes.Buffer
	logger := NewLogger(slog.LevelInfo, &buf)

	logger.Info("test", "private_key", sentinelPrivateKey)

	var record map[string]any
	if err := json.Unmarshal(buf.Bytes(), &record); err != nil {
		t.Fatalf("failed to parse JSON log output: %v", err)
	}

	val, ok := record["private_key"]
	if !ok {
		t.Fatal("expected private_key attribute in log output")
	}
	valStr, ok := val.(string)
	if !ok {
		t.Fatalf("expected string value, got %T", val)
	}
	if valStr != "[REDACTED]" {
		t.Errorf("expected [REDACTED], got %q", valStr)
	}
}

// TestLoggerMessageWithPrivateKey verifies that private key in message is redacted.
func TestLoggerMessageWithPrivateKey(t *testing.T) {
	var buf bytes.Buffer
	logger := NewLogger(slog.LevelInfo, &buf)

	logger.Info("key: " + sentinelPrivateKey)

	output := buf.String()
	if strings.Contains(output, "-----BEGIN") {
		t.Error("private key marker found in log output")
	}
	if !strings.Contains(output, "[REDACTED]") {
		t.Error("expected [REDACTED] in log output")
	}
}

// TestDefaultLogger verifies NewLogger returns a non-nil logger.
func TestDefaultLogger(t *testing.T) {
	var buf bytes.Buffer
	logger := NewLogger(slog.LevelInfo, &buf)
	if logger == nil {
		t.Fatal("NewLogger returned nil")
	}
	logger.Info("default logger works")
	if buf.Len() == 0 {
		t.Error("expected log output")
	}
}

// TestNormalStringsPreserved verifies that normal strings (paths, names) are preserved in log output.
func TestNormalStringsPreserved(t *testing.T) {
	var buf bytes.Buffer
	logger := NewLogger(slog.LevelInfo, &buf)

	logger.Info("request completed",
		"path", "/api/v1/users",
		"method", "GET",
		"status", 200,
	)

	var record map[string]any
	if err := json.Unmarshal(buf.Bytes(), &record); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	if record["msg"] != "request completed" {
		t.Errorf("expected msg 'request completed', got %v", record["msg"])
	}
	if record["path"] != "/api/v1/users" {
		t.Errorf("expected path '/api/v1/users', got %v", record["path"])
	}
	if record["method"] != "GET" {
		t.Errorf("expected method 'GET', got %v", record["method"])
	}
}