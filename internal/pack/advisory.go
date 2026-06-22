package pack

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// AdvisorySeverity levels for OSV findings.
const (
	AdvisorySeverityCritical = "CRITICAL"
	AdvisorySeverityHigh     = "HIGH"
	AdvisorySeverityMedium   = "MEDIUM"
	AdvisorySeverityLow      = "LOW"
	AdvisorySeverityNone     = "NONE"
)

// AdvisoryFinding represents a single OSV advisory.
type AdvisoryFinding struct {
	ID         string   `json:"id"`
	Package    string   `json:"package"`
	Version    string   `json:"version"`
	Severity   string   `json:"severity"`
	Summary    string   `json:"summary"`
	FixedIn    string   `json:"fixed_in,omitempty"`
	References []string `json:"references,omitempty"`
}

// AdvisoryReport is the summary of OSV scan results.
type AdvisoryReport struct {
	Total     int               `json:"total"`
	Critical  int               `json:"critical"`
	High      int               `json:"high"`
	Medium    int               `json:"medium"`
	Low       int               `json:"low"`
	Findings  []AdvisoryFinding `json:"findings"`
	Scanned   bool              `json:"scanned"`
	RawOutput string            `json:"raw_output,omitempty"`
}

// ScanAdvisories runs osv-scanner on the SBOM and returns an advisory summary.
func ScanAdvisories(ctx context.Context, sbomPath string) (*AdvisoryReport, error) {
	path, err := exec.LookPath("osv-scanner")
	if err != nil {
		return &AdvisoryReport{Scanned: false}, nil
	}

	runCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	output, runErr := exec.CommandContext(runCtx, path, "--format", "json", sbomPath).CombinedOutput()
	if runCtx.Err() != nil {
		return nil, fmt.Errorf("run osv-scanner: %w", runCtx.Err())
	}

	report, err := parseOSVOutput(output)
	if err != nil {
		return nil, fmt.Errorf("parse osv-scanner output: %w", err)
	}
	report.Scanned = true
	report.RawOutput = string(output)

	if runErr == nil {
		return report, nil
	}
	var exitErr *exec.ExitError
	if errors.As(runErr, &exitErr) && exitErr.ExitCode() == 1 {
		return report, nil
	}
	return nil, fmt.Errorf("run osv-scanner: %w", runErr)
}

// ShouldFailBuild returns true when configured critical/high advisories exist.
func (r *AdvisoryReport) ShouldFailBuild(failOnCritical bool) bool {
	if r == nil || !failOnCritical {
		return false
	}
	return r.Critical > 0 || r.High > 0
}

// Summary returns a human-readable summary string for CLI output.
func (r *AdvisoryReport) Summary() string {
	if r == nil {
		return "OSV advisories: not scanned"
	}
	if !r.Scanned {
		return "OSV advisories: not scanned (osv-scanner not installed)"
	}
	return fmt.Sprintf(
		"OSV advisories: %d total (critical=%d high=%d medium=%d low=%d)",
		r.Total,
		r.Critical,
		r.High,
		r.Medium,
		r.Low,
	)
}

type osvOutput struct {
	Results []osvResult `json:"results"`
}

type osvResult struct {
	Packages        []osvPackageResult `json:"packages"`
	Vulnerabilities []osvVulnerability `json:"vulnerabilities"`
}

type osvPackageResult struct {
	Package         osvPackage         `json:"package"`
	Vulnerabilities []osvVulnerability `json:"vulnerabilities"`
}

type osvPackage struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type osvVulnerability struct {
	ID               string              `json:"id"`
	Summary          string              `json:"summary"`
	Severity         json.RawMessage     `json:"severity"`
	DatabaseSpecific osvDatabaseSpecific `json:"database_specific"`
	Affected         []osvAffected       `json:"affected"`
	References       []osvReference      `json:"references"`
}

type osvDatabaseSpecific struct {
	Severity string `json:"severity"`
}

type osvAffected struct {
	Ranges []osvRange `json:"ranges"`
}

type osvRange struct {
	Events []osvEvent `json:"events"`
}

type osvEvent struct {
	Fixed string `json:"fixed"`
}

type osvReference struct {
	URL string `json:"url"`
}

func parseOSVOutput(output []byte) (*AdvisoryReport, error) {
	report := &AdvisoryReport{Scanned: true}
	if len(strings.TrimSpace(string(output))) == 0 {
		return report, nil
	}

	var parsed osvOutput
	if err := json.Unmarshal(output, &parsed); err != nil {
		return nil, fmt.Errorf("decode OSV JSON: %w", err)
	}

	for _, result := range parsed.Results {
		for _, pkg := range result.Packages {
			for _, vuln := range pkg.Vulnerabilities {
				report.addFinding(findingFromOSV(pkg.Package, vuln))
			}
		}
		for _, vuln := range result.Vulnerabilities {
			report.addFinding(findingFromOSV(osvPackage{}, vuln))
		}
	}

	return report, nil
}

