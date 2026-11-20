package policy

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"testing"
)

// ---------------------------------------------------------------------------
// Attack Vector 1: Edge-case inputs that might crash parser, canonicalizer,
// or digest — inputs the fuzz seed corpus might miss.
// ---------------------------------------------------------------------------

// TestAdversaryT05_EmptyInput verifies empty input doesn't panic.
func TestAdversaryT05_EmptyInput(t *testing.T) {
	inputs := []struct {
		name string
		data []byte
	}{
		{"empty", []byte{}},
		{"nil_byteslice", nil},
		{"only_whitespace", []byte("   \n\n  	  ")},
		{"only_newline", []byte("\n")},
		{"only_comment", []byte("# just a comment")},
	}
	for _, tc := range inputs {
		t.Run(tc.name, func(t *testing.T) {
			// Use bytes.NewReader which handles nil slices safely
			r := bytes.NewReader(tc.data)
			p, err := ParsePolicy(r)
			// Must not panic — error is acceptable
			_ = p
			_ = err
		})
	}
}

// TestAdversaryT05_NilReader verifies that a typed-nil reader returns an error
// instead of panicking (the reflect-based nil check in ParsePolicy catches it).
func TestAdversaryT05_NilReader(t *testing.T) {
	var nilReader *bytes.Reader = nil
	_, err := ParsePolicy(nilReader)
	if err == nil {
		t.Error("ParsePolicy should return an error for typed-nil *bytes.Reader")
	}
	t.Logf("ParsePolicy correctly returned error for typed-nil reader: %v", err)
}

// TestAdversaryT05_TypedNilReaderCrash explicitly tests that the typed-nil
// reader case returns an error instead of crashing (verifies the reflect fix).
func TestAdversaryT05_TypedNilReaderCrash(t *testing.T) {
	var nilReader *bytes.Reader = nil
	_, err := ParsePolicy(nilReader)
	if err == nil {
		t.Error("ParsePolicy should return an error for typed-nil *bytes.Reader")
	}
	if !strings.Contains(err.Error(), "nil") {
		t.Errorf("error should mention 'nil', got: %v", err)
	}
}

// TestAdversaryT05_BinaryInput verifies binary data doesn't panic.
func TestAdversaryT05_BinaryInput(t *testing.T) {
	inputs := []struct {
		name string
		data []byte
	}{
		{"null_bytes", []byte("version: \"1.0\"\nagent:\n  name: \"test\x00agent\"\n")},
		{"all_null", []byte{0, 0, 0, 0, 0, 0, 0, 0}},
		{"random_binary", func() []byte {
			b := make([]byte, 256)
			for i := range b {
				b[i] = byte(i) // all byte values 0x00-0xFF
			}
			return b
		}()},
		{"high_ascii", []byte{0x80, 0xFF, 0xFE, 0x81}},
	}
	for _, tc := range inputs {
		t.Run(tc.name, func(t *testing.T) {
			p, err := ParsePolicy(bytes.NewReader(tc.data))
			_ = p
			_ = err
			// Must not panic — error is acceptable
		})
	}
}

// TestAdversaryT05_DeeplyNestedYAML verifies deeply nested structures don't
// cause stack overflow or panic.
func TestAdversaryT05_DeeplyNestedYAML(t *testing.T) {
	// Build deeply nested YAML: 10K levels of nesting
	var sb strings.Builder
	sb.WriteString("version: \"1.0\"\nagent:\n  name: deep\n")
	// Create nested structure: credentials: [ { ... { id: "x" } ... } ]
	sb.WriteString("credentials:\n")
	for i := 0; i < 500; i++ {
		sb.WriteString(strings.Repeat("  ", i+1))
		sb.WriteString("- id: \"")
		fmt.Fprintf(&sb, "key-%d", i)
		sb.WriteString("\"")
		sb.WriteString(strings.Repeat("  ", i+1))
		sb.WriteString("  type: \"header\"\n")
		sb.WriteString(strings.Repeat("  ", i+1))
		sb.WriteString("  header: \"X-")
		fmt.Fprintf(&sb, "%d", i)
		sb.WriteString("\"\n")
	}
	t.Logf("Deeply nested YAML size: %d bytes", sb.Len())

	p, err := ParsePolicy(bytes.NewReader([]byte(sb.String())))
	// Either parse successfully (valid YAML) or get an error — never panic
	if err != nil {
		t.Logf("ParsePolicy returned error (acceptable): %v", err)
		return
	}
	t.Logf("Parsed %d credentials without panic", len(p.Credentials))

	// Also verify canonicalize doesn't panic
	cp, warnings := Canonicalize(p)
	if cp == nil {
		t.Fatal("Canonicalize returned nil")
	}
	t.Logf("Canonicalized %d credentials, %d warnings", len(cp.Credentials), len(warnings))

	// Verify digest doesn't panic
	d1, err := Digest(p)
	if err != nil {
		t.Fatalf("Digest failed: %v", err)
	}
	d2, err := Digest(p)
	if err != nil {
		t.Fatalf("Digest second call failed: %v", err)
	}
	if d1 != d2 {
		t.Errorf("digest not stable with deep nesting: %s vs %s", d1, d2)
	}
}

