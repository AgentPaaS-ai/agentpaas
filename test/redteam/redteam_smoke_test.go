package redteam

import (
	"encoding/json"
	"os"
	"testing"
)

// TestRedteamSmoke is the P1 red-team smoke gate.
//
// It runs all 6 fixtures through the REAL pipeline:
//   - pack.BuildImage (real Docker image builds)
//   - runtime.DockerRuntime (real container/network topology)
//   - secrets.Broker + Gateway (real credential brokering)
//   - Block 11 operator handlers (real daemon methods)
//
// No synthetic harnesses, direct daemon shortcuts, or test-only
// enforcement paths. Gate: make block12-gate (wraps make redteam-smoke).
//
// Each fixture asserts: action BLOCKED/CONTAINED/REFUSED + the expected
// machine-readable result and audit event. Runner prints a 6-row
// containment table plus a signed audit-export verification summary.
//
// Suite target runtime <10 minutes on a developer laptop.
func TestRedteamSmoke(t *testing.T) {
	requireDocker(t)
	skipOnPlatform(t)

	fixtures := []Fixture{
		&egressFixture{},
		&credentialMisuseFixture{},
		&secretInvisibilityFixture{},
		&hostAccessFixture{},
		&resourceContainmentFixture{},
		&operatorInjectionFixture{},
	}

	runner := NewRunner(fixtures...)
	report := runner.RunAll()

	// Print the containment table to test output
	t.Log(report.ContainmentTable())
	t.Logf("Verdict: %s", report.Verdict())

	// Write machine-readable JSON report to a file for CI artifacts
	jsonData, err := report.JSON()
	if err != nil {
		t.Fatalf("marshal report: %v", err)
	}
	reportPath := os.TempDir() + "/agentpaas-redteam-report.json"
	if err := os.WriteFile(reportPath, jsonData, 0o644); err != nil {
		t.Fatalf("write report: %v", err)
	}
	t.Logf("Machine-readable report: %s", reportPath)

	// Gate: all 6 must PASS
	if report.Failed > 0 {
		t.Errorf("RED-TEAM GATE FAILED: %d/%d fixtures failed", report.Failed, report.TotalFixtures)
		for _, f := range report.Results {
			if f.Status == "FAIL" {
				t.Errorf("  FAIL %s: %s — %s", f.ID, f.Name, f.Detail)
			}
		}
	}

	if report.Passed != report.TotalFixtures {
		t.Errorf("RED-TEAM GATE INCOMPLETE: %d/%d passed (expected all)", report.Passed, report.TotalFixtures)
	}
}

// TestRedteamReportFormat verifies the report format is valid JSON
// with the expected structure (runs without Docker).
func TestRedteamReportFormat(t *testing.T) {
	report := &Report{
		Timestamp:     "2026-06-23T12:00:00Z",
		Suite:         "agentpaas-p1-redteam",
		TotalFixtures: 6,
		Passed:        6,
		Failed:        0,
		Skipped:       0,
		Results: []FixtureResult{
			{
				ID:           "T02",
				Name:         "Default-Deny Egress",
				Status:       "PASS",
				Containment:  "BLOCKED",
				AuditVerdict: "verified",
				Detail:       "raw IP dial and HTTPS to non-allowed domain blocked",
			},
		},
		AuditSummary: AuditSummary{
			ExportPath:     "/tmp/audit-export.jsonl",
			RecordCount:    42,
			ChainValid:     true,
			SignatureValid: true,
			Verdict:        "verified",
		},
	}

	data, err := report.JSON()
	if err != nil {
		t.Fatalf("marshal report: %v", err)
	}
	if !json.Valid(data) {
		t.Fatalf("report is not valid JSON: %s", data)
	}

	table := report.ContainmentTable()
	if !containsAny(table, "CONTAINMENT TABLE", "PASS", "Default-Deny") {
		t.Fatalf("containment table missing expected content: %s", table)
	}

	verdict := report.Verdict()
	if verdict != "6/6 PASS" {
		t.Fatalf("verdict = %q, want %q", verdict, "6/6 PASS")
	}
}
