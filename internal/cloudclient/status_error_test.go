package cloudclient

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestStatusErrorRegistryAuthentication10000ProvidesPushCoaching(t *testing.T) {
	resp := &http.Response{
		StatusCode: http.StatusBadRequest,
		Body:       io.NopCloser(strings.NewReader(`{"error":"Authentication error 10000","message":"registry authentication failed"}`)),
	}

	err := statusError("upload image", resp)
	if !strings.Contains(err.Error(), "session is OK if cloud whoami works") {
		t.Fatalf("error = %q, want whoami coaching", err)
	}
	if strings.Contains(strings.ToLower(err.Error()), "log in again") {
		t.Fatalf("error must not suggest a cloud login loop: %q", err)
	}
}

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
