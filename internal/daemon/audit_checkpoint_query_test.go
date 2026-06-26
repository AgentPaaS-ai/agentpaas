package daemon

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	controlv1 "github.com/parvezsyed/agentpaas/api/control/v1"
	"github.com/parvezsyed/agentpaas/internal/audit"
	"github.com/parvezsyed/agentpaas/internal/home"
)

func TestAuditQuery_IncludesTailTruncationInChainVerification(t *testing.T) {
	dir := t.TempDir()
	hp := home.NewHomePaths(dir)
	if err := home.Ensure(hp); err != nil {
		t.Fatalf("home.Ensure: %v", err)
	}

	auditPath := filepath.Join(hp.State, "audit.jsonl")
	cpPath := filepath.Join(hp.State, "audit.jsonl.checkpoints")
	keyDER, pubKey, err := audit.GenerateCheckpointKey()
	if err != nil {
		t.Fatalf("GenerateCheckpointKey: %v", err)
	}

	w, err := audit.NewAuditWriterWithCheckpoints(auditPath, cpPath, 5, keyDER)
	if err != nil {
		t.Fatalf("NewAuditWriterWithCheckpoints: %v", err)
	}
	for i := 0; i < 10; i++ {
		rec := operatorTestRecord("egress_allowed", "run-trunc", map[string]interface{}{"i": i})
		if err := w.Append(rec); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close writer: %v", err)
	}

	data, err := os.ReadFile(auditPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	truncated := strings.Join(lines[:7], "\n") + "\n"
	if err := os.WriteFile(auditPath, []byte(truncated), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	indexer, err := audit.NewSQLiteIndexer(filepath.Join(hp.State, "audit.db"))
	if err != nil {
		t.Fatalf("NewSQLiteIndexer: %v", err)
	}
	t.Cleanup(func() { _ = indexer.Close() })
	if err := indexer.Rebuild(auditPath); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}

	server := &controlServer{
		auditIndex:            indexer,
		homePaths:             hp,
		auditCheckpointPubKey: pubKey,
		auditCheckpointsPath:  cpPath,
	}

	resp, err := server.AuditQuery(context.Background(), &controlv1.AuditQueryRequest{PageSize: 50})
	if err != nil {
		t.Fatalf("AuditQuery: %v", err)
	}
	verification := resp.GetChainVerification()
	if verification == nil {
		t.Fatal("expected chain_verification in AuditQuery response")
	}
	found := false
	for _, issue := range verification.GetIssues() {
		if issue.GetType() == string(audit.ErrTypeTailTruncation) {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected tail_truncation issue in response, got %+v", verification.GetIssues())
	}
	if verification.GetVerified() {
		t.Fatal("expected verified=false when tail truncated")
	}
}