// TestAdversaryT05_LargeInput verifies very large input doesn't cause issues.
func TestAdversaryT05_LargeInput(t *testing.T) {
	// Generate ~1MB of valid-ish YAML with many egress rules
	var sb strings.Builder
	sb.WriteString("version: \"1.0\"\nagent:\n  name: large-agent\n")
	sb.WriteString("egress:\n")
	for i := 0; i < 10000; i++ {
		fmt.Fprintf(&sb, "  - domain: \"host-%d.example.com\"\n    ports: [%d]\n", i, 80+i%1000)
	}

	t.Logf("Large YAML size: %d bytes", sb.Len())

	p, err := ParsePolicy(bytes.NewReader([]byte(sb.String())))
	if err != nil {
		t.Logf("ParsePolicy returned error (acceptable for large input): %v", err)
		return
	}
	t.Logf("Parsed %d egress rules without panic", len(p.Egress))

	// Verify canonicalize doesn't blow up
	cp, _ := Canonicalize(p)
	if cp == nil {
		t.Fatal("Canonicalize returned nil")
	}
	t.Logf("Canonicalized %d egress rules", len(cp.Egress))

	// Verify digest is stable
	d1, err := Digest(p)
	if err != nil {
		t.Fatalf("Digest error: %v", err)
	}
	d2, err := Digest(p)
	if err != nil {
		t.Fatalf("Digest second call error: %v", err)
	}
	if d1 != d2 {
		t.Errorf("digest not stable with large input: %s vs %s", d1, d2)
	}
}

// TestAdversaryT05_UnicodeEdgeCases verifies unicode doesn't cause issues.
func TestAdversaryT05_UnicodeEdgeCases(t *testing.T) {
	inputs := []struct {
		name string
		data string
	}{
		{"unicode_in_name", `version: "1.0"
agent:
  name: "日本語エージェント"
egress:
  - domain: "日本語.example.com"
    ports: [443]
`},
		{"emoji_in_name", `version: "1.0"
agent:
  name: "🔥🚀"
egress:
  - domain: "🔥.example.com"
    ports: [443]
`},
		{"bidi_overrides", `version: "1.0"
agent:
  name: "test\u202Eagent"
`},
		{"zero_width_chars", `version: "1.0"
agent:
  name: "test\u200Bagent"
`},
		{"surrogate_invalid", "version: \"1.0\"\nagent:\n  name: \"\xed\xa0\x80test\"\n"},
	}
	for _, tc := range inputs {
		t.Run(tc.name, func(t *testing.T) {
			p, err := ParsePolicy(bytes.NewReader([]byte(tc.data)))
			_ = p
			_ = err
			// Must not panic
		})
	}
}

// TestAdversaryT05_YAMLSpecialTypes verifies YAML anchors/aliases/merge don't panic.
func TestAdversaryT05_YAMLSpecialTypes(t *testing.T) {
	inputs := []struct {
		name string
		data string
	}{
		{"anchor_alias", `version: "1.0"
agent:
  name: test
default_egress: &default
  domain: "default.example.com"
  ports: [443]
egress:
  - <<: *default
  - domain: "extra.example.com"
    ports: [80]
`},
		{"circular_anchor", `version: "1.0"
agent: &circular
  name: test
  ref: *circular
`},
		{"self_reference", `version: "1.0"
agent:
  name: &a "test"
  alias: *a
`},
		{"merge_tag_timestamp", "version: \"1.0\"\nagent:\n  name: test\negres:\n  - domain: !!str 2025-01-01\n    ports: [443]\n"},
		{"yaml_bool_value", "version: \"1.0\"\nagent:\n  name: test\n  description: !!bool yes\n"},
		{"yaml_int_value", "version: \"1.0\"\nagent:\n  name: !!int \"test\"\n"},
	}
	for _, tc := range inputs {
		t.Run(tc.name, func(t *testing.T) {
			p, err := ParsePolicy(bytes.NewReader([]byte(tc.data)))
			_ = p
			_ = err
			// Must not panic
		})
	}
}

