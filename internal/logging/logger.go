package logging

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
)

// RedactingHandler is a slog.Handler wrapper that redacts sensitive values
// from log records before delegating to the underlying handler.
type RedactingHandler struct {
	inner slog.Handler
}

// NewRedactingHandler creates a new RedactingHandler that wraps the given handler.
func NewRedactingHandler(inner slog.Handler) *RedactingHandler {
	return &RedactingHandler{inner: inner}
}

// Enabled reports whether the handler handles records at the given level.
// It delegates to the underlying handler.
func (h *RedactingHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

// Handle redacts sensitive values in the log record and delegates to the underlying handler.
func (h *RedactingHandler) Handle(ctx context.Context, record slog.Record) error {
	// Redact the log message.
	msg := Redact(record.Message)
	record.Message = msg

	// Collect and redact all attributes.
	var attrs []slog.Attr
	record.Attrs(func(a slog.Attr) bool {
		attrs = append(attrs, h.redactAttr(a))
		return true
	})

	// Build a new record with redacted attrs.
	newRecord := slog.NewRecord(record.Time, record.Level, record.Message, record.PC)
	for _, a := range attrs {
		newRecord.AddAttrs(a)
	}

	return h.inner.Handle(ctx, newRecord)
}

// WithAttrs returns a new handler with the given attributes pre-attached.
// The attributes are redacted before being stored.
func (h *RedactingHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	redacted := make([]slog.Attr, len(attrs))
	for i, a := range attrs {
		redacted[i] = h.redactAttr(a)
	}
	return &RedactingHandler{inner: h.inner.WithAttrs(redacted)}
}

// WithGroup returns a new handler with the given group name.
func (h *RedactingHandler) WithGroup(name string) slog.Handler {
	return &RedactingHandler{inner: h.inner.WithGroup(name)}
}

// redactAttr redacts sensitive values from a single attribute.
func (h *RedactingHandler) redactAttr(a slog.Attr) slog.Attr {
	// If the key suggests a sensitive value (e.g., "password"), redact entirely.
	if hasSensitiveKey(a.Key) {
		a.Value = slog.StringValue("[REDACTED]")
		return a
	}

	return h.redactAttrValue(a)
}

// redactAttrValue redacts the value of an attribute.
func (h *RedactingHandler) redactAttrValue(a slog.Attr) slog.Attr {
	switch a.Value.Kind() {
	case slog.KindString:
		val := a.Value.String()
		if isSensitiveValue(val) || (len(val) >= 20 && looksLikeAPIKey(val)) {
			a.Value = slog.StringValue("[REDACTED]")
		} else {
			redacted := Redact(val)
			if redacted != val {
				a.Value = slog.StringValue(redacted)
			}
		}
	case slog.KindGroup:
		// Recursively redact group attributes
		attrs := a.Value.Group()
		redacted := make([]slog.Attr, len(attrs))
		for i, attr := range attrs {
			redacted[i] = h.redactAttr(attr)
		}
		a.Value = slog.GroupValue(redacted...)
	case slog.KindAny:
		// For KindAny, try to extract string representation
		val := a.Value.Any()
		if s, ok := val.(string); ok {
			if isSensitiveValue(s) || (len(s) >= 20 && looksLikeAPIKey(s)) {
				a.Value = slog.StringValue("[REDACTED]")
			} else {
				redacted := Redact(s)
				if redacted != s {
					a.Value = slog.StringValue(redacted)
				}
			}
		} else if err, ok := val.(error); ok {
			// Handle error types: extract .Error() string and redact
			errStr := err.Error()
			redacted := Redact(errStr)
			a.Value = slog.StringValue(redacted)
		} else {
			// For other non-string types (structs, maps, etc.),
			// marshal to JSON, redact the JSON string, and store as string value.
			jsonBytes, marshalErr := json.Marshal(val)
			if marshalErr == nil {
				jsonStr := string(jsonBytes)
				redacted := Redact(jsonStr)
				a.Value = slog.StringValue(redacted)
			} else {
				a.Value = slog.StringValue(fmt.Sprintf("[REDACTED:%T]", val))
			}
		}
	}
	return a
}

// NewLogger creates a new slog.Logger with JSON output format and redaction enabled.
//
// The logger writes JSON-formatted log records to the given io.Writer.
// Log records at or above the specified level are emitted.
// All sensitive values (API keys, tokens, passwords, etc.) are automatically
// replaced with "[REDACTED]" in the output.
//
// Example:
//
//	logger := logging.NewLogger(slog.LevelInfo, os.Stdout)
//	logger.Info("server starting", "port", 8080)
func NewLogger(level slog.Level, output io.Writer) *slog.Logger {
	jsonHandler := slog.NewJSONHandler(output, &slog.HandlerOptions{
		Level: level,
	})
	redactingHandler := NewRedactingHandler(jsonHandler)
	return slog.New(redactingHandler)
}