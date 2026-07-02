// Package logging provides structured logging with built-in redaction for sensitive data.
//
// It uses Go's standard library log/slog package with JSON output format.
// The redacting handler intercepts all log records and replaces sensitive values
// (API keys, tokens, passwords, private keys, high-entropy strings) with "[REDACTED]".
//
// Usage:
//
//	logger := logging.NewLogger(slog.LevelInfo, os.Stdout)
//	logger.Info("server started", "addr", ":8080")
//	logger.Info("request", "api_key", "sk-test-key-1234567890abcdef")
//	// The API key above will be redacted in output.
package logging
