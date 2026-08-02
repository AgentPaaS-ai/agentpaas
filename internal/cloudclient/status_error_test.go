package cloudclient

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestStatusErrorSurfacesAPIErrorField(t *testing.T) {
	resp := &http.Response{
		StatusCode: 503,
		Body:       io.NopCloser(strings.NewReader(`{"error":"cf_bind_not_configured"}`)),
	}
	err := statusError("create deployment", resp)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "cf_bind_not_configured") {
		t.Fatalf("want error field in message, got %q", err.Error())
	}
	if !strings.Contains(err.Error(), "503") {
		t.Fatalf("want status code in message, got %q", err.Error())
	}
}

func TestStatusErrorEmptyBody(t *testing.T) {
	resp := &http.Response{
		StatusCode: 500,
		Body:       io.NopCloser(strings.NewReader("")),
	}
	err := statusError("whoami", resp)
	if err == nil || !strings.Contains(err.Error(), "unexpected status 500") {
		t.Fatalf("got %v", err)
	}
}
