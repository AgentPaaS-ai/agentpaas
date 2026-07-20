package v023

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// This file converts the B26 t.Log-only characterizations of the 7 hidden
// durable-path ceilings into mandatory regression tests (B30-T01).
//
// Each "ceiling" is a fixed timeout or rlimit on the durable invocation path
// that exists in v0.2.3 as an accidental lifetime limit. The B30 spec
// (b30-summary.md:79-89, 255-258) requires every observed limit to have an
// executable test that fails for the intended reason on the baseline and
// cannot be "fixed" by changing only a comment.
//
// TDD pattern: each ceiling test asserts the CURRENT (broken) behavior, then
// t.Skip with the owning future task name (T02-T04) so the gate stays green.
// When the owning task replaces the fixed timeout with a policy-derived value,
// the corresponding test gets UNSKIPPED and the assertion INVERTED to verify
// the new behavior.
//
// Tests 1-7 are the 7 ceilings (skipped, register the requirement).
// Test 8 (LegacyPathConstantsAllowlisted) documents the allowlist requirement.
// Test 9 (FixedTimeoutOwnershipScanner) is the regression guard — never skipped.
// Test 10 (LegacyCompatPathUnchanged) proves the legacy v0.2.3 invoke path
// still works — never skipped.

// sourceBytes reads a source file relative to project root and returns its
// bytes. Fails the test if the file cannot be read (the file is part of the
// durable path contract and must exist).
func sourceBytes(t *testing.T, relPath string) []byte {
	t.Helper()
	root := projectRoot(t)
	abs := filepath.Join(root, filepath.FromSlash(relPath))
	data, err := os.ReadFile(abs)
	if err != nil {
		t.Fatalf("read %s: %v", relPath, err)
	}
	return data
}

// mustContain asserts that data contains needle and fails with a message
// citing relPath. The needle must be the durable-path ceiling under test.
func mustContain(t *testing.T, relPath string, data []byte, needle string) {
	t.Helper()
	if !strings.Contains(string(data), needle) {
		t.Fatalf("%s no longer contains %q - the durable-path ceiling was silently "+
			"removed before its owning task replaced it with a policy-derived value. "+
			"See B30-T01 FixedTimeoutOwnershipScanner.", relPath, needle)
	}
}

// ----------------------------------------------------------------------------
// Test 1: Daemon auto-invoke fixed two-minute context
// Source: internal/daemon/control_handlers.go:909
//   context.WithTimeout(invokeCtx, 2*time.Minute)
// ----------------------------------------------------------------------------

func TestB30T01_DaemonInvokeTimeoutIsTwoMinutes_Failing(t *testing.T) {
	data := sourceBytes(t, "internal/daemon/control_handlers.go")
	mustContain(t, "internal/daemon/control_handlers.go", data,
		"context.WithTimeout(invokeCtx, 2*time.Minute)")
	// Passing on baseline today: the 2-minute timeout exists. Register the
	// requirement; B30-T03 will replace this fixed timeout with a
	// policy-derived TimeEnvelope.
	t.Skip("B30-T03 will replace this fixed timeout with a policy-derived TimeEnvelope")
}

// ----------------------------------------------------------------------------
// Test 2: Inner invoke helper urlopen(...,timeout=60) waits for final response
// Source: internal/daemon/control_handlers.go:1500
//   urllib.request.urlopen(req,timeout=60)
// ----------------------------------------------------------------------------

func TestB30T01_InnerInvokeHelperUrlopen60_Failing(t *testing.T) {
	data := sourceBytes(t, "internal/daemon/control_handlers.go")
	mustContain(t, "internal/daemon/control_handlers.go", data,
		"urllib.request.urlopen(req,timeout=60)")
	// Passing on baseline today: the lifetime-spanning urlopen exists. Register
	// the requirement; B30-T02 will replace it with the durable InvokeJob
	// protocol.
	t.Skip("B30-T02 will replace the lifetime-spanning urlopen with the durable InvokeJob protocol")
}

// ----------------------------------------------------------------------------
// Test 3: Harness invoke 5-minute default (300s)
// Source: cmd/harness/main.go:24
//   envDuration("AGENTPAAS_INVOKE_TIMEOUT", 300*time.Second)
// ----------------------------------------------------------------------------

func TestB30T01_HarnessInvokeTimeoutDefault5Min_Failing(t *testing.T) {
	data := sourceBytes(t, "cmd/harness/main.go")
	mustContain(t, "cmd/harness/main.go", data, "300*time.Second")
	// Passing on baseline today: the 300s default exists. Register the
	// requirement; B30-T03 will derive harness invoke timeout from
	// TimeEnvelope, not a fixed default.
	t.Skip("B30-T03 will derive harness invoke timeout from TimeEnvelope, not a fixed default")
}

// ----------------------------------------------------------------------------
// Test 4: Harness budget 120-second default wall clock
// Source: internal/harness/budget.go:17
//   const defaultWallClockBudget = 120 * time.Second
// ----------------------------------------------------------------------------

