package install

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/AgentPaaS-ai/agentpaas/internal/audit"
	"github.com/AgentPaaS-ai/agentpaas/internal/pack"
)

func TestForkInstalled_MaterializedAgent(t *testing.T) {
	stateRoot, installedDir, ref := materializeInstalledFixture(t)
	target := filepath.Join(t.TempDir(), "fork-project")

	var records []audit.AuditRecord
	appender := &sliceAuditAppender{records: &records}

	if err := ForkInstalled(stateRoot, ref, target, appender); err != nil {
		t.Fatalf("ForkInstalled: %v", err)
	}

	if _, err := os.Stat(filepath.Join(target, "agent.yaml")); err != nil {
		t.Fatalf("agent.yaml: %v", err)
	}
	if _, err := os.Stat(filepath.Join(target, "main.py")); err != nil {
		t.Fatalf("main.py: %v", err)
	}

	lock, err := pack.ReadAgentLock(filepath.Join(installedDir, installedLockName))
	if err != nil {
		t.Fatalf("read lock: %v", err)
	}
	policyInstalled, err := os.ReadFile(filepath.Join(installedDir, installedPolicyName))
	if err != nil {
		t.Fatal(err)
	}
	policyFork, err := os.ReadFile(filepath.Join(target, installedPolicyName))
	if err != nil {
		t.Fatal(err)
	}
	if string(policyFork) != string(policyInstalled) {
		t.Fatal("policy.yaml not byte-equal")
	}

	lineage, err := ReadForkLineage(target)
	if err != nil {
		t.Fatalf("lineage: %v", err)
	}
	if lineage.Version != 1 {
		t.Fatalf("version = %d", lineage.Version)
	}
	if lineage.Parent.AgentName != lock.AgentName {
		t.Fatalf("agent_name = %q", lineage.Parent.AgentName)
	}
	if lineage.Parent.AgentVersion != lock.AgentVersion {
		t.Fatalf("agent_version = %q", lineage.Parent.AgentVersion)
	}
	if lineage.Parent.PublisherFingerprint != lock.Publisher.Fingerprint {
		t.Fatalf("publisher_fingerprint = %q", lineage.Parent.PublisherFingerprint)
	}
	if lineage.Parent.LockDigest != pack.LockDigest(lock) {
		t.Fatalf("lock_digest mismatch")
	}

	parentRaw, err := os.ReadFile(filepath.Join(installedDir, installedParentBundleRef))
	if err != nil {
		t.Fatal(err)
	}
	var parentBundle ParentBundleRef
	if err := json.Unmarshal(parentRaw, &parentBundle); err != nil {
		t.Fatal(err)
	}
	if lineage.Parent.BundleDigest != parentBundle.Digest {
		t.Fatalf("bundle_digest = %q want %q", lineage.Parent.BundleDigest, parentBundle.Digest)
	}
	if lineage.Parent.PolicyDigest != lock.PolicyDigest {
		t.Fatalf("policy_digest mismatch")
	}
	decoded, err := base64.StdEncoding.DecodeString(lineage.Parent.PolicyYAMLB64)
	if err != nil {
		t.Fatal(err)
	}
	if string(decoded) != string(policyInstalled) {
		t.Fatal("policy_yaml_b64 decode mismatch")
	}

	lockProv, err := json.Marshal(lock.Provenance)
	if err != nil {
		t.Fatal(err)
	}
	lineageProv, err := json.Marshal(lineage.Parent.Provenance)
	if err != nil {
		t.Fatal(err)
	}
	if string(lockProv) != string(lineageProv) {
		t.Fatalf("provenance mismatch:\nlock %s\nlineage %s", lockProv, lineageProv)
	}

	installedSourceDigest, err := pack.ComputeBuildInputDigest(filepath.Join(installedDir, installedSourceDir), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := filepath.WalkDir(filepath.Join(installedDir, installedSourceDir), func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(filepath.Join(installedDir, installedSourceDir), path)
		if err != nil {
			return err
		}
		want, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		got, err := os.ReadFile(filepath.Join(target, rel))
		if err != nil {
			return err
		}
		if string(got) != string(want) {
			return fmt.Errorf("file %s content mismatch", rel)
		}
		return nil
	}); err != nil {
		t.Fatalf("source files: %v", err)
	}
	if installedSourceDigest != lock.BuildInputDigest {
		t.Fatalf("installed source digest = %q lock = %q", installedSourceDigest, lock.BuildInputDigest)
	}

	if len(records) != 1 || records[0].EventType != audit.EventTypeAgentForked {
		t.Fatalf("audit = %+v", records)
	}
	if records[0].Payload["parent_ref"] != ref {
		t.Fatalf("audit parent_ref = %v", records[0].Payload["parent_ref"])
	}
}

func TestForkInstalled_TamperedPolicyRefused(t *testing.T) {
	stateRoot, dir, ref := materializeInstalledFixture(t)
	if err := os.WriteFile(filepath.Join(dir, installedPolicyName), []byte("egress: []\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "should-not-exist")
	if err := ForkInstalled(stateRoot, ref, target, nil); err == nil {
		t.Fatal("want error")
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		entries, _ := os.ReadDir(target)
		if len(entries) > 0 {
			t.Fatalf("target should be clean, entries=%d", len(entries))
		}
	}
}

func TestForkInstalled_NonEmptyTargetRefused(t *testing.T) {
	stateRoot, _, ref := materializeInstalledFixture(t)
	target := filepath.Join(t.TempDir(), "nonempty")
	if err := os.MkdirAll(target, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "marker"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := ForkInstalled(stateRoot, ref, target, nil)
	if err == nil {
		t.Fatal("want error")
	}
	if !errors.Is(err, ErrForkRefused) {
		t.Fatalf("err = %v", err)
	}
}

func TestForkInstalled_CreatesMissingTargetDir(t *testing.T) {
	stateRoot, _, ref := materializeInstalledFixture(t)
	target := filepath.Join(t.TempDir(), "nested", "fork")
	if err := ForkInstalled(stateRoot, ref, target, nil); err != nil {
		t.Fatalf("ForkInstalled: %v", err)
	}
	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o700 {
		t.Fatalf("mode = %#o", info.Mode().Perm())
	}
}

func TestForkInstalled_NotInstalledRef(t *testing.T) {
	stateRoot := filepath.Join(t.TempDir(), "state")
	if err := os.MkdirAll(stateRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "fork")
	err := ForkInstalled(stateRoot, "missing@deadbeef", target, nil)
	if err == nil {
		t.Fatal("want error")
	}
}