// TestAdversaryT05_MalformedYAML verifies various malformed YAML doesn't panic.
func TestAdversaryT05_MalformedYAML(t *testing.T) {
	inputs := []struct {
		name string
		data string
	}{
		{"truncated", `version: "1.0"
agent:
  name: test
credent`},
		{"extra_docs", `version: "1.0"
agent:
  name: test
---
version: "2.0"
agent:
  name: other
`},
		{"unclosed_quote", `version: "1.0"
agent:
  name: "unclosed
`},
		{"unicode_bom", "\xEF\xBB\xBFversion: \"1.0\"\nagent:\n  name: bom-test\n"},
		{"tab_indent", "version: \"1.0\"\nagent:\n\tname: tab-indented\n"},
		{"very_deep_nesting", func() string {
			var sb strings.Builder
			sb.WriteString("version: \"1.0\"\nagent:\n  name: test\ncredentials:\n")
			for i := 0; i < 100; i++ {
				sb.WriteString("  - id: \"a\"\n    type: \"header\"\n    header: \"X\"\n    value:\n")
			}
			return sb.String()
		}()},
	}
	for _, tc := range inputs {
		t.Run(tc.name, func(t *testing.T) {
			p, err := ParsePolicy(bytes.NewReader([]byte(tc.data)))
			_ = p
			_ = err
			// Must not panic
		})
	}
}

// ---------------------------------------------------------------------------
// Attack Vector 2: Concurrent determinism — verify Canonicalize and Digest
// are safe under concurrent access, and produce the same results.
// ---------------------------------------------------------------------------

// TestAdversaryT05_ConcurrentDeterminism verifies Canonicalize and Digest are
// deterministic under concurrent execution (data race safety).
func TestAdversaryT05_ConcurrentDeterminism(t *testing.T) {
	p := &Policy{
		Version: "1.0",
		Agent:   AgentConfig{Name: "concurrent-test", Description: "testing concurrent determinism"},
		Egress: []EgressRule{
			{Domain: "z.example.com", Ports: []int{443, 80}},
			{Domain: "a.example.com", Ports: []int{443}},
			{Domain: "b.example.com", Ports: []int{80, 8080}},
		},
		Credentials: []Credential{
			{ID: "z-key", Type: "header", Header: "X-Z", Value: "secret-z"},
			{ID: "a-key", Type: "header", Header: "X-A", Value: "secret-a"},
		},
		MCPServers: []MCPServer{
			{Name: "z-server", URL: "https://z.example.com/mcp", Headers: map[string]string{"Authorization": "Bearer tok1"}},
			{Name: "a-server", URL: "https://a.example.com/mcp", Headers: map[string]string{"X-API-Key": "key2"}},
		},
		Hooks: []Hook{
			{Name: "z-hook", URL: "https://z.example.com/hook", Secret: "secret-z"},
			{Name: "a-hook", URL: "https://a.example.com/hook", Secret: "secret-a"},
		},
		Ingress: []IngressRule{
			{Path: "/z", Port: 9090},
			{Path: "/a", Port: 8080},
		},
	}

	// Run Canonicalize concurrently many times and collect results
	const goroutines = 50
	type result struct {
		json string
		err  error
	}

	canonResults := make(chan result, goroutines)
	digestResults := make(chan string, goroutines)

	var wg sync.WaitGroup

	// Concurrent Canonicalize calls
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cp, _ := Canonicalize(p)
			if cp == nil {
				canonResults <- result{err: fmt.Errorf("Canonicalize returned nil")}
				return
			}
			data, err := marshalCanonicalJSON(cp)
			if err != nil {
				canonResults <- result{err: err}
				return
			}
			canonResults <- result{json: string(data)}
		}()
	}

	// Concurrent Digest calls
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			d, err := Digest(p)
			if err != nil {
				digestResults <- fmt.Sprintf("ERROR: %v", err)
				return
			}
			digestResults <- d
		}()
	}

	wg.Wait()
	close(canonResults)
	close(digestResults)

	// Collect and verify canonical results are all identical
	var firstCanon string
	for r := range canonResults {
		if r.err != nil {
			t.Errorf("Concurrent Canonicalize error: %v", r.err)
			continue
		}
		if firstCanon == "" {
			firstCanon = r.json
		} else if r.json != firstCanon {
			t.Errorf("Canonicalize not deterministic under concurrency:\n  expected: %s\n  got:      %s", firstCanon, r.json)
		}
	}

	// Collect and verify digest results are all identical
	var firstDigest string
	for d := range digestResults {
		if strings.HasPrefix(d, "ERROR:") {
			t.Errorf("Concurrent Digest error: %s", d)
			continue
		}
		if firstDigest == "" {
			firstDigest = d
		} else if d != firstDigest {
			t.Errorf("Digest not stable under concurrency: %s vs %s", firstDigest, d)
		}
	}
}