func (r *AdvisoryReport) addFinding(finding AdvisoryFinding) {
	r.Findings = append(r.Findings, finding)
	r.Total++
	switch finding.Severity {
	case AdvisorySeverityCritical:
		r.Critical++
	case AdvisorySeverityHigh:
		r.High++
	case AdvisorySeverityMedium:
		r.Medium++
	case AdvisorySeverityLow:
		r.Low++
	}
}

func findingFromOSV(pkg osvPackage, vuln osvVulnerability) AdvisoryFinding {
	return AdvisoryFinding{
		ID:         vuln.ID,
		Package:    pkg.Name,
		Version:    pkg.Version,
		Severity:   advisorySeverity(vuln),
		Summary:    vuln.Summary,
		FixedIn:    firstFixedVersion(vuln.Affected),
		References: referenceURLs(vuln.References),
	}
}

func advisorySeverity(vuln osvVulnerability) string {
	if severity := normalizeSeverity(vuln.DatabaseSpecific.Severity); severity != "" {
		return severity
	}
	if len(vuln.Severity) == 0 {
		return AdvisorySeverityNone
	}

	var severityString string
	if err := json.Unmarshal(vuln.Severity, &severityString); err == nil {
		if severity := normalizeSeverity(severityString); severity != "" {
			return severity
		}
		return severityFromCVSSScore(severityString)
	}

	var severities []struct {
		Score string `json:"score"`
	}
	if err := json.Unmarshal(vuln.Severity, &severities); err != nil {
		return AdvisorySeverityNone
	}
	for _, severity := range severities {
		if normalized := normalizeSeverity(severity.Score); normalized != "" {
			return normalized
		}
		if normalized := severityFromCVSSScore(severity.Score); normalized != AdvisorySeverityNone {
			return normalized
		}
	}
	return AdvisorySeverityNone
}

func normalizeSeverity(value string) string {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case AdvisorySeverityCritical:
		return AdvisorySeverityCritical
	case AdvisorySeverityHigh:
		return AdvisorySeverityHigh
	case AdvisorySeverityMedium:
		return AdvisorySeverityMedium
	case AdvisorySeverityLow:
		return AdvisorySeverityLow
	case AdvisorySeverityNone:
		return AdvisorySeverityNone
	default:
		return ""
	}
}

func severityFromCVSSScore(score string) string {
	score = strings.TrimSpace(score)
	if strings.HasPrefix(score, "CVSS:") {
		return severityFromCVSSVector(score)
	}
	value, err := strconv.ParseFloat(score, 64)
	if err != nil {
		return AdvisorySeverityNone
	}
	switch {
	case value >= 9:
		return AdvisorySeverityCritical
	case value >= 7:
		return AdvisorySeverityHigh
	case value >= 4:
		return AdvisorySeverityMedium
	case value > 0:
		return AdvisorySeverityLow
	default:
		return AdvisorySeverityNone
	}
}

func severityFromCVSSVector(vector string) string {
	metrics := map[string]string{}
	for _, part := range strings.Split(vector, "/") {
		key, value, ok := strings.Cut(part, ":")
		if ok {
			metrics[key] = value
		}
	}
	if metrics["I"] == "H" || metrics["C"] == "H" || metrics["A"] == "H" {
		return AdvisorySeverityHigh
	}
	if metrics["I"] == "L" || metrics["C"] == "L" || metrics["A"] == "L" {
		return AdvisorySeverityLow
	}
	return AdvisorySeverityNone
}

func firstFixedVersion(affected []osvAffected) string {
	for _, item := range affected {
		for _, affectedRange := range item.Ranges {
			for _, event := range affectedRange.Events {
				if event.Fixed != "" {
					return event.Fixed
				}
			}
		}
	}
	return ""
}

func referenceURLs(references []osvReference) []string {
	urls := make([]string, 0, len(references))
	for _, ref := range references {
		if ref.URL != "" {
			urls = append(urls, ref.URL)
		}
	}
	return urls
}

// ErrOCILayoutCorrupt is returned when the local OCI layout is missing or corrupt.
var ErrOCILayoutCorrupt = errors.New("local OCI layout is missing or corrupt")

// OCILayoutError provides an actionable repair hint when the OCI layout is missing or corrupt.
type OCILayoutError struct {
	Path   string
	Reason string
	Hint   string
	Cause  error
}

func (e *OCILayoutError) Error() string {
	return fmt.Sprintf("oci layout %s: %s - %s (cause: %v)", e.Path, e.Reason, e.Hint, e.Cause)
}

func (e *OCILayoutError) Unwrap() error { return e.Cause }

func (e *OCILayoutError) Is(target error) bool {
	return target == ErrOCILayoutCorrupt
}

