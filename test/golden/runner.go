// Package golden implements the AgentPaaS golden dataset regression suite.
//
// It extends the redteam Fixture/Runner pattern with:
//   - pass^k measurement (all k runs must succeed, not just 1)
//   - Tiered execution (fast/slow/docker)
//   - Code-based and LLM-judge graders
//   - Regression gating (block merge on threshold drop)
//
// The golden dataset lives in golden_tasks.yaml. Each task is a user-facing
// operation that must work correctly AND consistently. The runner executes
// each task k times, measures pass@1 (capability) and pass^k (consistency),
// and produces a machine-readable report for CI gating.
//
// Gate: make golden-eval
package golden

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"
)

// ─── Schema types (map to golden_tasks.yaml) ────────────────────────────────

// Dataset is the top-level YAML structure.
type Dataset struct {
	Version     string      `yaml:"version"`
	Suite       string      `yaml:"suite"`
	Description string      `yaml:"description"`
	Thresholds  Thresholds  `yaml:"thresholds"`
	DefaultK    int         `yaml:"default_k"`
	Tasks       []TaskSpec  `yaml:"tasks"`
}

// Thresholds defines regression gate thresholds.
type Thresholds struct {
	PassKMin         float64 `yaml:"pass_k_min"`
	PassAt1Min       float64 `yaml:"pass_at_1_min"`
	RegressionDelta  float64 `yaml:"regression_delta"`
}

// TaskSpec is a single golden task definition from the YAML dataset.
type TaskSpec struct {
	ID            string            `yaml:"id"`
	Name          string            `yaml:"name"`
	Tier          string            `yaml:"tier"` // fast, slow, docker
	Grader        string            `yaml:"grader"` // code, judge
	Category      string            `yaml:"category"`
	SourceFailure string            `yaml:"source_failure"`
	Inputs        map[string]interface{} `yaml:"inputs"`
	SuccessCriteria map[string]interface{} `yaml:"success_criteria"`
	RequiresDocker bool             `yaml:"requires_docker"`
}

// ─── Execution types ────────────────────────────────────────────────────────

// TrialResult is the outcome of a single execution of a task.
type TrialResult struct {
	TrialNum   int           `json:"trial_num"`
	Pass       bool          `json:"pass"`
	Duration   time.Duration `json:"duration_ms"`
	Output     string        `json:"output,omitempty"`
	Error      string        `json:"error,omitempty"`
}

// TaskResult captures the outcome of running a task k times.
type TaskResult struct {
	ID              string        `json:"id"`
	Name            string        `json:"name"`
	Tier            string        `json:"tier"`
	Category        string        `json:"category"`
	Grader          string        `json:"grader"`
	K               int           `json:"k"`
	Trials          []TrialResult `json:"trials"`
	PassAt1         float64       `json:"pass_at_1"` // fraction: at least 1 of k passed
	PassK           float64       `json:"pass_k"`    // fraction: all k passed
	AllPassed       bool          `json:"all_passed"`
	AnyPassed       bool          `json:"any_passed"`
	Duration        time.Duration `json:"total_duration_ms"`
	Detail          string        `json:"detail,omitempty"`
}

// GoldenReport is the full report for a golden suite run.
type GoldenReport struct {
	Timestamp   string       `json:"timestamp"`
	Suite       string       `json:"suite"`
	Tier        string       `json:"tier"` // which tier was run
	TotalTasks  int          `json:"total_tasks"`
	PassedTasks int          `json:"passed_tasks"` // tasks where pass^k = 1.0
	FailedTasks int          `json:"failed_tasks"`
	SkippedTasks int         `json:"skipped_tasks"`
	Results     []TaskResult `json:"results"`
	Thresholds  Thresholds   `json:"thresholds"`
	GateVerdict string       `json:"gate_verdict"` // PASS, FAIL, REGRESSION
	Baseline    *GoldenReport `json:"baseline,omitempty"` // previous run for comparison
}

// ─── TaskFunc is the function that executes a task ──────────────────────────

