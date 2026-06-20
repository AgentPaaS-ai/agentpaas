package policy

import (
	"bytes"
	"testing"
)

// FuzzParsePolicy feeds random bytes to the YAML parser and verifies it
// either returns a valid policy or an error, but never panics.
func FuzzParsePolicy(f *testing.F) {
	// Seed corpus with sample policy YAML strings.
	seeds := []string{
		`version: "1.0"
agent:
  name: test-agent
`,
		`version: "1.0"
agent:
  name: demo-agent
  description: "A demonstration agent"
egress:
  - domain: "api.example.com"
    ports: [443]
credentials:
  - id: "my-api-key"
    type: header
    header: "X-API-Key"
    value: "${env:MY_API_KEY}"
`,
		`version: "1.0"
agent:
  name: cred-agent
credentials:
  - id: "github-token"
    type: header
    header: "Authorization"
    value: "Bearer ${env:GITHUB_TOKEN}"
`,
		`version: "1.0"
agent:
  name: mcp-agent
mcp_servers:
  - name: "claude"
    url: "https://api.anthropic.com/v1"
    headers:
      x-api-key: "${cred:my-api-key}"
hooks:
  - name: "notify"
    url: "https://hooks.example.com/notify"
ingress:
  - path: "/webhook"
    port: 8080
`,
		`version: "1.0"
agent:
  name: empty-egress-agent
egress: []
`,
		// Invalid YAML (parser should return error, not panic)
		`garbage: [`,

		// Edge case: empty input
		``,
		// Non-string scalar version
		`version: 123`,

		// Binary data seed: null bytes in string values
		string([]byte{118, 101, 114, 115, 105, 111, 110, 58, 32, 34, 49, 46, 48, 34, 10, 97, 103, 101, 110, 116, 58, 10, 32, 32, 110, 97, 109, 101, 58, 32, 34, 116, 101, 115, 116, 0, 97, 103, 101, 110, 116, 34, 10}),
		// YAML anchors and aliases
		`version: "1.0"
agent:
  name: anchor-agent
default_egress: &default
  domain: "default.example.com"
  ports: [443]
egress:
  - <<: *default
  - domain: "extra.example.com"
    ports: [80]
`,
		// Deeply nested YAML structure
		`version: "1.0"
agent:
  name: nested
credentials:
  - id: "a"
    type: header
    header: "X-A"
    value: "v1"
`,
		// Unicode domains
		`version: "1.0"
agent:
  name: unicode-agent
egress:
  - domain: "日本語.example.com"
    ports: [443]
  - domain: "xn--n8j6d.example.com"
    ports: [80]
`,
	}

	for _, s := range seeds {
		f.Add([]byte(s))
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		r := bytes.NewReader(data)
		_, err := ParsePolicy(r)
		if err != nil {
			// Error is expected for invalid input — just log it.
			t.Logf("ParsePolicy returned expected error: %v", err)
		}
		// If no error, the policy was parsed successfully — that's fine too.
		// The key assertion: we never panic.
	})
}