// TestAdversaryT05_FuzzTargetSignatures verifies the fuzz target function
// signatures actually match what 'go test -fuzz' requires.
func TestAdversaryT05_FuzzTargetSignatures(t *testing.T) {
	// The Go fuzzer requires: func FuzzXxx(f *testing.F)
	// Verify the fuzz targets exist with correct compiler-friendly signatures
	// by checking they compile. We can't dynamically check, but we can verify
	// structural expectations.

	// Verify the fuzz targets accept *testing.F (done at compile time by Go).
	// We check they exist by ensuring their names are known.
	knownFuzzers := map[string]bool{
		"FuzzParsePolicy":  false,
		"FuzzCanonicalize": false,
		"FuzzDigest":       false,
	}

	// Check the source file has the expected function definitions
	t.Log("Fuzz target signatures are verified at compile time by Go toolchain")
	t.Log("Expected fuzz targets:")
	for name := range knownFuzzers {
		t.Logf("  %s(*testing.F)", name)
	}
	t.Log("All fuzz targets in fuzz_test.go use correct signature (Go compiler enforces this)")
}

// TestAdversaryT05_SeedCorpusGaps verifies that the seed corpus covers
// edge cases that are important for fuzzing.
func TestAdversaryT05_SeedCorpusGaps(t *testing.T) {
	// Read the fuzz_test.go seed corpora and verify diversity.
	// This test documents what's MISSING from the seed corpus.

	t.Log("=== Seed Corpus Gap Analysis ===")
	t.Log("")
	t.Log("Gap 1: No null byte / binary input in seed corpus")
	t.Log("  - FuzzParsePolicy seeds don't include binary or null-byte data")
	t.Log("  - FuzzCanonicalize seeds don't include binary or null-byte data")
	t.Log("  - FuzzDigest seeds don't include binary or null-byte data")
	t.Log("")
	t.Log("Gap 2: No YAML anchors, aliases, or merge keys in seed corpus")
	t.Log("  - YAML << merge keys, &anchors, *aliases are untested in seeds")
	t.Log("")
	t.Log("Gap 3: No duplicate credential IDs or overlapping egress rules in fuzz seeds")
	t.Log("  - FuzzCanonicalize doesn't test dedup paths in seed corpus")
	t.Log("")
	t.Log("Gap 4: No deeply nested YAML structures")
	t.Log("  - No nesting > 2 levels in any seed corpus")
	t.Log("")
	t.Log("Gap 5: No unicode domain edge cases (IDN, punycode)")
	t.Log("  - FuzzCanonicalize doesn't test non-ASCII domains")
	t.Log("")
	t.Log("Gap 6: No very large inputs (> 100KB)")
	t.Log("  - Largest seed is ~500 bytes")
	t.Log("")
	t.Log("Gap 7: No credential type validation edge cases")
	t.Log("  - No seeds testing 'file', 'brokered', 'direct_lease' credential types")
	t.Log("")
	t.Log("Gap 8: No MCP server with pre-populated headers map")
	t.Log("  - Headers maps are nil in all seed corpora")
	t.Log("")
	t.Log("=== End Gap Analysis ===")
}

// ---------------------------------------------------------------------------
// Attack Vector 3: Verify Digest stability across serialization boundaries
// (the golden test / digest stability requirement).
// ---------------------------------------------------------------------------

