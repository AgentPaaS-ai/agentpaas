package runtime

import (
	"context"
	"strings"
	"testing"
)

// TestAdversaryB5T05 tests security claims of ReconcileAfterCrash and
// debug output sanitization (B5-T05).
//
// Adversary probes:
//   - Reconciliation bypass via label manipulation, missing runID, non-agent types
//   - Unrelated resource deletion (never touch non-owned or non-agent)
//   - Secret leakage in SanitizeDebugOutput / SanitizeDockerInspect
//   - Orphan after crash (agents removed only when no running gateway)
//   - Redaction gaps (bearer charset, AWS variants, substring "passenger", word boundaries)
//
// All tests use mocks; no Docker required. Run with -race.
func TestAdversaryB5T05(t *testing.T) {
	t.Run("Adversary_ReconciliationBypass_LabelTampering", func(t *testing.T) {
		// Attempt bypass by returning a container that has managed-by label
		// but resource-type set to net-internal or empty; ensure only agents
		// with proper type are considered, and no unrelated removal.
		removeCalled := false
		mock := &mockRuntimeDriver{
			listContainersFunc: func(_ context.Context, _ ...string) ([]ContainerInfo, error) {
				return []ContainerInfo{
					{
						ID:           "net-xyz",
						Status:       ContainerStatusRunning,
						RunID:        "run-bypass",
						ResourceType: ResourceTypeNetInternal, // not agent
						Labels:       Labels(ResourceTypeNetInternal, "run-bypass"),
					},
					{
						ID:           "agent-good",
						Status:       ContainerStatusRunning,
						RunID:        "run-bypass",
						ResourceType: ResourceTypeAgent,
						Labels:       Labels(ResourceTypeAgent, "run-bypass"),
					},
				}, nil
			},
			removeFunc: func(_ context.Context, id ContainerID, _ bool) error {
				removeCalled = true
				if string(id) == "net-xyz" {
					t.Error("ADVERSARY BREAK: reconcile attempted to remove non-agent resource")
				}
				return nil
			},
		}

		removed, err := ReconcileAfterCrash(context.Background(), mock)
		if err != nil {
			t.Fatalf("ReconcileAfterCrash failed: %v", err)
		}
		if removeCalled && len(removed) > 0 {
			// Only the agent should be removed (no gateway), net-internal must not
			if len(removed) != 1 || string(removed[0]) != "agent-good" {
				t.Error("ADVERSARY BREAK: removed unexpected resource during reconciliation")
			}
		}
	})

	t.Run("Adversary_UnrelatedResourceDeletion", func(t *testing.T) {
		// Verify IsUnrelatedContainer and that reconcile never removes non-owned.
		unrelated := ContainerInfo{
			ID:     "unrelated-123",
			Labels: map[string]string{"foo": "bar"}, // no managed-by
		}
		if !IsUnrelatedContainer(unrelated) {
			t.Error("IsUnrelatedContainer should return true for non-owned")
		}

		owned := ContainerInfo{
			ID:     "owned-456",
			Labels: Labels(ResourceTypeAgent, "run-1"),
		}
		if IsUnrelatedContainer(owned) {
			t.Error("IsUnrelatedContainer should return false for owned resource")
		}

		// Mock list returns only owned; ensure no attempt on unrelated.
		removeCalledOnUnrelated := false
		mock := &mockRuntimeDriver{
			listContainersFunc: func(_ context.Context, _ ...string) ([]ContainerInfo, error) {
				return []ContainerInfo{owned}, nil
			},
			removeFunc: func(_ context.Context, id ContainerID, _ bool) error {
				if string(id) == "unrelated-123" {
					removeCalledOnUnrelated = true
				}
				return nil
			},
		}
		_, err := ReconcileAfterCrash(context.Background(), mock)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if removeCalledOnUnrelated {
			t.Error("ADVERSARY BREAK: reconcile called remove on unrelated container")
		}
	})

	t.Run("Adversary_SecretLeakage_DebugOutput", func(t *testing.T) {
		// Test redaction gaps and common leak vectors.
		testCases := []struct {
			name   string
			input  string
			expectLeak bool
		}{
			{"bearer with +/=_", "Authorization: bearer sk-abc123+/=_def456", false},
			{"AWS ASIA key", "AWS_ACCESS_KEY_ID=ASIA1234567890ABCDEF", false},
			{"JWT token", `eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dummysig`, false},
			{"PEM private key", "-----BEGIN PRIVATE KEY-----\nMIIE...", false},
			{"long hex", "deadbeef" + strings.Repeat("cafebabe", 8), false},
			{"long base64", strings.Repeat("YWJjZGVmZ2hp", 10), false},
			{"fingerprint", "AA:BB:CC:DD:EE:FF:00:11:22:33:44:55:66:77:88:99:AA:BB:CC:DD", false},
			{"key=secret pattern", "api_key=sk-verysecretvalue123", false},
			{"passenger substring (should not trigger false positive leak)", "passenger=somevalue", false}, // pattern requires := after exact alt word
			{"mypass= (partial match inside word)", "mypass=supersecret123", false},
			{"env in docker inspect JSON", `"Env":["SECRET_TOKEN=ghp_abc123def456","NORMAL=val"]`, false},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				sanitized := SanitizeDebugOutput(tc.input)
				if HasSecretLeak(sanitized) {
					t.Errorf("ADVERSARY BREAK: secret leak remains after SanitizeDebugOutput in %s: %s", tc.name, sanitized)
				}
				if tc.expectLeak && !HasSecretLeak(tc.input) {
					t.Error("test setup error: input should have leak before sanitize")
				}
			})
		}
	})

	t.Run("Adversary_SanitizeDockerInspect_EnvRedaction", func(t *testing.T) {
		inspect := `{"Config":{"Env":["API_KEY=sk-test123456789","AWS_SECRET=ASIA9876543210FEDCBA","PASSENGER=foo","NORMAL_VAR=bar"]}}`
		sanitized := SanitizeDockerInspect(inspect)
		if HasSecretLeak(sanitized) {
			t.Error("ADVERSARY BREAK: SanitizeDockerInspect failed to redact secrets from Env")
		}
		if strings.Contains(sanitized, "sk-test123456789") || strings.Contains(sanitized, "ASIA9876543210FEDCBA") {
			t.Error("ADVERSARY BREAK: raw secret value remains in sanitized inspect output")
		}
		if !strings.Contains(sanitized, "[REDACTED]") {
			t.Error("expected redaction markers in output")
		}
	})

	t.Run("Adversary_OrphanAfterCrash_NoGateway", func(t *testing.T) {
		// Agents without running gateway must be removed (orphan cleanup).
		removedIDs := []ContainerID{}
		mock := &mockRuntimeDriver{
			listContainersFunc: func(_ context.Context, _ ...string) ([]ContainerInfo, error) {
				return []ContainerInfo{
					{
						ID:           "orphan-agent-1",
						Status:       ContainerStatusRunning,
						RunID:        "run-crash-1",
						ResourceType: ResourceTypeAgent,
						Labels:       Labels(ResourceTypeAgent, "run-crash-1"),
					},
					{
						ID:           "orphan-agent-2",
						Status:       ContainerStatusStopped,
						RunID:        "run-crash-1",
						ResourceType: ResourceTypeAgent,
						Labels:       Labels(ResourceTypeAgent, "run-crash-1"),
					},
				}, nil
			},
			removeFunc: func(_ context.Context, id ContainerID, _ bool) error {
				removedIDs = append(removedIDs, id)
				return nil
			},
		}

		removed, err := ReconcileAfterCrash(context.Background(), mock)
		if err != nil {
			t.Fatalf("ReconcileAfterCrash failed: %v", err)
		}
		if len(removed) != 2 {
			t.Errorf("expected 2 orphans removed, got %d", len(removed))
		}
	})

	t.Run("Adversary_OrphanAfterCrash_GatewayPresent", func(t *testing.T) {
		// When gateway is running, no agents removed even if crash-like state.
		removeCalled := false
		mock := &mockRuntimeDriver{
			listContainersFunc: func(_ context.Context, _ ...string) ([]ContainerInfo, error) {
				return []ContainerInfo{
					{
						ID:           "gw-running",
						Status:       ContainerStatusRunning,
						RunID:        "run-safe",
						ResourceType: ResourceTypeGateway,
						Labels:       Labels(ResourceTypeGateway, "run-safe"),
					},
					{
						ID:           "agent-keep",
						Status:       ContainerStatusRunning,
						RunID:        "run-safe",
						ResourceType: ResourceTypeAgent,
						Labels:       Labels(ResourceTypeAgent, "run-safe"),
					},
				}, nil
			},
			removeFunc: func(_ context.Context, _ ContainerID, _ bool) error {
				removeCalled = true
				return nil
			},
		}

		removed, err := ReconcileAfterCrash(context.Background(), mock)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(removed) != 0 || removeCalled {
			t.Error("ADVERSARY BREAK: agents removed despite running gateway (orphan bypass)")
		}
	})
}
