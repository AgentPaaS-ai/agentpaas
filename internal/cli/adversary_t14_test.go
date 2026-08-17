package cli

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ADVERSARY BREAK: SC4 is enforced only on get/inspect. list/search render
// whatever the server returns, including unknown schema_version on a
// future list row that carries the field.
func TestAdversaryT14_ListDoesNotRejectUnknownSchema(t *testing.T) {
	store := setupFakeTokenStore(t)
	if err := store.Set(context.Background(), "apc_adv_list_schema"); err != nil {
		t.Fatalf("store token: %v", err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"schema_version": "component-index/99",
			"components": []map[string]any{{
				"schema_version": "component-index/99",
				"id":             "img_future",
				"kind":           "mcp",
				"name":           "future-mcp",
				"version":        "9.9.9",
				"description":    "must not be silently rendered",
				"egress_count":   1,
			}},
		})
	}))
	defer func() { server.Close() }()
	t.Setenv("AGENTPAAS_CLOUD_API_URL", server.URL)

	stdout, stderr, err := executeCloudCmd(t, "", "cloud", "registry", "list")
	// ADVERSARY BREAK: list must fail-closed on unknown schema_version, not print the row.
	if err == nil {
		t.Fatalf("ADVERSARY BREAK: registry list rendered unknown schema_version; stdout=%q stderr=%q", stdout, stderr)
	}
	combined := err.Error() + stderr + stdout
	if !strings.Contains(combined, "schema_version") {
		t.Fatalf("ADVERSARY BREAK: list error must mention schema_version, got %s", combined)
	}
	if strings.Contains(stdout, "future-mcp") {
		t.Fatalf("ADVERSARY BREAK: list printed future-mcp under unknown schema: %q", stdout)
	}
}

// ADVERSARY BREAK: server-stamped component-index/1 (missing on lock) is
// accepted by CLI get/inspect — fail-open after cloud defaulting.
func TestAdversaryT14_GetAcceptsServerStampedV1(t *testing.T) {
	store := setupFakeTokenStore(t)
	if err := store.Set(context.Background(), "apc_adv_stamped"); err != nil {
		t.Fatalf("store token: %v", err)
	}
	// Simulate cloud projectComponentIndex defaulting a lock with no schema_version.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"schema_version": "component-index/1",
			"id":             "img_noschema",
			"kind":           "mcp",
			"name":           "noschema-mcp",
			"version":        "1.0.0",
			"description":    "stamped-by-server",
			"egress":         []string{"evil.example"},
			"_lock_had_schema_version": false,
		})
	}))
	defer func() { server.Close() }()
	t.Setenv("AGENTPAAS_CLOUD_API_URL", server.URL)

	stdout, stderr, err := executeCloudCmd(t, "", "cloud", "registry", "inspect", "noschema-mcp", "--json")
	if err != nil {
		t.Fatalf("inspect: err=%v stdout=%q stderr=%q", err, stdout, stderr)
	}
	// ADVERSARY BREAK: CLI must not treat a server-stamped v1 as authentic when
	// the lock omitted schema_version. Presence of the stamp-only card is the leak.
	if strings.Contains(stdout, "stamped-by-server") {
		t.Fatal("ADVERSARY BREAK: inspect silently rendered a card whose schema_version was stamped by the server, not the lock")
	}
}

func TestAdversaryT14_UnicodeHomoglyphSchemaRejected(t *testing.T) {
	store := setupFakeTokenStore(t)
	if err := store.Set(context.Background(), "apc_adv_homo"); err != nil {
		t.Fatalf("store token: %v", err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			// U+2010 hyphen instead of ASCII hyphen-minus
			"schema_version": "component‐index/1",
			"id":             "img_homo",
			"kind":           "mcp",
			"name":           "homo-mcp",
			"description":    "homoglyph",
			"egress":         []string{"api.example"},
		})
	}))
	defer func() { server.Close() }()
	t.Setenv("AGENTPAAS_CLOUD_API_URL", server.URL)

	_, stderr, err := executeCloudCmd(t, "", "cloud", "registry", "inspect", "homo-mcp")
	if err == nil {
		t.Fatal("expected reject of homoglyph schema_version")
	}
	combined := err.Error() + stderr
	if !strings.Contains(combined, "schema_version") {
		t.Fatalf("error = %s, want schema_version rejection", combined)
	}
}

func TestAdversaryT14_NumericSchemaRejected(t *testing.T) {
	store := setupFakeTokenStore(t)
	if err := store.Set(context.Background(), "apc_adv_num"); err != nil {
		t.Fatalf("store token: %v", err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"schema_version": 1,
			"id":             "img_num",
			"kind":           "agent",
			"name":           "num-agent",
			"description":    "numeric schema",
		})
	}))
	defer func() { server.Close() }()
	t.Setenv("AGENTPAAS_CLOUD_API_URL", server.URL)

	_, stderr, err := executeCloudCmd(t, "", "cloud", "registry", "get", "num-agent")
	if err == nil {
		t.Fatal("expected reject of numeric schema_version")
	}
	combined := err.Error() + stderr
	if !strings.Contains(combined, "schema_version") {
		t.Fatalf("error = %s, want schema_version rejection", combined)
	}
}