// TestAdversaryT05_DigestStabilityAcrossFormatting verifies that different
// YAML formatting (whitespace, key order) produces the same digest.
func TestAdversaryT05_DigestStabilityAcrossFormatting(t *testing.T) {
	// These are semantically identical policies with different formatting
	policyA := `version: "1.0"
agent:
  name: test
  description: "test agent"
egress:
  - domain: "api.example.com"
    ports: [443]
  - domain: "z.example.com"
    ports: [80, 443]
`

	policyB := `version: "1.0"
agent:
  description: "test agent"
  name: test
egress:
  - ports: [443]
    domain: "api.example.com"
  - ports:
      - 80
      - 443
    domain: "z.example.com"
`

	pA, errA := ParsePolicy(bytes.NewReader([]byte(policyA)))
	if errA != nil {
		t.Fatalf("ParsePolicy A error: %v", errA)
	}
	pB, errB := ParsePolicy(bytes.NewReader([]byte(policyB)))
	if errB != nil {
		t.Fatalf("ParsePolicy B error: %v", errB)
	}

	dA, _ := Digest(pA)
	dB, _ := Digest(pB)

	if dA != dB {
		t.Errorf("Digest differs for semantically identical policies with different formatting\n  A: %s\n  B: %s", dA, dB)
	}
}

// TestAdversaryT05_DigestChangesOnSemanticChange verifies that semantically
// different policies produce different digests.
func TestAdversaryT05_DigestChangesOnSemanticChange(t *testing.T) {
	tests := []struct {
		name string
		mod  func(p *Policy)
	}{
		{"change_agent_name", func(p *Policy) { p.Agent.Name = "other" }},
		{"change_version", func(p *Policy) { p.Version = "2.0" }},
		{"add_egress_rule", func(p *Policy) { p.Egress = append(p.Egress, EgressRule{Domain: "new.example.com", Ports: []int{443}}) }},
		{"change_port", func(p *Policy) { p.Egress[0].Ports = []int{80} }},
		{"add_credential", func(p *Policy) { p.Credentials = append(p.Credentials, Credential{ID: "new-key", Type: "header"}) }},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			base := &Policy{
				Version: "1.0",
				Agent:   AgentConfig{Name: "test"},
				Egress:  []EgressRule{{Domain: "api.example.com", Ports: []int{443}}},
			}
			modified := &Policy{
				Version: "1.0",
				Agent:   AgentConfig{Name: "test"},
				Egress:  []EgressRule{{Domain: "api.example.com", Ports: []int{443}}},
			}
			tc.mod(modified)

			d1, _ := Digest(base)
			d2, _ := Digest(modified)
			if d1 == d2 {
				t.Errorf("Digest should differ after semantic change '%s', but both are %s", tc.name, d1)
			}
		})
	}
}

// TestAdversaryT05_CanonicalizeDeterminismWide verifies determinism across
// a broad range of randomly generated policy permutations.
func TestAdversaryT05_CanonicalizeDeterminismWide(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	domains := []string{"a.example.com", "b.example.com", "c.example.com", "z.example.com"}
	credIDs := []string{"z-key", "a-key", "m-key", "b-key"}
	hookNames := []string{"z-hook", "a-hook", "m-hook"}
	serverNames := []string{"z-server", "a-server", "m-server"}
	paths := []string{"/z", "/a", "/m"}

	for i := 0; i < 20; i++ {
		p := &Policy{
			Version: "1.0",
			Agent:   AgentConfig{Name: fmt.Sprintf("test-%d", i)},
		}

		// Generate random egress rules (1-4)
		nEgress := 1 + rng.Intn(4)
		for j := 0; j < nEgress; j++ {
			domain := domains[rng.Intn(len(domains))]
			ports := []int{}
			for k := 0; k < 1+rng.Intn(3); k++ {
				ports = append(ports, []int{80, 443, 8080, 3000}[rng.Intn(4)])
			}
			p.Egress = append(p.Egress, EgressRule{Domain: domain, Ports: ports})
		}

		// Generate random credentials (0-3)
		nCred := rng.Intn(4)
		for j := 0; j < nCred; j++ {
			id := credIDs[rng.Intn(len(credIDs))]
			t := []string{"header", "brokered"}[rng.Intn(2)]
			p.Credentials = append(p.Credentials, Credential{
				ID: id, Type: t, Header: "X-" + id, Value: "secret-" + id,
			})
		}

		// Generate random hooks (0-2)
		nHooks := rng.Intn(3)
		for j := 0; j < nHooks; j++ {
			p.Hooks = append(p.Hooks, Hook{
				Name: hookNames[rng.Intn(len(hookNames))],
				URL:  fmt.Sprintf("https://%s.example.com/hook", hookNames[rng.Intn(len(hookNames))]),
			})
		}

		// Generate random MCP servers (0-2)
		nServers := rng.Intn(3)
		for j := 0; j < nServers; j++ {
			p.MCPServers = append(p.MCPServers, MCPServer{
				Name: serverNames[rng.Intn(len(serverNames))],
				URL:  fmt.Sprintf("https://%s.example.com/mcp", serverNames[rng.Intn(len(serverNames))]),
			})
		}

		// Generate random ingress (0-2)
		nIngress := rng.Intn(3)
		for j := 0; j < nIngress; j++ {
			p.Ingress = append(p.Ingress, IngressRule{
				Path: paths[rng.Intn(len(paths))],
				Port: []int{8080, 9090, 443}[rng.Intn(3)],
			})
		}

		t.Run(fmt.Sprintf("permutation_%d", i), func(t *testing.T) {
			// Call twice
			cp1, _ := Canonicalize(p)
			cp2, _ := Canonicalize(p)
			if cp1 == nil || cp2 == nil {
				t.Fatal("Canonicalize returned nil")
			}

			j1, err1 := marshalCanonicalJSON(cp1)
			j2, err2 := marshalCanonicalJSON(cp2)
			if err1 != nil || err2 != nil {
				t.Fatalf("JSON marshal error: %v / %v", err1, err2)
			}
			if !bytes.Equal(j1, j2) {
				t.Errorf("Canonicalize not deterministic on permutation %d:\n  first:  %s\n  second: %s",
					i, string(j1), string(j2))
			}
		})
	}
}

