package daemon

import "testing"

func TestInvokeStdoutIndicatesError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		stdout string
		want   bool
	}{
		{
			name:   "ok status",
			stdout: `{"result":{"status":"OK"}}`,
			want:   false,
		},
		{
			name:   "succeeded status",
			stdout: `{"result":{"status":"SUCCEEDED"}}`,
			want:   false,
		},
		{
			name:   "envelope result.status ERROR",
			stdout: `{"result":{"status":"ERROR","error":"tool exploded"}}`,
			want:   true,
		},
		{
			name:   "nested result.result.status ERROR",
			stdout: `{"result":{"result":{"status":"ERROR","error":"nested fail"}}}`,
			want:   true,
		},
		{
			name:   "jsonrpc error object",
			stdout: `{"jsonrpc":"2.0","id":1,"error":{"code":-32603,"message":"Internal error"}}`,
			want:   true,
		},
		{
			name:   "jsonrpc success result",
			stdout: `{"jsonrpc":"2.0","id":1,"result":{"content":[]}}`,
			want:   false,
		},
		{
			name:   "non-json stdout is not an error",
			stdout: "not json",
			want:   false,
		},
		{
			name:   "empty stdout is not an error",
			stdout: "",
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := invokeStdoutIndicatesError(tt.stdout)
			if got != tt.want {
				t.Fatalf("invokeStdoutIndicatesError(%q) = %v, want %v", tt.stdout, got, tt.want)
			}
		})
	}
}
