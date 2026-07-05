package golden

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// FindDataset locates and loads the golden_tasks.yaml file.
// It tries the given path, then walks up from the current directory.
func FindDataset(path string) (*Dataset, error) {
	// Try the given path directly
	if _, err := os.Stat(path); err == nil {
		return loadDataset(path)
	}

	// Try walking up from cwd
	cwd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("getwd: %w", err)
	}

	// Try common locations
	candidates := []string{
		filepath.Join(cwd, "test", "golden", "golden_tasks.yaml"),
		filepath.Join(cwd, "..", "..", "test", "golden", "golden_tasks.yaml"),
		filepath.Join(cwd, "golden_tasks.yaml"),
	}

	// Walk up the tree looking for test/golden/golden_tasks.yaml
	dir := cwd
	for i := 0; i < 10; i++ {
		candidate := filepath.Join(dir, "test", "golden", "golden_tasks.yaml")
		if _, err := os.Stat(candidate); err == nil {
			return loadDataset(candidate)
		}
		// Also check if we're IN test/golden/
		candidate = filepath.Join(dir, "golden_tasks.yaml")
		if _, err := os.Stat(candidate); err == nil {
			return loadDataset(candidate)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return loadDataset(c)
		}
	}

	return nil, fmt.Errorf("golden_tasks.yaml not found (tried %s and walked up from %s)", path, cwd)
}

// loadDataset reads and parses a golden_tasks.yaml file.
func loadDataset(path string) (*Dataset, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	var dataset Dataset
	if err := yaml.Unmarshal(data, &dataset); err != nil {
		return nil, fmt.Errorf("unmarshal %s: %w", path, err)
	}

	// Set defaults
	if dataset.DefaultK <= 0 {
		dataset.DefaultK = 3
	}
	if dataset.Thresholds.PassKMin <= 0 {
		dataset.Thresholds.PassKMin = 0.85
	}
	if dataset.Thresholds.PassAt1Min <= 0 {
		dataset.Thresholds.PassAt1Min = 0.95
	}
	if dataset.Thresholds.RegressionDelta <= 0 {
		dataset.Thresholds.RegressionDelta = 0.05
	}

	return &dataset, nil
}

// Validate checks that the dataset is internally consistent.
// Useful for a "golden-validate" make target.
func (d *Dataset) Validate() []string {
	var issues []string
	seenIDs := make(map[string]bool)

	for i, task := range d.Tasks {
		// Check unique ID
		if seenIDs[task.ID] {
			issues = append(issues, fmt.Sprintf("task %d: duplicate ID %q", i, task.ID))
		}
		seenIDs[task.ID] = true

		// Check tier is valid
		switch task.Tier {
		case "fast", "slow", "docker":
		default:
			issues = append(issues, fmt.Sprintf("task %s: invalid tier %q (must be fast, slow, or docker)", task.ID, task.Tier))
		}

		// Check grader is valid
		switch task.Grader {
		case "code", "judge":
		default:
			issues = append(issues, fmt.Sprintf("task %s: invalid grader %q (must be code or judge)", task.ID, task.Grader))
		}

		// Check source_failure is documented
		if strings.TrimSpace(task.SourceFailure) == "" {
			issues = append(issues, fmt.Sprintf("task %s: missing source_failure documentation", task.ID))
		}

		// Check docker tasks have requires_docker flag
		if task.Tier == "docker" && !task.RequiresDocker {
			issues = append(issues, fmt.Sprintf("task %s: docker tier task should have requires_docker: true", task.ID))
		}
	}

	return issues
}

// Stats returns summary statistics about the dataset.
func (d *Dataset) Stats() string {
	tiers := map[string]int{}
	graders := map[string]int{}
	categories := map[string]int{}

	for _, task := range d.Tasks {
		tiers[task.Tier]++
		graders[task.Grader]++
		categories[task.Category]++
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Total tasks: %d\n", len(d.Tasks))
	fmt.Fprintf(&b, "By tier: fast=%d slow=%d docker=%d\n", tiers["fast"], tiers["slow"], tiers["docker"])
	fmt.Fprintf(&b, "By grader: code=%d judge=%d\n", graders["code"], graders["judge"])
	fmt.Fprintf(&b, "Categories: ")
	cats := make([]string, 0, len(categories))
	for cat := range categories {
		cats = append(cats, fmt.Sprintf("%s=%d", cat, categories[cat]))
	}
	// Sort for deterministic output
	for i := 0; i < len(cats); i++ {
		for j := i + 1; j < len(cats); j++ {
			if cats[i] > cats[j] {
				cats[i], cats[j] = cats[j], cats[i]
			}
		}
	}
	b.WriteString(strings.Join(cats, ", "))
	return b.String()
}
