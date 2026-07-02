package redteam

import (
	"context"
	"encoding/json"
	"fmt"
	goruntime "runtime"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/parvezsyed/agentpaas/internal/audit"
	docker "github.com/parvezsyed/agentpaas/internal/runtime"
)

// requireDocker skips the test if AGENTPAAS_DOCKER_TESTS is not set.
func requireDocker(t *testing.T) {
	t.Helper()
	if os.Getenv("AGENTPAAS_DOCKER_TESTS") != "1" {
		t.Skip("set AGENTPAAS_DOCKER_TESTS=1 to run red-team smoke tests")
	}
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not available")
	}
}

// uniqueRunID generates a unique run ID for red-team fixtures to avoid
// resource name collisions across parallel test runs.
func uniqueRunID(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
}

// dockerExec runs a command inside a running Docker container.
func dockerExec(ctx context.Context, containerID string, args ...string) (string, error) {
	cmdArgs := append([]string{"exec", containerID}, args...)
	cmd := exec.CommandContext(ctx, "docker", cmdArgs...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// cleanupContainers removes the given containers (force).
func cleanupContainers(ctx context.Context, dr *docker.DockerRuntime, ids ...docker.ContainerID) {
	for _, id := range ids {
		if id != "" {
			_ = dr.Remove(ctx, id, true)
		}
	}
}

// cleanupNetworks removes the given networks.
func cleanupNetworks(ctx context.Context, dr *docker.DockerRuntime, ids ...docker.NetworkID) {
	for _, id := range ids {
		if id != "" {
			_ = dr.RemoveNetwork(ctx, id)
		}
	}
}

// topologyT is the minimal subset of testing.TB that createTopology needs.
// Both *testing.T and *fixtureT satisfy this interface.
type topologyT interface {
	Helper()
	Fatalf(format string, args ...any)
}

// createTopology creates the standard AgentPaaS network topology:
// internal (no-egress) bridge + egress bridge + dual-homed gateway +
// agent on internal-only. Returns all resources for cleanup.
// The gateway image should be a simple alpine:latest with sleep.
func createTopology(ctx context.Context, t topologyT, dr *docker.DockerRuntime, runID string) (
	internalNetID, egressNetID docker.NetworkID,
	gatewayID, agentID docker.ContainerID,
) {
	t.Helper()

	// Internal bridge (no external access)
	internalNetID, err := dr.CreateNetwork(ctx, docker.NetworkSpec{
		Name:     docker.NetworkName("internal", runID),
		Internal: true,
		Labels:   docker.Labels(docker.ResourceTypeNetInternal, runID),
	})
	if err != nil {
		t.Fatalf("CreateNetwork(internal): %v", err)
	}

	// Egress bridge (has external access)
	egressNetID, err = dr.CreateNetwork(ctx, docker.NetworkSpec{
		Name:     docker.NetworkName("egress", runID),
		Internal: false,
		Labels:   docker.Labels(docker.ResourceTypeNetEgress, runID),
	})
	if err != nil {
		t.Fatalf("CreateNetwork(egress): %v", err)
	}

	// Gateway: dual-homed (internal + egress)
	gatewayID, err = dr.Create(ctx, docker.ContainerSpec{
		Image:      "alpine:latest",
		Command:    []string{"sleep", "3600"},
		NetworkIDs: []string{string(internalNetID), string(egressNetID)},
		Labels:     docker.Labels(docker.ResourceTypeGateway, runID),
	})
	if err != nil {
		t.Fatalf("Create(gateway): %v", err)
	}

	// Agent: internal-only (no egress)
	agentID, err = dr.Create(ctx, docker.ContainerSpec{
		Image:      "alpine:latest",
		Command:    []string{"sleep", "3600"},
		NetworkIDs: []string{string(internalNetID)},
		Labels:     docker.Labels(docker.ResourceTypeAgent, runID),
	})
	if err != nil {
		t.Fatalf("Create(agent): %v", err)
	}

	if err := dr.Start(ctx, gatewayID); err != nil {
		t.Fatalf("Start(gateway): %v", err)
	}
	if err := dr.Start(ctx, agentID); err != nil {
		t.Fatalf("Start(agent): %v", err)
	}

	// Allow containers to initialize networking
	time.Sleep(2 * time.Second)

	return internalNetID, egressNetID, gatewayID, agentID
}

// probeBlocked runs a command in the agent container and returns true if
// the probe was BLOCKED (command failed or timed out).
// (Reserved for future fixtures that need a boolean probe helper.)

// auditContainsEvent checks if the audit writer contains an event of the
// given type with matching payload key-values.
// (Reserved for future fixtures that need structured audit assertions.)

// marshalPayload converts an audit payload to a JSON string for inspection.
func marshalPayload(payload map[string]interface{}) string {
	data, _ := json.Marshal(payload)
	return string(data)
}

// skipOnPlatform skips the test on non-macOS (P1 target is macOS).
func skipOnPlatform(t *testing.T) {
	t.Helper()
	if goruntime.GOOS != "darwin" {
		t.Skip("P1 red-team is macOS-only; Linux certification is P2")
	}
}

// fixtureT adapts a FixtureResult to the testing.TB interface so that
// createTopology and similar helpers can use t.Fatalf/t.Helper. When a
// fixture runs outside a *testing.T context (e.g. via Runner.RunAll),
// fatal errors are recorded in the result instead of panicking.
type fixtureT struct {
	result *FixtureResult
}

func (f *fixtureT) Helper()         {}
func (f *fixtureT) Cleanup(_ func()) {}
func (f *fixtureT) TempDir() string  { return "" }
func (f *fixtureT) Logf(format string, args ...any) {}
func (f *fixtureT) Logf_(format string, args ...any) {}

// Fatalf records the failure in the fixture result. Since fixtures run
// in their own goroutine via Runner.RunAll, we can't actually fatal-exit;
// instead we set the result detail and panic to unwind.
func (f *fixtureT) Fatalf(format string, args ...any) {
	if f.result != nil {
		f.result.Detail = fmt.Sprintf(format, args...)
	}
	panic(fmt.Sprintf(format, args...))
}

func (f *fixtureT) Errorf(format string, args ...any) {
	if f.result != nil {
		f.result.Detail = fmt.Sprintf(format, args...)
	}
}

func (f *fixtureT) Skipf(format string, args ...any) {
	if f.result != nil {
		f.result.Status = "SKIP"
		f.result.Detail = fmt.Sprintf(format, args...)
	}
	panic(fmt.Sprintf(format, args...))
}

func (f *fixtureT) Failed() bool  { return false }
func (f *fixtureT) Skipped() bool { return false }
func (f *fixtureT) Name() string  { return "fixture" }

// recoverFixture catches panics from fixtureT.Fatalf and converts them
// to a failed result instead of crashing the test process.
func recoverFixture(result *FixtureResult) {
	if r := recover(); r != nil {
		if result.Status != "SKIP" {
			result.Status = "FAIL"
		}
		if result.Containment == "" {
			result.Containment = "LEAKED"
		}
		if result.Detail == "" {
			result.Detail = fmt.Sprintf("%v", r)
		}
	}
}

// tempAuditDir creates a temporary directory for audit logs.
// Fixtures run outside a *testing.T context use tempAuditDirSimple instead.

// tempAuditDirSimple creates a temporary directory without *testing.T.
// Used by fixtures that run outside a test context.
func tempAuditDirSimple() string {
	dir, err := os.MkdirTemp("", "agentpaas-redteam-audit-*")
	if err != nil {
		return "/tmp/agentpaas-redteam-audit"
	}
	// Resolve symlinks to avoid macOS /var -> /private/var issues
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		return resolved
	}
	return dir
}

// readAuditRecords reads all audit records from a JSONL file using the
// SQLiteIndexer Rebuild path.
func readAuditRecords(auditPath string) ([]audit.AuditRecord, error) {
	dbPath := auditPath + ".db"
	defer func() { _ = os.Remove(dbPath) }()
	ix, err := audit.NewSQLiteIndexer(dbPath)
	if err != nil {
		return nil, fmt.Errorf("NewSQLiteIndexer: %w", err)
	}
	defer func() { _ = ix.Close() }()
	if err := ix.Rebuild(auditPath); err != nil {
		// Rebuild returns chain verification errors but still imports records
		// up to the break. Records may still be available.
		_ = err
	}
	count, _ := ix.RecordCount()
	if count == 0 {
		return nil, fmt.Errorf("no records indexed")
	}
	// Read all records by querying all event types
	var records []audit.AuditRecord
	for _, eventType := range []string{
		"egress_denied", "policy_denied", "secret_injected",
		"secret_leased", "secret_read", "run_start", "run_failed",
		"run_complete", "invoke",
	} {
		recs, err := ix.QueryByEventType(eventType, 100)
		if err == nil {
			records = append(records, recs...)
		}
	}
	return records, nil
}

// containsAny checks if the string contains any of the substrings.
func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
