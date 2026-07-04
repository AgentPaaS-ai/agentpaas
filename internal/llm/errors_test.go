package llm

import (
	"strings"
	"testing"
)

func TestFormatHTTPErrorXAIFmt(t *testing.T) {
	// xAI returns: {"code":"unauthenticated:bad-credentials","error":"The OAuth2 access token could not be validated."}
	body := []byte(`{"code":"unauthenticated:bad-credentials","error":"The OAuth2 access token could not be validated."}`)
	err := formatHTTPError("xai", 403, body)
	msg := err.Error()
	if !strings.Contains(msg, "OAuth2 access token could not be validated") {
		t.Errorf("expected provider message in error, got: %s", msg)
	}
	if !strings.Contains(msg, "403") {
		t.Errorf("expected status code 403 in error, got: %s", msg)
	}
	if !strings.Contains(msg, "expired") && !strings.Contains(msg, "rejected") {
		t.Errorf("expected credential-expiry guidance, got: %s", msg)
	}
}

func TestFormatHTTPErrorOpenAIFmt(t *testing.T) {
	// OpenAI: {"error":{"message":"...","type":"..."}}
	body := []byte(`{"error":{"message":"Incorrect API key provided","type":"invalid_request_error"}}`)
	err := formatHTTPError("openai", 401, body)
	msg := err.Error()
	if !strings.Contains(msg, "Incorrect API key provided") {
		t.Errorf("expected provider message, got: %s", msg)
	}
}
