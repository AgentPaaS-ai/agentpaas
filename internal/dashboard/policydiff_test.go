package dashboard

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/parvezsyed/agentpaas/internal/policy"
)

func TestPolicyDiff_IdenticalPolicies(t *testing.T) {
	pathA := writePolicyDiffTestPolicy(t, "a.yaml", testPolicyYAML("agent-a", "example.com"))
	pathB := writePolicyDiffTestPolicy(t, "b.yaml", testPolicyYAML("agent-a", "example.com"))
	server := NewServer("", testAPIKey, nil, &MockResourceManager{})

	var view PolicyDiffView
	requestPolicyDiffJSON(t, server, pathA, pathB, &view)

	if !view.Identical {
		t.Fatalf("Identical = false, want true: %#v", view)
	}
	if view.DigestA == "" || view.DigestB == "" || view.DigestA != view.DigestB {
		t.Fatalf("digests not identical: %q %q", view.DigestA, view.DigestB)
	}
}

func TestPolicyDiff_DifferentPolicies(t *testing.T) {
	pathA := writePolicyDiffTestPolicy(t, "a.yaml", testPolicyYAML("agent-a", "example.com"))
	pathB := writePolicyDiffTestPolicy(t, "b.yaml", testPolicyYAML("agent-b", "api.example.com"))
	server := NewServer("", testAPIKey, nil, &MockResourceManager{})

	var view PolicyDiffView
	requestPolicyDiffJSON(t, server, pathA, pathB, &view)

	if view.Identical {
		t.Fatalf("Identical = true, want false")
	}
	if len(view.DiffSections) == 0 {
		t.Fatalf("DiffSections empty: %#v", view)
	}
}

func TestPolicyDiff_MissingPath(t *testing.T) {
	server := NewServer("", testAPIKey, nil, &MockResourceManager{})
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/policy/diff?a=/tmp/policy.yaml", nil)
	req.Header.Set("Authorization", "Bearer "+testAPIKey)

	server.handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusBadRequest)
	}
}

func TestPolicyDiff_PathTraversal(t *testing.T) {
	server := NewServer("", testAPIKey, nil, &MockResourceManager{})
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/policy/diff?a=../a.yaml&b=/tmp/b.yaml", nil)
	req.Header.Set("Authorization", "Bearer "+testAPIKey)

	server.handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusBadRequest)
	}
}

func TestPolicyDiff_DigestMatches(t *testing.T) {
	body := testPolicyYAML("agent-a", "example.com")
	pathA := writePolicyDiffTestPolicy(t, "a.yaml", body)
	pathB := writePolicyDiffTestPolicy(t, "b.yaml", body)
	file, err := os.Open(pathA)
	if err != nil {
		t.Fatalf("open policy: %v", err)
	}
	defer func() { _ = file.Close() }()
	parsed, err := policy.ParsePolicy(file)
	if err != nil {
		t.Fatalf("parse policy: %v", err)
	}
	want, err := policy.Digest(parsed)
	if err != nil {
		t.Fatalf("digest policy: %v", err)
	}
	server := NewServer("", testAPIKey, nil, &MockResourceManager{})

	var view PolicyDiffView
	requestPolicyDiffJSON(t, server, pathA, pathB, &view)

	if view.DigestA != want {
		t.Fatalf("DigestA = %q, want %q", view.DigestA, want)
	}
}

func writePolicyDiffTestPolicy(t *testing.T, name string, body string) string {
	t.Helper()
	dir := dashboardTestTempDir(t, "policy-diff-*")
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatalf("write policy: %v", err)
	}
	return path
}

func testPolicyYAML(agentName, domain string) string {
	return "version: v1\nagent:\n  name: " + agentName + "\negress:\n  - domain: " + domain + "\n    ports: [443]\n"
}

func requestPolicyDiffJSON(t *testing.T, server *Server, pathA, pathB string, dst interface{}) {
	t.Helper()
	endpoint := "/api/policy/diff?a=" + url.QueryEscape(pathA) + "&b=" + url.QueryEscape(pathB)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, endpoint, nil)
	req.Header.Set("Authorization", "Bearer "+testAPIKey)

	server.handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", rr.Code, http.StatusOK, rr.Body.String())
	}
	if err := json.Unmarshal(rr.Body.Bytes(), dst); err != nil {
		t.Fatalf("decode response: %v; body=%s", err, rr.Body.String())
	}
}

func dashboardTestTempDir(t *testing.T, pattern string) string {
	t.Helper()
	dir, err := os.MkdirTemp(".", pattern)
	if err != nil {
		t.Fatalf("make temp dir: %v", err)
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		t.Fatalf("abs temp dir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(abs) })
	return abs
}