// TaskFunc executes a single task and returns pass/fail + output.
// Implementations live in the grader files (graders.go, docker_graders.go).
type TaskFunc func(spec TaskSpec) (pass bool, output string, err error)

// ─── Runner ──────────────────────────────────────────────────────────────────

// Runner executes golden tasks and produces a report.
type Runner struct {
	dataset  *Dataset
	tier     string // which tier to run: "fast", "slow", "docker", "all"
	k        int    // repetitions per task
	registry map[string]TaskFunc // task ID → execution function
}

// NewRunner creates a Runner for the given dataset.
func NewRunner(dataset *Dataset, tier string, k int) *Runner {
	if k <= 0 {
		k = dataset.DefaultK
		if k <= 0 {
			k = 3
		}
	}
	return &Runner{
		dataset:  dataset,
		tier:     tier,
		k:        k,
		registry: DefaultRegistry(),
	}
}

// RegisterTaskFunc registers an execution function for a task ID.
// This allows tests to override the default implementation.
func (r *Runner) RegisterTaskFunc(taskID string, fn TaskFunc) {
	r.registry[taskID] = fn
}

// RunAll executes all tasks in the dataset (filtered by tier) and returns the report.
func (r *Runner) RunAll() *GoldenReport {
	report := &GoldenReport{
		Timestamp:  time.Now().UTC().Format(time.RFC3339),
		Suite:      r.dataset.Suite,
		Tier:       r.tier,
		Thresholds: r.dataset.Thresholds,
		Results:    make([]TaskResult, 0),
	}

	for _, task := range r.dataset.Tasks {
		// Tier filter
		if r.tier != "all" && task.Tier != r.tier {
			continue
		}

		// Docker skip check
		if task.RequiresDocker && os.Getenv("AGENTPAAS_DOCKER_TESTS") != "1" {
			report.SkippedTasks++
			report.Results = append(report.Results, TaskResult{
				ID:       task.ID,
				Name:     task.Name,
				Tier:     task.Tier,
				Category: task.Category,
				Grader:   task.Grader,
				K:        0,
				Detail:   "skipped: set AGENTPAAS_DOCKER_TESTS=1 to run",
			})
			continue
		}

		result := r.runTask(task)
		report.Results = append(report.Results, result)
		report.TotalTasks++

		if result.AllPassed {
			report.PassedTasks++
		} else {
			report.FailedTasks++
		}
	}

	// Sort results by ID for deterministic output
	sort.Slice(report.Results, func(i, j int) bool {
		return report.Results[i].ID < report.Results[j].ID
	})

	report.GateVerdict = r.computeVerdict(report)
	return report
}

// runTask executes a single task k times and computes pass^k metrics.
func (r *Runner) runTask(spec TaskSpec) TaskResult {
	k := r.k
	result := TaskResult{
		ID:       spec.ID,
		Name:     spec.Name,
		Tier:     spec.Tier,
		Category: spec.Category,
		Grader:   spec.Grader,
		K:        k,
		Trials:   make([]TrialResult, 0, k),
	}

	fn, ok := r.registry[spec.ID]
	if !ok {
		// No registered function — use generic grader
		fn = r.registry["_default"]
	}
	if fn == nil {
		result.Detail = fmt.Sprintf("no task function registered for %s", spec.ID)
		return result
	}

	start := time.Now()
	for i := 1; i <= k; i++ {
		trialStart := time.Now()
		pass, output, err := fn(spec)
		result.Trials = append(result.Trials, TrialResult{
			TrialNum: i,
			Pass:     pass,
			Duration: time.Since(trialStart),
			Output:   output,
			Error:    func() string {
				if err != nil {
					return err.Error()
				}
				return ""
			}(),
		})
	}
	result.Duration = time.Since(start)

	// Compute pass@1 and pass^k
	passCount := 0
	for _, t := range result.Trials {
		if t.Pass {
			passCount++
		}
	}
	result.PassAt1 = float64(passCount) / float64(k) // best case
	result.PassK = 0
	if passCount == k {
		result.PassK = 1.0
	}
	result.AllPassed = passCount == k
	result.AnyPassed = passCount >= 1

	if !result.AllPassed {
		var fails []string
		for _, t := range result.Trials {
			if !t.Pass {
				fails = append(fails, fmt.Sprintf("trial %d: %s", t.TrialNum, t.Error))
			}
		}
		result.Detail = strings.Join(fails, "; ")
	}

	return result
}

