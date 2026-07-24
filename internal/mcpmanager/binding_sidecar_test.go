package mcpmanager

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// TestWriteReadMCPBindingSidecar verifies roundtrip serialization.
func TestWriteReadMCPBindingSidecar(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test-sidecar.json")

	sc := MCPBindingSidecar{
		WorkflowID: "wf-1",
		Bindings: []MCPBindingSidecarEntry{
			{
				BindingID:    "svc-1",
				ServiceRunID: "run-1",
				Endpoint:     "http://internal:8080",
				Capability:   "cap-token-1",
				AllowedTools: []string{"tool_a", "tool_b"},
				PackageDigest: "digest-abc",
				NetworkAlias: "alias-1",
				State:        "READY",
			},
		},
	}

	if err := WriteMCPBindingSidecar(path, sc); err != nil {
		t.Fatalf("WriteMCPBindingSidecar error = %v", err)
	}

	// Verify file has 0600 permissions.
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat error = %v", err)
	}
	if fi.Mode().Perm() != 0600 {
		t.Fatalf("file mode = %04o, want 0600", fi.Mode().Perm())
	}

	got, err := ReadMCPBindingSidecar(path)
	if err != nil {
		t.Fatalf("ReadMCPBindingSidecar error = %v", err)
	}

	if got.WorkflowID != sc.WorkflowID {
		t.Fatalf("WorkflowID = %q, want %q", got.WorkflowID, sc.WorkflowID)
	}
	if len(got.Bindings) != 1 {
		t.Fatalf("len(Bindings) = %d, want 1", len(got.Bindings))
	}
	if got.Bindings[0].BindingID != "svc-1" {
		t.Fatalf("Bindings[0].BindingID = %q, want svc-1", got.Bindings[0].BindingID)
	}
	if got.Bindings[0].Capability != "cap-token-1" {
		t.Fatalf("Bindings[0].Capability = %q, want cap-token-1", got.Bindings[0].Capability)
	}
	if got.Bindings[0].State != "READY" {
		t.Fatalf("Bindings[0].State = %q, want READY", got.Bindings[0].State)
	}
}

// TestReadMCPBindingSidecar_MissingFile returns an error for missing files.
func TestReadMCPBindingSidecar_MissingFile(t *testing.T) {
	_, err := ReadMCPBindingSidecar("/nonexistent/path/sidecar.json")
	if err == nil {
		t.Fatal("ReadMCPBindingSidecar with missing file: error = nil, want error")
	}
}

// TestServiceRegistryFromSidecar_READYOnly creates a registry from READY bindings.
func TestServiceRegistryFromSidecar_READYOnly(t *testing.T) {
	sc := MCPBindingSidecar{
		WorkflowID: "wf-2",
		Bindings: []MCPBindingSidecarEntry{
			{
				BindingID:    "svc-ready",
				Endpoint:     "http://internal:9090",
				Capability:   "cap-ready-1",
				AllowedTools: []string{"lookup"},
				State:        "READY",
			},
			{
				BindingID:    "svc-declared",
				Endpoint:     "http://internal:9091",
				Capability:   "cap-declared-1",
				AllowedTools: []string{"search"},
				State:        "DECLARED",
			},
		},
	}

	reg, err := ServiceRegistryFromSidecar(sc)
	if err != nil {
		t.Fatalf("ServiceRegistryFromSidecar error = %v", err)
	}

	// READY entry is present.
	inst, err := reg.Get("wf-2", "svc-ready")
	if err != nil {
		t.Fatalf("Get svc-ready error = %v", err)
	}
	if inst.State != StateReady {
		t.Fatalf("svc-ready state = %s, want READY", inst.State)
	}
	if inst.Endpoint != "http://internal:9090" {
		t.Fatalf("svc-ready endpoint = %q, want http://internal:9090", inst.Endpoint)
	}
	if inst.Capability != "cap-ready-1" {
		t.Fatalf("svc-ready capability = %q, want cap-ready-1", inst.Capability)
	}

	// Non-READY entry is skipped.
	_, err = reg.Get("wf-2", "svc-declared")
	if err == nil {
		t.Fatal("svc-declared should not be present (non-READY skipped)")
	}
}

// TestServiceRegistryFromSidecar_EmptyBindings handles empty bindings.
func TestServiceRegistryFromSidecar_EmptyBindings(t *testing.T) {
	sc := MCPBindingSidecar{
		WorkflowID: "wf-empty",
		Bindings:   []MCPBindingSidecarEntry{},
	}

	reg, err := ServiceRegistryFromSidecar(sc)
	if err != nil {
		t.Fatalf("ServiceRegistryFromSidecar error = %v", err)
	}
	if reg == nil {
		t.Fatal("ServiceRegistryFromSidecar returned nil for empty sidecar")
	}
}