// ValidateOCILayout checks that a local OCI layout directory is valid.
func ValidateOCILayout(layoutPath string) error {
	if err := validateSafePath(layoutPath); err != nil {
		return newOCILayoutError(layoutPath, "missing", repairHintForReason("missing"), err)
	}

	if err := requireDirectory(layoutPath); err != nil {
		return newOCILayoutError(layoutPath, "missing", repairHintForReason("missing"), err)
	}

	ociLayoutPath := filepath.Join(layoutPath, "oci-layout")
	if err := requireRegularFile(ociLayoutPath); err != nil {
		return newOCILayoutError(layoutPath, "missing", repairHintForReason("missing"), err)
	}

	contents, err := os.ReadFile(ociLayoutPath)
	if err != nil {
		return newOCILayoutError(layoutPath, "corrupt_index", repairHintForReason("corrupt_index"), fmt.Errorf("read oci-layout: %w", err))
	}
	var layout struct {
		ImageLayoutVersion string `json:"imageLayoutVersion"`
	}
	if err := json.Unmarshal(contents, &layout); err != nil {
		return newOCILayoutError(layoutPath, "corrupt_index", repairHintForReason("corrupt_index"), fmt.Errorf("decode oci-layout: %w", err))
	}
	if layout.ImageLayoutVersion != "1.0.0" {
		return newOCILayoutError(layoutPath, "corrupt_index", repairHintForReason("corrupt_index"), fmt.Errorf("unsupported imageLayoutVersion %q", layout.ImageLayoutVersion))
	}

	indexPath := filepath.Join(layoutPath, "index.json")
	if err := requireRegularFile(indexPath); err != nil {
		return newOCILayoutError(layoutPath, "corrupt_index", repairHintForReason("corrupt_index"), err)
	}
	indexContents, err := os.ReadFile(indexPath)
	if err != nil {
		return newOCILayoutError(layoutPath, "corrupt_index", repairHintForReason("corrupt_index"), fmt.Errorf("read index.json: %w", err))
	}
	var index struct {
		SchemaVersion int `json:"schemaVersion"`
	}
	if err := json.Unmarshal(indexContents, &index); err != nil {
		return newOCILayoutError(layoutPath, "corrupt_index", repairHintForReason("corrupt_index"), fmt.Errorf("decode index.json: %w", err))
	}
	if index.SchemaVersion == 0 {
		return newOCILayoutError(layoutPath, "corrupt_index", repairHintForReason("corrupt_index"), errors.New("index.json missing schemaVersion"))
	}

	if err := requireDirectory(filepath.Join(layoutPath, "blobs")); err != nil {
		return newOCILayoutError(layoutPath, "missing_blob", repairHintForReason("missing_blob"), err)
	}
	if err := requireDirectory(filepath.Join(layoutPath, "blobs", "sha256")); err != nil {
		return newOCILayoutError(layoutPath, "missing_blob", repairHintForReason("missing_blob"), err)
	}

	return nil
}

// RepairHint returns a human-readable repair hint for the given OCILayoutError.
func RepairHint(err *OCILayoutError) string {
	if err == nil {
		return ""
	}
	if err.Hint != "" {
		return err.Hint
	}
	return repairHintForReason(err.Reason)
}

func newOCILayoutError(path string, reason string, hint string, cause error) error {
	return &OCILayoutError{
		Path:   path,
		Reason: reason,
		Hint:   hint,
		Cause:  fmt.Errorf("%w: %w", ErrOCILayoutCorrupt, cause),
	}
}

func repairHintForReason(reason string) string {
	switch reason {
	case "missing":
		return "rebuild the agent package to recreate the local OCI layout"
	case "corrupt_index":
		return "remove the local OCI layout and rebuild the agent package to regenerate oci-layout and index.json"
	case "missing_blob":
		return "remove and rebuild the local OCI layout so missing blobs are regenerated"
	case "invalid_manifest":
		return "rebuild the agent package to regenerate the invalid OCI manifest"
	default:
		return "rebuild the agent package to recreate the local OCI layout"
	}
}

func validateSafePath(path string) error {
	if !filepath.IsAbs(path) {
		return fmt.Errorf("path must be absolute: %s", path)
	}
	cleaned := filepath.Clean(path)
	if cleaned != path {
		return fmt.Errorf("path must be clean: %s", path)
	}
	for _, disallowed := range []string{"/etc", "/usr", "/bin"} {
		if cleaned == disallowed || strings.HasPrefix(cleaned, disallowed+string(os.PathSeparator)) {
			return fmt.Errorf("path is in disallowed system directory: %s", path)
		}
	}
	return checkNoSymlinksInExistingPath(cleaned)
}

func checkNoSymlinksInExistingPath(path string) error {
	current := filepath.VolumeName(path)
	if current == "" {
		current = string(os.PathSeparator)
	}
	rest := strings.TrimPrefix(path, current)
	rest = strings.Trim(rest, string(os.PathSeparator))
	if rest == "" {
		return nil
	}
	for _, part := range strings.Split(rest, string(os.PathSeparator)) {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			return fmt.Errorf("lstat %s: %w", current, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("path component %s is a symlink", current)
		}
	}
	return nil
}

func requireDirectory(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("lstat directory %s: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("directory %s is a symlink", path)
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", path)
	}
	return nil
}

func requireRegularFile(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("lstat file %s: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("file %s is a symlink", path)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%s is not a regular file", path)
	}
	return nil
}
