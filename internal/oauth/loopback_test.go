package oauth

import (
	"context"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestLoopbackConsent_BindLocalhostOnly(t *testing.T) {
	done := make(chan struct{}, 1)
	lc, err := StartLoopback("grant123", time.Minute, func(code, state string) error {
		if code != "abc" {
			t.Fatalf("code=%s", code)
		}
		done <- struct{}{}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = lc.Stop(context.Background()) }()

	if !strings.HasPrefix(lc.BaseURL(), "http://127.0.0.1:") {
		t.Fatalf("base=%s", lc.BaseURL())
	}
	// start page
	res, err := http.Get(lc.ConsentURL())
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	_ = res.Body.Close()
	if res.StatusCode != 200 || !strings.Contains(string(body), "Authorize agent access") {
		t.Fatalf("start status=%d body=%s", res.StatusCode, body)
	}
	// callback
	res2, err := http.Get(lc.BaseURL() + "/oauth/callback?code=abc&state=s")
	if err != nil {
		t.Fatal(err)
	}
	_ = res2.Body.Close()
	if res2.StatusCode != 200 {
		t.Fatalf("cb %d", res2.StatusCode)
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("onCode not called")
	}
	// ensure listener is loopback
	host, _, _ := net.SplitHostPort(strings.TrimPrefix(lc.BaseURL(), "http://"))
	if host != "127.0.0.1" {
		t.Fatalf("host=%s", host)
	}
}
