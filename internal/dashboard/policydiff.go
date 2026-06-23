package dashboard

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"reflect"
	"strings"

	"github.com/parvezsyed/agentpaas/internal/policy"
)

// PolicyDiffView is the sanitized API response for a policy diff.
type PolicyDiffView struct {
	DigestA      string            `json:"digest_a"`
	DigestB      string            `json:"digest_b"`
	Identical    bool              `json:"identical"`
	CanonicalA   map[string]string `json:"canonical_a"`
	CanonicalB   map[string]string `json:"canonical_b"`
	DiffSections []DiffSection     `json:"diff_sections,omitempty"`
	WarningsA    []string          `json:"warnings_a,omitempty"`
	WarningsB    []string          `json:"warnings_b,omitempty"`
}

// DiffSection represents a single changed section between two policies.
type DiffSection struct {
	Section string `json:"section"`
	Status  string `json:"status"`
	Summary string `json:"summary"`
}

// ServePolicyDiff handles GET /api/policy/diff?a=<path>&b=<path>.
func (s *Server) ServePolicyDiff(w http.ResponseWriter, r *http.Request) {
	pathA := strings.TrimSpace(r.URL.Query().Get("a"))
	pathB := strings.TrimSpace(r.URL.Query().Get("b"))
	if pathA == "" || pathB == "" {
		writeJSONError(w, http.StatusBadRequest, "both 'a' and 'b' query params required")
		return
	}

	resolvedA, err := resolveDashboardReadPath(pathA, s.policyDir)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid path")
		return
	}
	resolvedB, err := resolveDashboardReadPath(pathB, s.policyDir)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid path")
		return
	}

	policyA, err := parsePolicyFile(resolvedA)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "parse policy a failed")
		return
	}
	policyB, err := parsePolicyFile(resolvedB)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "parse policy b failed")
		return
	}

	canonicalA, warningsA := policy.Canonicalize(policyA)
	canonicalB, warningsB := policy.Canonicalize(policyB)
	digestA, err := policy.Digest(policyA)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "digest policy a failed")
		return
	}
	digestB, err := policy.Digest(policyB)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "digest policy b failed")
		return
	}

	sectionsA, err := canonicalPolicySections(canonicalA)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "canonicalize policy a failed")
		return
	}
	sectionsB, err := canonicalPolicySections(canonicalB)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "canonicalize policy b failed")
		return
	}

	view := PolicyDiffView{
		DigestA:      digestA,
		DigestB:      digestB,
		Identical:    digestA == digestB,
		CanonicalA:   sanitizeMap(sectionsA, maxAttributeValueLen),
		CanonicalB:   sanitizeMap(sectionsB, maxAttributeValueLen),
		DiffSections: diffPolicySections(sectionsA, sectionsB),
		WarningsA:    sanitizeStringSlice(warningsA),
		WarningsB:    sanitizeStringSlice(warningsB),
	}
	writeJSON(w, http.StatusOK, view)
}

func parsePolicyFile(path string) (*policy.Policy, error) {
	// #nosec G304 -- path is absolute, traversal-checked, and symlink-checked by resolveDashboardReadPath.
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open policy: %w", err)
	}
	defer func() { _ = file.Close() }()
	parsed, err := policy.ParsePolicy(file)
	if err != nil {
		return nil, fmt.Errorf("parse policy: %w", err)
	}
	return parsed, nil
}

func canonicalPolicySections(cp *policy.CanonicalPolicy) (map[string]string, error) {
	sections := map[string]interface{}{
		"version":     cp.Version,
		"agent":       cp.Agent,
		"egress":      cp.Egress,
		"credentials": cp.Credentials,
		"mcp_servers": cp.MCPServers,
		"hooks":       cp.Hooks,
		"ingress":     cp.Ingress,
	}
	out := make(map[string]string, len(sections))
	for name, value := range sections {
		data, err := json.Marshal(value)
		if err != nil {
			return nil, fmt.Errorf("marshal canonical section %s: %w", name, err)
		}
		out[name] = string(data)
	}
	return out, nil
}

func diffPolicySections(a, b map[string]string) []DiffSection {
	names := []string{"version", "agent", "egress", "credentials", "mcp_servers", "hooks", "ingress"}
	diffs := make([]DiffSection, 0)
	for _, name := range names {
		valueA, okA := a[name]
		valueB, okB := b[name]
		switch {
		case !okA && okB:
			diffs = append(diffs, newDiffSection(name, "added"))
		case okA && !okB:
			diffs = append(diffs, newDiffSection(name, "removed"))
		case !reflect.DeepEqual(valueA, valueB):
			diffs = append(diffs, newDiffSection(name, "changed"))
		}
	}
	return diffs
}

func newDiffSection(section string, status string) DiffSection {
	safeSection := sanitizeString(section, maxAttributeValueLen)
	safeStatus := sanitizeString(status, maxAttributeValueLen)
	return DiffSection{
		Section: safeSection,
		Status:  safeStatus,
		Summary: sanitizeString(fmt.Sprintf("%s %s", safeSection, safeStatus), maxAttributeValueLen),
	}
}

func sanitizeStringSlice(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = append(out, sanitizeString(value, maxAttributeValueLen))
	}
	return out
}