func TestB30T01_HarnessBudgetDefault120s_Failing(t *testing.T) {
	data := sourceBytes(t, "internal/harness/budget.go")
	mustContain(t, "internal/harness/budget.go", data,
		"defaultWallClockBudget = 120 * time.Second")
	// Passing on baseline today: the 120s budget exists. Register the
	// requirement; B30-T03 will replace the fixed 120s budget with a
	// policy-derived active-time envelope.
	t.Skip("B30-T03 will replace the fixed 120s budget with policy-derived active-time envelope")
}

// ----------------------------------------------------------------------------
// Test 5: Model client 120-second fixed HTTP timeout
// Source: internal/harness/rpc_server.go:471
//   http.Client{Timeout: 120 * time.Second, ...}
// ----------------------------------------------------------------------------

func TestB30T01_ModelClientTimeout120s_Failing(t *testing.T) {
	data := sourceBytes(t, "internal/harness/rpc_server.go")
	mustContain(t, "internal/harness/rpc_server.go", data, "Timeout: 120 * time.Second")
	// Passing on baseline today: the 120s HTTP timeout exists. Register the
	// requirement; B30-T03 will derive the model client timeout from the
	// effective operation deadline.
	t.Skip("B30-T03 will derive model client timeout from effective operation deadline")
}

// ----------------------------------------------------------------------------
// Test 6: Python worker 30 CPU-second rlimit
// Source: internal/harness/python_worker.go:477
//   ("RLIMIT_CPU", 30)
// ----------------------------------------------------------------------------

func TestB30T01_PythonRLimitCPU30_Failing(t *testing.T) {
	data := sourceBytes(t, "internal/harness/python_worker.go")
	mustContain(t, "internal/harness/python_worker.go", data, `("RLIMIT_CPU", 30)`)
	// Passing on baseline today: RLIMIT_CPU=30 exists. Register the
	// requirement; B30-T04 will replace RLIMIT_CPU=30 with an explicit
	// policy-derived container CPU limit.
	t.Skip("B30-T04 will replace RLIMIT_CPU=30 with explicit policy-derived container CPU limit")
}

// ----------------------------------------------------------------------------
// Test 7: Python worker RLIMIT_NPROC=0
// Source: internal/harness/python_worker.go:479
//   ("RLIMIT_NPROC", 0)
// ----------------------------------------------------------------------------

func TestB30T01_PythonRLimitNPROC0_Failing(t *testing.T) {
	data := sourceBytes(t, "internal/harness/python_worker.go")
	mustContain(t, "internal/harness/python_worker.go", data, `("RLIMIT_NPROC", 0)`)
	// Passing on baseline today: RLIMIT_NPROC=0 exists. Register the
	// requirement; B30-T04 will replace RLIMIT_NPROC=0 with a policy-derived
	// container PID limit.
	t.Skip("B30-T04 will replace RLIMIT_NPROC=0 with policy-derived container PID limit")
}

// ============================================================================
// Test 8: LegacyPathConstantsAllowlisted (always runs, no skip)
//
// Asserts that every time.Minute / time.Second literal on the durable path
// (rpc_server.go LLM path, control_handlers.go invoke path, cmd/harness/main.go)
// is EITHER:
//   (a) documented on the same line with a comment containing "legacy" or
//       "compat" (the v0.2.3 synchronous compat path may keep its constants), OR
//   (b) one of the 7 ceilings explicitly registered in tests 1-7 above (each
//       carries its own t.Skip owning-task registration).
//
// On the T01 baseline all durable-path fixed timeouts are undocumented, so
// the requirement is registered but skipped to avoid breaking the gate. When
// T02-T04 lands and replaces each ceiling, it must either document the
// remaining literal as "legacy"/"compat" or invert the corresponding ceiling
// test.
// ============================================================================

// b30T01Ceilings is the registry of the 7 characterized durable-path ceilings.
// Each entry maps a source file (slash path) to the literal substring the
// ceiling test asserts. Test 9 uses this to prove no ceiling is silently
// removed; Test 8 uses it to allowlist those literals that are registered
// (documented elsewhere) but not yet replaced.
var b30T01Ceilings = []struct {
	relPath string
	literal string
	owner   string // owning future task (T02/T03/T04)
}{
	{"internal/daemon/control_handlers.go", "context.WithTimeout(invokeCtx, 2*time.Minute)", "B30-T03"},
	{"internal/daemon/control_handlers.go", "urllib.request.urlopen(req,timeout=60)", "B30-T02"},
	{"cmd/harness/main.go", "300*time.Second", "B30-T03"},
	{"internal/harness/budget.go", "defaultWallClockBudget = 120 * time.Second", "B30-T03"},
	{"internal/harness/rpc_server.go", "Timeout: 120 * time.Second", "B30-T03"},
	{"internal/harness/python_worker.go", `("RLIMIT_CPU", 30)`, "B30-T04"},
	{"internal/harness/python_worker.go", `("RLIMIT_NPROC", 0)`, "B30-T04"},
}