// FuzzCanonicalize feeds random bytes to the parser, then canonicalizes the
// result, verifying no panic and deterministic output (same input → same output).
func FuzzCanonicalize(f *testing.F) {
	// Seed corpus with sample policy YAML strings.
	seeds := []string{
		`version: "1.0"
agent:
  name: test-agent
`,
		`version: "1.0"
agent:
  name: demo-agent
egress:
  - domain: "api.example.com"
    ports: [443]
  - domain: "z.example.com"
    ports: [80, 443]
`,
		`version: "1.0"
agent:
  name: cred-agent
credentials:
  - id: "a-key"
    type: header
    header: "X-A"
    value: "secret-a"
  - id: "b-key"
    type: header
    header: "X-B"
    value: "secret-b"
`,
		`version: "1.0"
agent:
  name: mcp-agent
mcp_servers:
  - name: "z-server"
    url: "https://z.example.com/mcp"
  - name: "a-server"
    url: "https://a.example.com/mcp"
`,
		`version: "1.0"
agent:
  name: hook-agent
hooks:
  - name: "z-hook"
    url: "https://z.example.com/hook"
  - name: "a-hook"
    url: "https://a.example.com/hook"
`,
		`version: "1.0"
agent:
  name: ingress-agent
ingress:
  - path: "/z"
    port: 9090
  - path: "/a"
    port: 8080
`,
		// YAML anchors and aliases
		`version: "1.0"
agent:
  name: anchor-agent
default_egress: &default
  domain: "default.example.com"
  ports: [443]
egress:
  - <<: *default
  - domain: "extra.example.com"
    ports: [80]
`,
		// Unicode domains
		`version: "1.0"
agent:
  name: unicode-agent
egress:
  - domain: "日本語.example.com"
    ports: [443]
  - domain: "xn--n8j6d.example.com"
    ports: [80]
`,
	}

	for _, s := range seeds {
		f.Add([]byte(s))
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		r := bytes.NewReader(data)
		p, err := ParsePolicy(r)
		if err != nil {
			t.Logf("ParsePolicy returned error (expected for invalid input): %v", err)
			return
		}

		// Canonicalize once.
		cp1, warnings1 := Canonicalize(p)
		if cp1 == nil {
			t.Log("Canonicalize returned nil for a successfully parsed policy")
			return
		}
		_ = warnings1

		// Canonicalize again — deterministic output check.
		cp2, _ := Canonicalize(p)
		if cp2 == nil {
			t.Error("Canonicalize returned nil on second call")
			return
		}

		// Compare via JSON marshaling to ensure deterministic output.
		json1, err1 := marshalCanonicalJSON(cp1)
		json2, err2 := marshalCanonicalJSON(cp2)

		if err1 != nil {
			t.Errorf("first marshal failed: %v", err1)
			return
		}
		if err2 != nil {
			t.Errorf("second marshal failed: %v", err2)
			return
		}

		if !bytes.Equal(json1, json2) {
			t.Errorf("canonicalization not deterministic: first=%s second=%s", string(json1), string(json2))
		}
	})
}

// FuzzDigest feeds random bytes to the parser, computes the digest twice,
// and verifies they match.
func FuzzDigest(f *testing.F) {
	// Seed corpus with sample policy YAML strings.
	seeds := []string{
		`version: "1.0"
agent:
  name: test-agent
`,
		`version: "1.0"
agent:
  name: demo-agent
  description: "A demonstration agent"
egress:
  - domain: "api.example.com"
    ports: [443]
  - domain: "wild.example.com"
    ports: [80, 443]
    allow_wildcard: true
`,
		`version: "1.0"
agent:
  name: cred-agent
credentials:
  - id: "my-key"
    type: header
    header: "X-API-Key"
    value: "super-secret"
`,
		`version: "1.0"
agent:
  name: mcp-agent
mcp_servers:
  - name: "local-llm"
    url: "http://localhost:11434/v1"
`,
		`version: "1.0"
agent:
  name: full-agent
egress:
  - domain: "a.example.com"
    ports: [443]
  - domain: "b.example.com"
    ports: [80]
credentials:
  - id: "key-1"
    type: header
    header: "X-Key-1"
    value: "val-1"
  - id: "key-2"
    type: header
    header: "X-Key-2"
    value: "val-2"
mcp_servers:
  - name: "server-1"
    url: "https://s1.example.com/mcp"
hooks:
  - name: "hook-1"
    url: "https://h1.example.com/hook"
ingress:
  - path: "/webhook"
    port: 8080
`,
		// YAML anchors and aliases
		`version: "1.0"
agent:
  name: anchor-agent
default_egress: &default
  domain: "default.example.com"
  ports: [443]
egress:
  - <<: *default
  - domain: "extra.example.com"
    ports: [80]
`,
		// Unicode domains
		`version: "1.0"
agent:
  name: unicode-agent
egress:
  - domain: "日本語.example.com"
    ports: [443]
  - domain: "xn--n8j6d.example.com"
    ports: [80]
`,
	}

	for _, s := range seeds {
		f.Add([]byte(s))
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		r := bytes.NewReader(data)
		p, err := ParsePolicy(r)
		if err != nil {
			t.Logf("ParsePolicy returned error (expected for invalid input): %v", err)
			return
		}

		// Compute digest twice.
		d1, err1 := Digest(p)
		if err1 != nil {
			t.Errorf("Digest failed on valid policy: %v", err1)
			return
		}

		d2, err2 := Digest(p)
		if err2 != nil {
			t.Errorf("Digest failed on second call: %v", err2)
			return
		}

		if d1 != d2 {
			t.Errorf("digest not stable: first=%s second=%s", d1, d2)
		}
	})
}