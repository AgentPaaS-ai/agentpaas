package golden

import (
	"os"
	"testing"
)

// TestGoldenSuite is the entry point for the golden dataset regression suite.
//
// Usage:
//   make golden-fast     # run fast tier only (every commit)
//   make golden-slow     # run slow tier (PRs)
//   make golden-docker   # run docker tier (main merge)
//   make golden-eval     # run all tiers
//
// Environment:
//   AGENTPAAS_DOCKER_TESTS=1  — enables docker-tier tasks
//   GOLDEN_K=N                — override repetition count (default: 3)
//   GOLDEN_TIER=fast|slow|docker|all
//
// The suite measures pass^k (all k runs succeed) per task and gates on
// the thresholds defined in golden_tasks.yaml.
func TestGoldenSuite(t *testing.T) {
	// Load dataset
	datasetPath := "golden_tasks.yaml"
	if _, err := os.Stat(datasetPath); os.IsNotExist(err) {
		// Try relative to test file
		datasetPath = "../../test/golden/golden_tasks.yaml"
	}
	if _, err := os.Stat(datasetPath); os.IsNotExist(err) {
		// Try from repo root
		datasetPath = "test/golden/golden_tasks.yaml"
	}

	// Find the dataset — use the FindDataset function that tries multiple paths
	dataset, err := FindDataset(datasetPath)
	if err != nil {
		// Try a few more paths
		for _, p := range []string{
			"golden_tasks.yaml",
			"test/golden/golden_tasks.yaml",
			"../../test/golden/golden_tasks.yaml",
		} {
			dataset, err = FindDataset(p)
			if err == nil {
				break
			}
		}
	}
	if err != nil {
		t.Fatalf("could not load golden_tasks.yaml: %v", err)
	}

	// Determine tier
	tier := os.Getenv("GOLDEN_TIER")
	if tier == "" {
		tier = "all"
	}

	// Determine k
	k := 3
	if envK := os.Getenv("GOLDEN_K"); envK != "" {
		if n, err := parseIntSafe(envK); err == nil && n > 0 {
			k = n
		}
	}

	// Create runner
	runner := NewRunner(dataset, tier, k)

	// Run all tasks
	report := runner.RunAll()

	// Print table
	t.Log(report.Table())
	t.Logf("Verdict: %s", report.Verdict())

	// Write machine-readable report
	jsonData, _ := report.JSON()
	reportPath := os.TempDir() + "/agentpaas-golden-report.json"
	os.WriteFile(reportPath, jsonData, 0o644)
	t.Logf("Machine-readable report: %s", reportPath)

	// Gate: fail if any task didn't pass all k trials
	if report.FailedTasks > 0 {
		t.Errorf("GOLDEN GATE FAILED: %d/%d tasks did not pass all %d trials",
			report.FailedTasks, report.TotalTasks, k)
		for _, r := range report.Results {
			if r.K > 0 && !r.AllPassed {
				t.Errorf("  %s (%s): pass@1=%.0f%% pass^k=%.0f%% — %s",
					r.ID, r.Name, r.PassAt1*100, r.PassK*100, r.Detail)
			}
		}
	}
}

// parseIntSafe parses an integer, returning an error if invalid.
func parseIntSafe(s string) (int, error) {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, errInvalidK
		}
		n = n*10 + int(c-'0')
	}
	return n, nil
}

var errInvalidK = &invalidKError{}

type invalidKError struct{}

func (e *invalidKError) Error() string { return "invalid GOLDEN_K value" }
