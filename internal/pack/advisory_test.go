package pack

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestScanAdvisories_NotInstalled_ReturnsScannedFalse(t *testing.T) {
	t.Setenv("PATH", symlinkSafeTempDir(t))

	report, err := ScanAdvisories(context.Background(), filepath.Join(symlinkSafeTempDir(t), "sbom.json"))
	if err != nil {
		t.Fatalf("ScanAdvisories() error = %v, want nil", err)
	}
	if report.Scanned {
		t.Fatal("report.Scanned = true, want false")
	}
	if report.Total != 0 {
		t.Fatalf("report.Total = %d, want 0", report.Total)
	}
}

func TestScanAdvisories_NoFindings(t *testing.T) {
	installMockOSVScanner(t)
	t.Setenv("TEST_OSV_EXIT", "0")
	t.Setenv("TEST_OSV_OUTPUT", `{"results":[]}`)

	report, err := ScanAdvisories(context.Background(), filepath.Join(symlinkSafeTempDir(t), "sbom.json"))
	if err != nil {
		t.Fatalf("ScanAdvisories() error = %v, want nil", err)
	}
	if !report.Scanned {
		t.Fatal("report.Scanned = false, want true")
	}
	if report.Total != 0 {
		t.Fatalf("report.Total = %d, want 0", report.Total)
	}
}

func TestScanAdvisories_WithFindings(t *testing.T) {
	installMockOSVScanner(t)
	t.Setenv("TEST_OSV_EXIT", "1")
	t.Setenv("TEST_OSV_OUTPUT", `{
		"results": [{
			"packages": [{
				"package": {"name": "github.com/example/pkg", "version": "v1.2.3"},
				"vulnerabilities": [{
					"id": "GHSA-1234",
					"summary": "critical bug",
					"database_specific": {"severity": "CRITICAL"},
					"affected": [{"ranges": [{"events": [{"introduced": "0"}, {"fixed": "v1.2.4"}]}]}],
					"references": [{"url": "https://osv.dev/GHSA-1234"}]
				}, {
					"id": "CVE-2026-0001",
					"summary": "low bug",
					"severity": [{"type": "CVSS_V3", "score": "CVSS:3.1/AV:N/AC:H/PR:L/UI:R/S:U/C:L/I:N/A:N"}]
				}]
			}]
		}]
	}`)

	report, err := ScanAdvisories(context.Background(), filepath.Join(symlinkSafeTempDir(t), "sbom.json"))
	if err != nil {
		t.Fatalf("ScanAdvisories() error = %v, want nil", err)
	}
	if report.Total != 2 {
		t.Fatalf("report.Total = %d, want 2", report.Total)
	}
	if report.Critical != 1 || report.Low != 1 {
		t.Fatalf("counts critical=%d low=%d, want critical=1 low=1", report.Critical, report.Low)
	}
	if got := report.Findings[0].FixedIn; got != "v1.2.4" {
		t.Fatalf("FixedIn = %q, want v1.2.4", got)
	}
	if len(report.Findings[0].References) != 1 {
		t.Fatalf("References len = %d, want 1", len(report.Findings[0].References))
	}
}

func TestScanAdvisories_ScannerError(t *testing.T) {
	installMockOSVScanner(t)
	t.Setenv("TEST_OSV_EXIT", "2")
	t.Setenv("TEST_OSV_OUTPUT", "scanner exploded")

	_, err := ScanAdvisories(context.Background(), filepath.Join(symlinkSafeTempDir(t), "sbom.json"))
	if err == nil {
		t.Fatal("ScanAdvisories() error = nil, want error")
	}
}

func TestAdvisoryReport_ShouldFailBuild_CriticalWithFlag(t *testing.T) {
	report := &AdvisoryReport{Critical: 1}
	if !report.ShouldFailBuild(true) {
		t.Fatal("ShouldFailBuild(true) = false, want true")
	}
}

func TestAdvisoryReport_ShouldFailBuild_CriticalWithoutFlag(t *testing.T) {
	report := &AdvisoryReport{Critical: 1}
	if report.ShouldFailBuild(false) {
		t.Fatal("ShouldFailBuild(false) = true, want false")
	}
}

func TestAdvisoryReport_ShouldFailBuild_LowNeverFails(t *testing.T) {
	report := &AdvisoryReport{Low: 1}
	if report.ShouldFailBuild(true) {
		t.Fatal("ShouldFailBuild(true) = true, want false")
	}
}

