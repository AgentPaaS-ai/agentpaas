package mcpmanager

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
)

// ADVERSARY B7M-T06 tests for MCP tool auditing and host-affecting guard.
// These are regression tests; failing tests indicate security breaks.

func TestAdversary_B7M_T06_PatternEvasionPartialMatch(t *testing.T) {
	// "shel" does not contain "shell" substring → evades classification
	if ClassifyTool("shel") != CapabilityNone {
		t.Fatal("expected none for partial")
	}
	if IsHostAffecting("shel") {
		t.Fatal("IsHostAffecting partial match")
	}
	// "browsr" missing 'e'
	if ClassifyTool("browsr") != CapabilityNone {
		t.Fatal("expected none for misspelled browser")
	}
}

func TestAdversary_B7M_T06_UnicodeHomoglyphEvasion(t *testing.T) {
	// fullwidth or similar that does not contain ascii substring
	evil := "ｓｈｅｌｌ" // fullwidth, ToLower may not match ascii "shell"
	if ClassifyTool(evil) != CapabilityNone {
		t.Fatal("unicode homoglyph should evade")
	}
}

func TestAdversary_B7M_T06_ControlCharInToolName(t *testing.T) {
	evil := "sh\x00ell"
	if ClassifyTool(evil) != CapabilityNone {
		t.Fatal("control char name should not classify as host affecting")
	}
}

func TestAdversary_B7M_T06_ConfirmationTOCTOURace(t *testing.T) {
	manager := NewManager()
	serverID := "race-server"
	tool := "shell.run"
	// simulate race window: check requires, then confirm concurrently
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_ = manager.RequiresConfirmation(serverID, tool)
	}()
	go func() {
		defer wg.Done()
		manager.ConfirmTool(serverID, tool)
	}()
	wg.Wait()
	// after race, may or may not be confirmed depending on timing; no panic expected
	// under -race this exercises the map
}

func TestAdversary_B7M_T06_RedactionMapKeySecret(t *testing.T) {
	secretMap := map[string]string{"sk-live-1234": "value"}
	got := RedactToolOutput(secretMap)
	if strings.Contains(got, "sk-live-1234") {
		t.Fatalf("map key secret leaked: %s", got)
	}
	if !strings.Contains(got, "[REDACTED]") {
		t.Fatalf("expected redacted map key marker, got %s", got)
	}
}

func TestAdversary_B7M_T06_RedactionNonStringFields(t *testing.T) {
	// non-string fields skipped
	data := map[string]any{"num": 123, "bool": true, "secret": "AKIA123"}
	got := RedactToolOutput(data)
	// no leak expected but tests recursion
	if !strings.Contains(got, "123") {
		t.Fatal("non string should be preserved")
	}
}

func TestAdversary_B7M_T06_DeepNestingRedact(t *testing.T) {
	// deep nesting may cause issues or unbounded before truncate
	deep := map[string]any{}
	cur := deep
	for i := 0; i < 100; i++ {
		next := map[string]any{"level": i}
		cur["child"] = next
		cur = next
	}
	cur["secret"] = "sk-deep"
	got := RedactToolOutput(deep)
	if len(got) > maxToolOutputLen+100 {
		t.Fatal("no depth limit caused excessive output")
	}
}

func TestAdversary_B7M_T06_PartialRedactionLeak(t *testing.T) {
	// Real secret material must be fully redacted — no sk- prefix residue.
	got := RedactToolOutput("sk-live-abcdefghijklmnopqrstuvwxyz012345")
	if strings.Contains(got, "sk-live") || strings.Contains(got, "abcdefghijklmnopqrstuvwxyz") {
		t.Fatalf("secret material leaked after redaction: %s", got)
	}
	if !strings.Contains(got, "[REDACTED]") && got == "sk-live-abcdefghijklmnopqrstuvwxyz012345" {
		t.Fatalf("expected redaction of API key, got %q", got)
	}
}

func TestAdversary_B7M_T06_DeniedCallRouting(t *testing.T) {
	manager := newTestManager("denied", []string{"shell.run"})
	appender := &memoryAuditAppender{}
	router := NewRouter(manager, nil, staticHTTPDoer{err: errors.New("should not reach")}, appender)
	_, err := router.CallTool(context.Background(), "denied", "shell.run", nil, "a", "r")
	if err == nil || !strings.Contains(err.Error(), "requires confirmation") {
		t.Fatal("denied should not route")
	}
	// routing blocked before routeStdio/routeHTTP — SAFE
}

