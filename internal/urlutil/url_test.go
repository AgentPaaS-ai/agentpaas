package urlutil

import (
	"strings"
	"testing"
)

func TestStripUserinfo(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"no_userinfo", "https://example.com/path", "https://example.com/path"},
		{"user_only", "https://alice@example.com/x", "https://example.com/x"},
		{"user_pass", "https://alice:s3cret@example.com/x", "https://example.com/x"},
		{"preserves_query", "https://u:p@example.com/p?q=1#frag", "https://example.com/p?q=1#frag"},
		{"no_scheme_opaque", "not a url", "not a url"},
		{"invalid_keeps_original", "://bad", "://bad"},
		{"ftp_with_userinfo", "ftp://user:pass@host/file", "ftp://host/file"},
		// empty password still has userinfo
		{"empty_password", "https://user:@example.com/", "https://example.com/"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := StripUserinfo(tc.in)
			if got != tc.want {
				t.Fatalf("StripUserinfo(%q) = %q, want %q", tc.in, got, tc.want)
			}
			// Never leak password material when input had userinfo with password
			if strings.Contains(tc.in, "s3cret") && strings.Contains(got, "s3cret") {
				t.Fatalf("password leaked in output: %q", got)
			}
			if strings.Contains(tc.in, "pass@") && strings.Contains(got, "pass") && strings.Contains(got, "@") {
				// pass might appear in path legitimately; ensure userinfo form gone
				if strings.Contains(got, "user:pass@") || strings.Contains(got, "://pass@") {
					t.Fatalf("userinfo leaked: %q", got)
				}
			}
		})
	}
}

func TestStripUserinfo_Idempotent(t *testing.T) {
	t.Parallel()
	in := "https://alice:pw@example.com/path?x=1"
	once := StripUserinfo(in)
	twice := StripUserinfo(once)
	if once != twice {
		t.Fatalf("not idempotent: %q vs %q", once, twice)
	}
	// No userinfo input must be byte-identical (preserve canonical digests)
	plain := "https://example.com/path"
	if StripUserinfo(plain) != plain {
		t.Fatalf("no-userinfo URL must be returned unchanged: got %q", StripUserinfo(plain))
	}
}
