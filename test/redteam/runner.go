package redteam

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"
)

// FixtureResult captures the outcome of a single red-team fixture.
type FixtureResult struct {
	ID          string        `json:"id"`
	Name        string        `json:"name"`
	Status      string        `json:"status"` // PASS, FAIL, SKIP
	Duration    time.Duration `json:"duration_ms"`
	Containment string        `json:"containment"` // BLOCKED, CONTAINED, REFUSED, LEAKED
	AuditVerdict string       `json:"audit_verdict"` // verified, missing, n/a
	Detail      string        `json:"detail"`
}

// Report is the full red-team containment report.
type Report struct {
	Timestamp   string          `json:"timestamp"`
	Suite       string          `json:"suite"`
	TotalFixtures int           `json:"total_fixtures"`
	Passed      int             `json:"passed"`
	Failed      int             `json:"failed"`
	Skipped     int             `json:"skipped"`
	Results     []FixtureResult `json:"results"`
	AuditSummary AuditSummary   `json:"audit_summary"`
}

// AuditSummary captures the signed audit-export verification.
type AuditSummary struct {
	ExportPath    string `json:"export_path"`
	RecordCount   int    `json:"record_count"`
	ChainValid    bool   `json:"chain_valid"`
	SignatureValid bool  `json:"signature_valid"`
	Verdict       string `json:"verdict"`
}

// ContainmentTable prints a 6-row containment table to the given writer.
func (r *Report) ContainmentTable() string {
	var b strings.Builder
	b.WriteString("\n")
	b.WriteString("╔══════════════════════════════════════════════════════════════════════════════╗\n")
	b.WriteString("║                    AGENTPAAS P1 RED-TEAM CONTAINMENT TABLE                   ║\n")
	b.WriteString("╠═══════╦══════════════════════════════════╦════════════╦═════════════╦═══════╣\n")
	b.WriteString("║  ID   ║ Fixture                           ║ Status     ║ Containment ║ Audit ║\n")
	b.WriteString("╠═══════╬══════════════════════════════════╬════════════╬═════════════╬═══════╣\n")

	for _, f := range r.Results {
		id := fmt.Sprintf("%-5s", f.ID)
		name := truncate(f.Name, 32)
		status := fmt.Sprintf("%-10s", f.Status)
		cont := fmt.Sprintf("%-11s", f.Containment)
		audit := fmt.Sprintf("%-5s", auditShort(f.AuditVerdict))
		fmt.Fprintf(&b, "║ %s ║ %-32s ║ %s ║ %s ║ %s ║\n", id, name, status, cont, audit)
	}

	b.WriteString("╠═══════╩══════════════════════════════════╩════════════╩═════════════╩═══════╣\n")
	summary := fmt.Sprintf("PASS:%d  FAIL:%d  SKIP:%d  TOTAL:%d", r.Passed, r.Failed, r.Skipped, r.TotalFixtures)
	fmt.Fprintf(&b, "║ %-76s ║\n", summary)
	b.WriteString("╚══════════════════════════════════════════════════════════════════════════════╝\n")

	if r.AuditSummary.Verdict != "" {
		fmt.Fprintf(&b, "\nAudit Export: %s\n", r.AuditSummary.ExportPath)
		fmt.Fprintf(&b, "  Records: %d  Chain: %v  Signature: %v  Verdict: %s\n",
			r.AuditSummary.RecordCount, r.AuditSummary.ChainValid,
			r.AuditSummary.SignatureValid, r.AuditSummary.Verdict)
	}

	return b.String()
}

// JSON returns the machine-readable JSON report.
func (r *Report) JSON() ([]byte, error) {
	return json.MarshalIndent(r, "", "  ")
}

// Verdict returns the overall gate verdict: "6/6 PASS" or "N/6 PASS (M FAIL)".
func (r *Report) Verdict() string {
	if r.Failed == 0 && r.Passed == r.TotalFixtures {
		return fmt.Sprintf("%d/%d PASS", r.Passed, r.TotalFixtures)
	}
	return fmt.Sprintf("%d/%d PASS (%d FAIL)", r.Passed, r.TotalFixtures, r.Failed)
}

// Fixture is a single red-team fixture.
type Fixture interface {
	ID() string
	Name() string
	Run() FixtureResult
}

// Runner executes red-team fixtures and produces a containment report.
type Runner struct {
	fixtures []Fixture
}

// NewRunner creates a Runner with the given fixtures.
func NewRunner(fixtures ...Fixture) *Runner {
	return &Runner{fixtures: fixtures}
}

// RunAll executes all fixtures and returns the report.
func (r *Runner) RunAll() *Report {
	report := &Report{
		Timestamp:     time.Now().UTC().Format(time.RFC3339),
		Suite:         "agentpaas-p1-redteam",
		TotalFixtures: len(r.fixtures),
		Results:       make([]FixtureResult, 0, len(r.fixtures)),
	}

	for _, f := range r.fixtures {
		result := f.Run()
		report.Results = append(report.Results, result)
		switch result.Status {
		case "PASS":
			report.Passed++
		case "FAIL":
			report.Failed++
		case "SKIP":
			report.Skipped++
		}
	}

	// Sort results by ID for deterministic output
	sort.Slice(report.Results, func(i, j int) bool {
		return report.Results[i].ID < report.Results[j].ID
	})

	return report
}

// RunFixture executes a single fixture by ID.
func (r *Runner) RunFixture(id string) (*Report, error) {
	for _, f := range r.fixtures {
		if f.ID() == id {
			result := f.Run()
			return &Report{
				Timestamp:     time.Now().UTC().Format(time.RFC3339),
				Suite:         "agentpaas-p1-redteam",
				TotalFixtures: 1,
				Passed:        boolToInt(result.Status == "PASS"),
				Failed:        boolToInt(result.Status == "FAIL"),
				Skipped:       boolToInt(result.Status == "SKIP"),
				Results:       []FixtureResult{result},
			}, nil
		}
	}
	return nil, fmt.Errorf("fixture %q not found", id)
}

// PrintReport writes the containment table and JSON report to stdout.
func PrintReport(report *Report, w *os.File) error {
	if _, err := fmt.Fprint(w, report.ContainmentTable()); err != nil {
		return fmt.Errorf("write table: %w", err)
	}
	if _, err := fmt.Fprintf(w, "\nVerdict: %s\n", report.Verdict()); err != nil {
		return fmt.Errorf("write verdict: %w", err)
	}

	jsonData, err := report.JSON()
	if err != nil {
		return fmt.Errorf("marshal report: %w", err)
	}
	if _, err := fmt.Fprintf(w, "\nMachine-readable report:\n%s\n", jsonData); err != nil {
		return fmt.Errorf("write json: %w", err)
	}
	return nil
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-1] + "…"
}

func auditShort(verdict string) string {
	switch verdict {
	case "verified":
		return "✓"
	case "missing":
		return "✗"
	default:
		return "—"
	}
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