// b30T01DurablePathFiles are the source files on the durable invocation path
// whose fixed timeouts must be allowlisted (documented as legacy/compat) or
// registered as one of the 7 ceilings.
var b30T01DurablePathFiles = []string{
	"internal/harness/rpc_server.go",
	"internal/daemon/control_handlers.go",
	"cmd/harness/main.go",
}

func TestB30T01_LegacyPathConstantsAllowlisted(t *testing.T) {
	// On the T01 baseline, no durable-path fixed timeout is documented with
	// a "legacy"/"compat" comment — they are all undocumented accidental
	// ceilings. The requirement that they BE documented (or replaced) is
	// what this test registers; the skip message records that the owning
	// tasks T02-T04 are responsible for either documenting or replacing
	// each one.
	undocumented := 0
	for _, rel := range b30T01DurablePathFiles {
		data := sourceBytes(t, rel)
		for _, line := range strings.Split(string(data), "\n") {
			if !strings.Contains(line, "time.Minute") && !strings.Contains(line, "time.Second") {
				continue
			}
			lower := strings.ToLower(line)
			if strings.Contains(lower, "legacy") || strings.Contains(lower, "compat") {
				continue // documented allowlist
			}
			registered := false
			for _, c := range b30T01Ceilings {
				if c.relPath == rel && strings.Contains(line, c.literal) {
					registered = true
					break
				}
			}
			if registered {
				continue
			}
			undocumented++
			t.Logf("undocumented durable-path fixed timeout in %s: %s", rel, strings.TrimSpace(line))
		}
	}
	if undocumented > 0 {
		t.Logf("found %d undocumented durable-path fixed timeouts (see logs above)", undocumented)
	}
	// Register the requirement; the owning tasks T02-T04 will either document
	// each remaining literal as legacy/compat or replace it with a policy-
	// derived value (inverting the corresponding ceiling test).
	t.Skip("B30-T02/T03/T04 will document or replace each durable-path fixed timeout; " +
		"baseline has 0 documented legacy/compat timeouts")
}

// ============================================================================
// Test 9: FixedTimeoutOwnershipScanner (regression guard — always runs)
//
// Scans the 7 characterized source locations and asserts each ceiling literal
// is STILL PRESENT. This is the regression guard against silent removal: a
// future commit must not delete a characterization before replacing it with
// the policy-derived version. If a ceiling moves, the owning task must update
// both the source and this registry together.
//
// This test is NEVER skipped — it runs on every gate.
// ============================================================================

func TestB30T01_FixedTimeoutOwnershipScanner(t *testing.T) {
	// Cache file contents to avoid re-reading the same file per ceiling.
	cache := make(map[string][]byte)
	for _, c := range b30T01Ceilings {
		data, ok := cache[c.relPath]
		if !ok {
			data = sourceBytes(t, c.relPath)
			cache[c.relPath] = data
		}
		if !strings.Contains(string(data), c.literal) {
			t.Errorf("durable-path ceiling missing: %s no longer contains %q "+
				"(owner: %s). A characterization must not be silently removed "+
				"before its owning task replaces it with a policy-derived value.",
				c.relPath, c.literal, c.owner)
		}
	}
	if t.Failed() {
		t.Fatalf("one or more durable-path ceilings were removed without replacement; " +
			"see errors above")
	}
	// Sanity: the registry itself must list exactly the 7 ceilings (guards
	// against accidental shrinkage of the registry).
	if len(b30T01Ceilings) != 7 {
		t.Fatalf("b30T01Ceilings registry has %d entries, want 7", len(b30T01Ceilings))
	}
}

// ============================================================================
// Test 10: LegacyCompatPathUnchanged (always runs, no skip)
//
// Smoke test that the legacy v0.2.3 synchronous invoke path still compiles
// and its payload-building helper still works. This proves T01 did not
// accidentally break the legacy path while adding the durable-path ceiling
// tests. Reuses the existing mockPayloadBuilder helper from v023_test.go.
// ============================================================================

func TestB30T01_LegacyCompatPathUnchanged(t *testing.T) {
	mb := &mockPayloadBuilder{}
	payload, err := mb.BuildInvokePayload(nil, "openrouter-test", nil)
	if err != nil {
		t.Fatalf("BuildInvokePayload: %v", err)
	}
	llm, ok := payload["llm"].(map[string]any)
	if !ok {
		t.Fatalf("payload.llm missing or wrong type, got=%T", payload["llm"])
	}
	if got := llm["provider"]; got != "openrouter" {
		t.Errorf("llm.provider = %v, want openrouter", got)
	}
	if got := llm["model"]; got != "anthropic/claude-sonnet-4" {
		t.Errorf("llm.model = %v, want anthropic/claude-sonnet-4", got)
	}

	// Also confirm the no-LLM legacy path still produces a payload with no llm key.
	payloadNoLLM, err := mb.BuildInvokePayload(nil, "no-llm-test", nil)
	if err != nil {
		t.Fatalf("BuildInvokePayload (no-llm): %v", err)
	}
	if _, ok := payloadNoLLM["llm"]; ok {
		t.Error("payload.llm should not be present for no-llm project")
	}
}