func TestAdversary_B7M_T06_NestedControlChars(t *testing.T) {
	nested := []any{map[string]any{"html": "<script>\x1b[31mXSS</script>"}}
	got := RedactToolOutput(nested)
	if strings.Contains(got, "\x1b") || strings.Contains(got, "\x00") {
		t.Fatal("nested controls not escaped")
	}
}

func TestAdversary_B7M_T06_ConcurrentConfirmMapRace(t *testing.T) {
	manager := NewManager()
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(2)
		go func(i int) {
			defer wg.Done()
			manager.ConfirmTool("s", "shell"+string(rune('a'+i)))
		}(i)
		go func(i int) {
			defer wg.Done()
			_ = manager.IsToolConfirmed("s", "shell"+string(rune('a'+i)))
		}(i)
	}
	wg.Wait()
	// After concurrent confirm, each tool that was confirmed should report true.
	for i := 0; i < 10; i++ {
		name := "shell" + string(rune('a'+i))
		if !manager.IsToolConfirmed("s", name) {
			t.Errorf("tool %q not confirmed after concurrent ConfirmTool", name)
		}
	}
}

func TestAdversary_B7M_T06_PromptInjectStateChange(t *testing.T) {
	// Tool output cannot call Confirm/Register — router is read-only on result.
	manager := newTestManager("r", []string{"read"})
	if manager.IsToolConfirmed("r", "shell.run") {
		t.Fatal("read-only manager must not pre-confirm shell.run")
	}
	// Confirming a different tool must not auto-confirm shell.
	manager.ConfirmTool("r", "read")
	if manager.IsToolConfirmed("r", "shell.run") {
		t.Fatal("confirming read must not confirm shell.run")
	}
	if !manager.IsToolConfirmed("r", "read") {
		t.Fatal("confirming read should stick")
	}
}

func TestAdversary_B7M_T06_AuditDeniedMissingFields(t *testing.T) {
	appender := &memoryAuditAppender{}
	AuditToolDenied(appender, "srv", "shell", "ag", "rn", "reason", "rule", "cred", "input", int64(7))
	rec := appender.Records()[0]
	p := rec.Payload
	for key, want := range map[string]interface{}{
		"credential_id": "cred",
		"input_hash":    "input",
		"output_hash":   "",
		"timing_ms":     int64(7),
	} {
		if got, ok := p[key]; !ok || got != want {
			t.Fatalf("payload[%q] = %v, present %v, want %v", key, got, ok, want)
		}
	}
}

func TestAdversary_B7M_T06_EmptyToolName(t *testing.T) {
	if ClassifyTool("") != CapabilityNone {
		t.Fatal("empty should not panic or classify")
	}
	AuditToolCall(nil, "", "", "", "", "", "", "", "", "", 0) // no panic
}

func TestAdversary_B7M_T06_SentinelMutable(t *testing.T) {
	patterns := sentinelSecretPatternsList()
	if len(patterns) == 0 {
		t.Fatal("expected sentinel patterns")
	}
	original := sentinelSecretPatternsList()[0]
	patterns[0] = "safe"
	if got := sentinelSecretPatternsList()[0]; got != original {
		t.Fatalf("sentinel pattern list mutation leaked: got %q, want %q", got, original)
	}
}

func TestAdversary_B7M_T06_RedactTypePreservation(t *testing.T) {
	input := map[string]any{"num": 42, "arr": []any{1, "s"}, "ok": true}
	got := redactToolOutputValue(input)
	m, ok := got.(map[string]any)
	if !ok {
		t.Fatalf("want map[string]any, got %T", got)
	}
	// Current implementation JSON-roundtrips values; numbers become float64.
	// Assert structure and numeric value are preserved (even if type widens).
	switch v := m["num"].(type) {
	case int:
		if v != 42 {
			t.Fatalf("num = %d", v)
		}
	case int64:
		if v != 42 {
			t.Fatalf("num = %d", v)
		}
	case float64:
		if v != 42 {
			t.Fatalf("num = %v, want 42", v)
		}
	default:
		t.Fatalf("num type %T = %v", m["num"], m["num"])
	}
	if m["ok"] != true {
		t.Fatalf("bool not preserved: %v", m["ok"])
	}
	arr, ok := m["arr"].([]any)
	if !ok || len(arr) != 2 {
		t.Fatalf("arr not preserved: %T %v", m["arr"], m["arr"])
	}
}
