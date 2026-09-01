package harness

import (
	"encoding/base64"
	"testing"
)

func TestAuthorizationHeaderValue(t *testing.T) {
	userPass := "user@x.com:tok"
	wantBasic := "Basic " + base64.StdEncoding.EncodeToString([]byte(userPass))

	tests := []struct {
		name   string
		header string
		value  string
		want   string
	}{
		{
			name:   "non-authorization header with colon stays raw",
			header: "X-API-Key",
			value:  "foo:bar",
			want:   "foo:bar",
		},
		{
			name:   "authorization bearer scheme stays",
			header: "Authorization",
			value:  "Bearer tok",
			want:   "Bearer tok",
		},
		{
			name:   "authorization basic scheme stays",
			header: "Authorization",
			value:  "Basic abc",
			want:   "Basic abc",
		},
		{
			name:   "authorization user:pass becomes basic base64",
			header: "Authorization",
			value:  userPass,
			want:   wantBasic,
		},
		{
			name:   "authorization raw token stays",
			header: "Authorization",
			value:  "rawtoken",
			want:   "rawtoken",
		},
		{
			name:   "empty stays empty",
			header: "Authorization",
			value:  "",
			want:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := authorizationHeaderValue(tt.header, tt.value)
			if got != tt.want {
				t.Fatalf("authorizationHeaderValue(%q, %q) = %q, want %q", tt.header, tt.value, got, tt.want)
			}
		})
	}
}