func TestAdvisoryReport_Summary(t *testing.T) {
	report := &AdvisoryReport{Scanned: true, Total: 4, Critical: 1, High: 1, Medium: 1, Low: 1}
	got := report.Summary()
	for _, want := range []string{"OSV advisories: 4 total", "critical=1", "high=1", "medium=1", "low=1"} {
		if !strings.Contains(got, want) {
			t.Fatalf("Summary() = %q, want substring %q", got, want)
		}
	}
}

func TestValidateOCILayout_ValidLayout(t *testing.T) {
	layoutPath := writeTestOCILayout(t)

	if err := ValidateOCILayout(layoutPath); err != nil {
		t.Fatalf("ValidateOCILayout() error = %v, want nil", err)
	}
}

func TestValidateOCILayout_MissingDir(t *testing.T) {
	err := ValidateOCILayout(filepath.Join(symlinkSafeTempDir(t), "missing"))
	assertOCILayoutError(t, err, "missing")
}

func TestValidateOCILayout_MissingOciLayoutFile(t *testing.T) {
	layoutPath := symlinkSafeTempDir(t)

	err := ValidateOCILayout(layoutPath)
	assertOCILayoutError(t, err, "missing")
}

func TestValidateOCILayout_CorruptOciLayoutJSON(t *testing.T) {
	layoutPath := writeTestOCILayout(t)
	if err := os.WriteFile(filepath.Join(layoutPath, "oci-layout"), []byte("{"), 0o644); err != nil {
		t.Fatalf("write corrupt oci-layout: %v", err)
	}

	err := ValidateOCILayout(layoutPath)
	assertOCILayoutError(t, err, "corrupt_index")
}

func TestValidateOCILayout_MissingIndexJSON(t *testing.T) {
	layoutPath := writeTestOCILayout(t)
	if err := os.Remove(filepath.Join(layoutPath, "index.json")); err != nil {
		t.Fatalf("remove index.json: %v", err)
	}

	err := ValidateOCILayout(layoutPath)
	assertOCILayoutError(t, err, "corrupt_index")
}

func TestValidateOCILayout_MissingBlobsDir(t *testing.T) {
	layoutPath := writeTestOCILayout(t)
	if err := os.RemoveAll(filepath.Join(layoutPath, "blobs")); err != nil {
		t.Fatalf("remove blobs: %v", err)
	}

	err := ValidateOCILayout(layoutPath)
	assertOCILayoutError(t, err, "missing_blob")
}

func TestRepairHint(t *testing.T) {
	err := &OCILayoutError{
		Path:   "/tmp/layout",
		Reason: "missing_blob",
		Hint:   "remove and rebuild the local OCI layout",
		Cause:  os.ErrNotExist,
	}

	got := RepairHint(err)
	if !strings.Contains(got, "remove and rebuild") {
		t.Fatalf("RepairHint() = %q, want repair hint", got)
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatal("errors.Is(err, os.ErrNotExist) = false, want true")
	}
}

func installMockOSVScanner(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake shell tools require a POSIX shell")
	}
	binDir := symlinkSafeTempDir(t)
	script := `#!/bin/sh
printf '%s' "$TEST_OSV_OUTPUT"
exit "$TEST_OSV_EXIT"
`
	path := filepath.Join(binDir, "osv-scanner")
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatalf("write fake osv-scanner: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func writeTestOCILayout(t *testing.T) string {
	t.Helper()
	layoutPath := symlinkSafeTempDir(t)
	if err := os.WriteFile(filepath.Join(layoutPath, "oci-layout"), []byte(`{"imageLayoutVersion":"1.0.0"}`), 0o644); err != nil {
		t.Fatalf("write oci-layout: %v", err)
	}
	if err := os.WriteFile(filepath.Join(layoutPath, "index.json"), []byte(`{"schemaVersion":2,"manifests":[]}`), 0o644); err != nil {
		t.Fatalf("write index.json: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(layoutPath, "blobs", "sha256"), 0o755); err != nil {
		t.Fatalf("mkdir blobs: %v", err)
	}
	return layoutPath
}

func assertOCILayoutError(t *testing.T, err error, reason string) {
	t.Helper()
	var layoutErr *OCILayoutError
	if !errors.As(err, &layoutErr) {
		t.Fatalf("error = %v, want *OCILayoutError", err)
	}
	if !errors.Is(err, ErrOCILayoutCorrupt) {
		t.Fatalf("errors.Is(err, ErrOCILayoutCorrupt) = false, want true")
	}
	if layoutErr.Reason != reason {
		t.Fatalf("Reason = %q, want %q", layoutErr.Reason, reason)
	}
	if layoutErr.Hint == "" {
		t.Fatal("Hint is empty, want actionable repair hint")
	}
}