// TestInstallSidecarOnRouter_WiresManagedPath verifies the sidecar wires
// the managed resolver and registers bindings on the Manager.
func TestInstallSidecarOnRouter_WiresManagedPath(t *testing.T) {
	// Stand up a fake MCP service endpoint that returns a distinctive value.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := mcpResponse{
			JSONRPC: "2.0",
			ID:      0,
			Result:  json.RawMessage(`{"sidecar":"wired","value":"distinctive"}`),
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer func() { ts.Close() }()

	sc := MCPBindingSidecar{
		WorkflowID: "wf-install",
		Bindings: []MCPBindingSidecarEntry{
			{
				BindingID:    "binding-1",
				Endpoint:     ts.URL,
				Capability:   "cap-install-0123456789abcdef0123456789abcdef01",
				AllowedTools: []string{"lookup", "search"},
				State:        "READY",
			},
		},
	}

	manager := NewManager()
	router := NewRouter(manager, nil, nil, nil)

	if err := InstallSidecarOnRouter(router, manager, sc); err != nil {
		t.Fatalf("InstallSidecarOnRouter error = %v", err)
	}

	// Verify the tool is now allowed on the Manager.
	if !manager.IsToolAllowed("binding-1", "lookup") {
		t.Fatal("lookup tool not allowed after InstallSidecarOnRouter")
	}

	// Verify CallTool works through the managed path.
	result, err := router.CallTool(context.Background(), "binding-1", "lookup", map[string]any{"q": "test"}, "agent-1", "run-1")
	if err != nil {
		t.Fatalf("CallTool error = %v", err)
	}
	resultMap, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("result type = %T, want map[string]any", result)
	}
	if resultMap["sidecar"] != "wired" {
		t.Fatalf("result.sidecar = %q, want wired", resultMap["sidecar"])
	}
	if resultMap["value"] != "distinctive" {
		t.Fatalf("result.value = %q, want distinctive", resultMap["value"])
	}
}

// TestInstallSidecarOnRouter_NonREADYSkipped ensures non-READY entries are skipped.
func TestInstallSidecarOnRouter_NonREADYSkipped(t *testing.T) {
	sc := MCPBindingSidecar{
		WorkflowID: "wf-skip",
		Bindings: []MCPBindingSidecarEntry{
			{
				BindingID:    "declared-only",
				Endpoint:     "http://internal:8080",
				Capability:   "cap-skip-1",
				AllowedTools: []string{"lookup"},
				State:        "DECLARED",
			},
		},
	}

	manager := NewManager()
	router := NewRouter(manager, nil, nil, nil)

	if err := InstallSidecarOnRouter(router, manager, sc); err != nil {
		t.Fatalf("InstallSidecarOnRouter error = %v", err)
	}

	// The DECLARED binding should NOT be registered.
	if manager.IsToolAllowed("declared-only", "lookup") {
		t.Fatal("lookup tool should not be allowed for non-READY entry")
	}
}

// TestWriteBindingSidecar_DumpsREADY verifies WriteBindingSidecar dumps only READY instances.
func TestWriteBindingSidecar_DumpsREADY(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "dump-sidecar.json")

	// Build a ServiceRegistry with mixed states.
	instReady := TestServiceInstance("wf-dump", "svc-ready", StateReady, "http://endpoint:8080", "cap-dump-1", []string{"tool_x"})
	instReady.RunID = "run-ready-1"
	instReady.NetworkAlias = "alias-dump"
	instReady.BundleDigest = "digest-dump"

	instDeclared := TestServiceInstance("wf-dump", "svc-declared", StateDeclared, "http://endpoint:8081", "cap-dump-2", []string{"tool_y"})

	reg := TestServiceRegistry([]*ServiceInstance{instReady, instDeclared})

	if err := reg.WriteBindingSidecar(path, "wf-dump"); err != nil {
		t.Fatalf("WriteBindingSidecar error = %v", err)
	}

	// Read back and verify only READY.
	sc, err := ReadMCPBindingSidecar(path)
	if err != nil {
		t.Fatalf("ReadMCPBindingSidecar error = %v", err)
	}
	if sc.WorkflowID != "wf-dump" {
		t.Fatalf("WorkflowID = %q, want wf-dump", sc.WorkflowID)
	}
	if len(sc.Bindings) != 1 {
		t.Fatalf("len(Bindings) = %d, want 1 (only READY)", len(sc.Bindings))
	}
	if sc.Bindings[0].BindingID != "svc-ready" {
		t.Fatalf("Bindings[0].BindingID = %q, want svc-ready", sc.Bindings[0].BindingID)
	}
	if sc.Bindings[0].State != "READY" {
		t.Fatalf("Bindings[0].State = %q, want READY", sc.Bindings[0].State)
	}
	if sc.Bindings[0].Endpoint != "http://endpoint:8080" {
		t.Fatalf("Bindings[0].Endpoint = %q, want http://endpoint:8080", sc.Bindings[0].Endpoint)
	}
}