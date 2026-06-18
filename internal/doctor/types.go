package doctor

// CheckResult holds the outcome of a single diagnostic check.
//
// Status must be one of "ok", "warning", or "error". Message is a
// human-readable description of the result. FixHint provides actionable
// remediation advice when Status is "warning" or "error".
type CheckResult struct {
	// Name is the unique identifier for this check (e.g. "docker_reachable").
	Name string

	// Status is the check outcome: "ok", "warning", or "error".
	Status string

	// Message is a human-readable description of the check result.
	Message string

	// FixHint is optional remediation advice. It is non-empty when Status
	// is "warning" or "error".
	FixHint string
}

// OverallStatus derives the aggregate health from a set of check results.
//
// It returns "error" if any check has status "error" or an unknown/empty
// status (defensive measure), "warning" if any check has status "warning"
// (and no errors), and "ok" otherwise.
func OverallStatus(checks []CheckResult) string {
	hasWarning := false
	for _, c := range checks {
		if c.Status == "error" {
			return "error"
		}
		if c.Status == "" || (c.Status != "ok" && c.Status != "warning") {
			// Treat unknown/empty status as an error.
			return "error"
		}
		if c.Status == "warning" {
			hasWarning = true
		}
	}
	if hasWarning {
		return "warning"
	}
	return "ok"
}