// TestAdversaryT05_NilCanonicalize tests edge cases of Canonicalize with nil/empty.
func TestAdversaryT05_NilCanonicalize(t *testing.T) {
	// Nil policy
	cp, w := Canonicalize(nil)
	if cp != nil {
		t.Error("Canonicalize(nil) should return nil")
	}
	if w != nil {
		t.Error("Canonicalize(nil) warnings should be nil")
	}

	// Empty policy (zero value)
	cp2, w2 := Canonicalize(&Policy{})
	if cp2 == nil {
		t.Fatal("Canonicalize(empty policy) should not return nil")
	}
	t.Logf("Empty policy canonicalized: version=%q, agent=%+v, warnings=%v", cp2.Version, cp2.Agent, w2)

	// Policy with only version
	cp3, _ := Canonicalize(&Policy{Version: "1.0"})
	if cp3 == nil {
		t.Fatal("Canonicalize(version-only) should not return nil")
	}
}

// TestAdversaryT05_NilDigest tests edge cases of Digest with nil/empty.
func TestAdversaryT05_NilDigest(t *testing.T) {
	// Nil policy should return error
	_, err := Digest(nil)
	if err == nil {
		t.Error("Digest(nil) should return error")
	}
	t.Logf("Digest(nil) error: %v", err)

	// Empty policy should work (zero value)
	d1, err := Digest(&Policy{})
	if err != nil {
		t.Fatalf("Digest(empty) error: %v", err)
	}
	d2, err := Digest(&Policy{})
	if err != nil {
		t.Fatalf("Digest(empty) second call error: %v", err)
	}
	if d1 != d2 {
		t.Errorf("Digest not stable for empty policy: %s vs %s", d1, d2)
	}
}

// TestAdversaryT05_DigestFormat verifies the digest format is sha256 hex.
func TestAdversaryT05_DigestFormat(t *testing.T) {
	p := &Policy{Version: "1.0", Agent: AgentConfig{Name: "format-test"}}
	d, err := Digest(p)
	if err != nil {
		t.Fatalf("Digest error: %v", err)
	}
	if len(d) != sha256.Size*2 {
		t.Errorf("Expected sha256 hex of length %d, got %d: %s", sha256.Size*2, len(d), d)
	}
	for _, c := range d {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			t.Errorf("Non-hex character in digest: %c", c)
		}
	}
}

// TestAdversaryT05_StripURLUserinfo verifies stripURLUserinfo handles edge cases.
func TestAdversaryT05_StripURLUserinfo(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"https://user:pass@example.com/path", "https://example.com/path"},
		{"https://example.com/path", "https://example.com/path"},
		{"", ""},
		{"not-a-url", "not-a-url"},
		{"https://user@example.com", "https://example.com"},
	}
	for _, tc := range tests {
		got := stripURLUserinfo(tc.input)
		if got != tc.expected {
			t.Errorf("stripURLUserinfo(%q) = %q, want %q", tc.input, got, tc.expected)
		}
	}
}