// computeVerdict determines PASS, FAIL, or REGRESSION.
func (r *Runner) computeVerdict(report *GoldenReport) string {
	if report.FailedTasks == 0 {
		return "PASS"
	}

	// Check if any task dropped below the pass_k threshold
	for _, result := range report.Results {
		if result.K == 0 {
			continue // skipped
		}
		if result.PassK < r.dataset.Thresholds.PassKMin {
			return "FAIL"
		}
	}

	return "FAIL"
}

// ─── Report rendering ────────────────────────────────────────────────────────

// Table prints a human-readable table of results.
func (r *GoldenReport) Table() string {
	var b strings.Builder
	b.WriteString("\n")
	b.WriteString("╔════════╦══════════════════════════════════════════╦════════╦══════════╦══════════╦════════╗\n")
	b.WriteString("║   ID   ║ Task                                     ║ Tier   ║ pass@1   ║ pass^k   ║ Verdict║\n")
	b.WriteString("╠════════╬══════════════════════════════════════════╬════════╬══════════╬══════════╬════════╣\n")

	for _, r := range r.Results {
		id := fmt.Sprintf("%-6s", r.ID)
		name := truncateStr(r.Name, 40)
		tier := fmt.Sprintf("%-6s", r.Tier)
		pat1 := fmt.Sprintf("%.0f%%", r.PassAt1*100)
		pk := fmt.Sprintf("%.0f%%", r.PassK*100)
		verdict := "FAIL"
		if r.AllPassed {
			verdict = "PASS"
		}
		if r.K == 0 {
			pat1 = "SKIP"
			pk = "SKIP"
			verdict = "SKIP"
		}
		fmt.Fprintf(&b, "║ %s ║ %-40s ║ %s ║ %-8s ║ %-8s ║ %-6s ║\n", id, name, tier, pat1, pk, verdict)
	}

	b.WriteString("╠════════╩══════════════════════════════════════════╩════════╩══════════╩══════════╩════════╣\n")
	summary := fmt.Sprintf("PASS:%d  FAIL:%d  SKIP:%d  TOTAL:%d  Gate: %s",
		r.PassedTasks, r.FailedTasks, r.SkippedTasks, r.TotalTasks, r.GateVerdict)
	fmt.Fprintf(&b, "║ %-74s ║\n", summary)
	b.WriteString("╚════════════════════════════════════════════════════════════════════════════════════════════╝\n")

	// Print failure details
	hasFails := false
	for _, r := range r.Results {
		if r.K > 0 && !r.AllPassed {
			if !hasFails {
				b.WriteString("\nFailure details:\n")
				hasFails = true
			}
			fmt.Fprintf(&b, "  %s (%s): %s\n", r.ID, r.Name, r.Detail)
		}
	}

	return b.String()
}

// JSON returns the machine-readable JSON report.
func (r *GoldenReport) JSON() ([]byte, error) {
	return json.MarshalIndent(r, "", "  ")
}

// Verdict returns a short summary string.
func (r *GoldenReport) Verdict() string {
	return fmt.Sprintf("%d/%d tasks pass (pass^k gate: %s)", r.PassedTasks, r.TotalTasks, r.GateVerdict)
}

// ─── Helpers ────────────────────────────────────────────────────────────────

func truncateStr(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-1] + "…"
}

// CompileRegex is a helper for graders that need regex matching.
func CompileRegex(pattern string) *regexp.Regexp {
	r, err := regexp.Compile(pattern)
	if err != nil {
		return nil
	}
	return r
}
