package install

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/AgentPaaS-ai/agentpaas/internal/audit"
	"github.com/AgentPaaS-ai/agentpaas/internal/pack"
)

func adversaryAssertTargetUnmaterialized(t *testing.T, target string) {
	t.Helper()
	info, err := os.Stat(target)
	if os.IsNotExist(err) {
		return
	}
	if err != nil {
		return
	}
	entries, err := os.ReadDir(target)
	if err != nil {
		t.Fatalf("read target dir: %v", err)
	}
	if len(entries) > 0 {
		names := make([]string, len(entries))
		for i, e := range entries {
			names[i] = e.Name()
		}
		t.Fatalf("target %q must have no partial writes; entries=%v mode=%#o", target, names, info.Mode().Perm())
	}
}

func adversaryAssertNoForkAudit(t *testing.T, records []audit.AuditRecord) {
	t.Helper()
	for _, r := range records {
		if r.EventType == audit.EventTypeAgentForked {
			t.Fatalf("must not emit agent_forked on failure: %+v", records)
		}
	}
}

func TestAdversaryForkVerifyRunsBeforeTargetWrites(t *testing.T) {
	stateRoot, dir, ref := materializeInstalledFixture(t)
	if err := os.WriteFile(filepath.Join(dir, installedPolicyName), append(mustRead(t, filepath.Join(dir, installedPolicyName)), 'X'), 0o600); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "precondition-order")
	var records []audit.AuditRecord
	app := &sliceAuditAppender{records: &records}
	if err := ForkInstalled(stateRoot, ref, target, app); err == nil {
		t.Fatal("want error")
	}
	adversaryAssertTargetUnmaterialized(t, target)
	adversaryAssertNoForkAudit(t, records)
}

func TestAdversaryForkTamperedLockRefused(t *testing.T) {
	stateRoot, dir, ref := materializeInstalledFixture(t)
	lockPath := filepath.Join(dir, installedLockName)
	raw := mustRead(t, lockPath)
	if err := os.WriteFile(lockPath, append(raw, 0xff), 0o600); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "lock-tamper")
	err := ForkInstalled(stateRoot, ref, target, nil)
	if err == nil {
		t.Fatal("want error")
	}
	if !errors.Is(err, ErrInstallVerifyFailed) {
		t.Fatalf("want verify failure, got %v", err)
	}
	adversaryAssertTargetUnmaterialized(t, target)
}

func TestAdversaryForkTamperedSourceRefused(t *testing.T) {
	stateRoot, dir, ref := materializeInstalledFixture(t)
	src := filepath.Join(dir, installedSourceDir, "main.py")
	if err := os.WriteFile(src, []byte("tampered\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "source-tamper")
	if err := ForkInstalled(stateRoot, ref, target, nil); err == nil {
		t.Fatal("want error")
	}
	adversaryAssertTargetUnmaterialized(t, target)
}

func TestAdversaryForkTamperedPolicyRefusedNoAuditForked(t *testing.T) {
	stateRoot, dir, ref := materializeInstalledFixture(t)
	if err := os.WriteFile(filepath.Join(dir, installedPolicyName), []byte("egress: []\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "policy-tamper")
	var records []audit.AuditRecord
	app := &sliceAuditAppender{records: &records}
	if err := ForkInstalled(stateRoot, ref, target, app); err == nil {
		t.Fatal("want error")
	}
	adversaryAssertTargetUnmaterialized(t, target)
	adversaryAssertNoForkAudit(t, records)
}

func TestAdversaryForkNonEmptyTargetHiddenFileRefused(t *testing.T) {
	stateRoot, _, ref := materializeInstalledFixture(t)
	target := filepath.Join(t.TempDir(), "hidden")
	if err := os.MkdirAll(target, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, ".DS_Store"), []byte("x"), 0o600); err != nil {
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

func TestAdversaryForkNonEmptyTargetSubdirRefused(t *testing.T) {
	stateRoot, _, ref := materializeInstalledFixture(t)
	target := filepath.Join(t.TempDir(), "subdir-only")
	if err := os.MkdirAll(filepath.Join(target, "child"), 0o700); err != nil {
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

func TestAdversaryForkEmptyExistingTargetSucceeds(t *testing.T) {
	stateRoot, _, ref := materializeInstalledFixture(t)
	target := filepath.Join(t.TempDir(), "empty-existing")
	if err := os.MkdirAll(target, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := ForkInstalled(stateRoot, ref, target, nil); err != nil {
		t.Fatalf("ForkInstalled: %v", err)
	}
	if _, err := os.Stat(filepath.Join(target, "agent.yaml")); err != nil {
		t.Fatalf("agent.yaml: %v", err)
	}
}

func TestAdversaryForkLineageIntegrity(t *testing.T) {
	stateRoot, installedDir, ref := materializeInstalledFixture(t)
	target := filepath.Join(t.TempDir(), "lineage-check")
	if err := ForkInstalled(stateRoot, ref, target, nil); err != nil {
		t.Fatal(err)
	}
	lock, err := pack.ReadAgentLock(filepath.Join(installedDir, installedLockName))
	if err != nil {
		t.Fatal(err)
	}
	policyInstalled := mustRead(t, filepath.Join(installedDir, installedPolicyName))
	parentRaw := mustRead(t, filepath.Join(installedDir, installedParentBundleRef))
	var parentBundle ParentBundleRef
	if err := json.Unmarshal(parentRaw, &parentBundle); err != nil {
		t.Fatal(err)
	}
	lineage, err := ReadForkLineage(target)
	if err != nil {
		t.Fatal(err)
	}
	if lineage.Parent.LockDigest != pack.LockDigest(lock) {
		t.Fatalf("lock_digest got %q want %q", lineage.Parent.LockDigest, pack.LockDigest(lock))
	}
	if lineage.Parent.BundleDigest != parentBundle.Digest {
		t.Fatalf("bundle_digest got %q want %q", lineage.Parent.BundleDigest, parentBundle.Digest)
	}
	decoded, err := base64.StdEncoding.DecodeString(lineage.Parent.PolicyYAMLB64)
	if err != nil {
		t.Fatal(err)
	}
	if string(decoded) != string(policyInstalled) {
		t.Fatal("policy_yaml_b64 decode not byte-equal to installed policy")
	}
	lockProv, _ := json.Marshal(lock.Provenance)
	lineageProv, _ := json.Marshal(lineage.Parent.Provenance)
	if string(lockProv) != string(lineageProv) {
		t.Fatalf("provenance not byte-equal:\nlock %s\nlineage %s", lockProv, lineageProv)
	}
	if len(lineage.Parent.Provenance) != len(lock.Provenance) {
		t.Fatalf("provenance count %d want %d", len(lineage.Parent.Provenance), len(lock.Provenance))
	}
}

func TestAdversaryForkSourceCopyExactNoInstallArtifacts(t *testing.T) {
	stateRoot, installedDir, ref := materializeInstalledFixture(t)
	target := filepath.Join(t.TempDir(), "source-exact")
	if err := ForkInstalled(stateRoot, ref, target, nil); err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{installedLockName, installedManifestName, installedParentBundleRef, forkLineageFileName, installedPolicyName} {
		if forbidden == installedPolicyName || forbidden == forkLineageFileName {
			continue
		}
		if _, err := os.Stat(filepath.Join(target, forbidden)); err == nil {
			t.Fatalf("install artifact %q must not appear in fork root", forbidden)
		}
	}
	var fileCount int
	err := filepath.WalkDir(filepath.Join(installedDir, installedSourceDir), func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil || d.IsDir() {
			return walkErr
		}
		fileCount++
		rel, err := filepath.Rel(filepath.Join(installedDir, installedSourceDir), path)
		if err != nil {
			return err
		}
		want := mustRead(t, path)
		got := mustRead(t, filepath.Join(target, rel))
		if string(got) != string(want) {
			return fmt.Errorf("file %s content mismatch", rel)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if fileCount == 0 {
		t.Fatal("fixture must have source files")
	}
	agentInstalled := mustRead(t, filepath.Join(installedDir, installedSourceDir, "agent.yaml"))
	agentFork := mustRead(t, filepath.Join(target, "agent.yaml"))
	if string(agentFork) != string(agentInstalled) {
		t.Fatal("agent.yaml not byte-equal")
	}
}

func TestAdversaryForkPolicyBytesExactNotReserialized(t *testing.T) {
	stateRoot, installedDir, ref := materializeInstalledFixture(t)
	target := filepath.Join(t.TempDir(), "policy-bytes")
	if err := ForkInstalled(stateRoot, ref, target, nil); err != nil {
		t.Fatal(err)
	}
	installed := mustRead(t, filepath.Join(installedDir, installedPolicyName))
	forked := mustRead(t, filepath.Join(target, installedPolicyName))
	if string(forked) != string(installed) {
		t.Fatal("policy.yaml must be byte-equal copy")
	}
}

func TestAdversaryForkSuccessWritesAgentForkedAudit(t *testing.T) {
	stateRoot, _, ref := materializeInstalledFixture(t)
	target := filepath.Join(t.TempDir(), "audit-ok")
	var records []audit.AuditRecord
	app := &sliceAuditAppender{records: &records}
	if err := ForkInstalled(stateRoot, ref, target, app); err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].EventType != audit.EventTypeAgentForked {
		t.Fatalf("audit = %+v", records)
	}
}

func TestAdversaryForkSymlinkInInstalledSourceRefused(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink test skipped on windows")
	}
	stateRoot, dir, ref := materializeInstalledFixture(t)
	outside := filepath.Join(t.TempDir(), "outside-secret")
	if err := os.WriteFile(outside, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, installedSourceDir, "evil-link")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "symlink-fork")
	if err := ForkInstalled(stateRoot, ref, target, nil); err == nil {
		t.Fatal("want error")
	}
	adversaryAssertTargetUnmaterialized(t, target)
	if _, err := os.Stat(filepath.Join(target, "evil-link")); err == nil {
		t.Fatal("must not copy symlink escape into target")
	}
}

func TestAdversaryForkLineageJsonMode0600(t *testing.T) {
	stateRoot, _, ref := materializeInstalledFixture(t)
	target := filepath.Join(t.TempDir(), "perms")
	if err := ForkInstalled(stateRoot, ref, target, nil); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(target, forkLineageFileName))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("lineage.json mode = %#o want 0600", info.Mode().Perm())
	}
	dirInfo, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if dirInfo.Mode().Perm() != 0o700 {
		t.Fatalf("target dir mode = %#o want 0700", dirInfo.Mode().Perm())
	}
}

func TestAdversaryForkDoubleForkIndependent(t *testing.T) {
	stateRoot, installedDir, ref := materializeInstalledFixture(t)
	base := t.TempDir()
	target1 := filepath.Join(base, "fork-a")
	target2 := filepath.Join(base, "fork-b")
	if err := ForkInstalled(stateRoot, ref, target1, nil); err != nil {
		t.Fatal(err)
	}
	if err := ForkInstalled(stateRoot, ref, target2, nil); err != nil {
		t.Fatal(err)
	}
	if string(mustRead(t, filepath.Join(target1, "main.py"))) != string(mustRead(t, filepath.Join(target2, "main.py"))) {
		t.Fatal("both forks should have same source")
	}
	installedBefore := string(mustRead(t, filepath.Join(installedDir, installedSourceDir, "main.py")))
	if err := os.WriteFile(filepath.Join(target1, "main.py"), []byte("only-a\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if string(mustRead(t, filepath.Join(installedDir, installedSourceDir, "main.py"))) != installedBefore {
		t.Fatal("modifying one fork must not change installed source")
	}
}

func TestAdversaryForkModifyTargetDoesNotAffectInstalled(t *testing.T) {
	stateRoot, installedDir, ref := materializeInstalledFixture(t)
	before := mustRead(t, filepath.Join(installedDir, installedPolicyName))
	target := filepath.Join(t.TempDir(), "independent")
	if err := ForkInstalled(stateRoot, ref, target, nil); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, installedPolicyName), []byte("fork-only: true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	after := mustRead(t, filepath.Join(installedDir, installedPolicyName))
	if string(after) != string(before) {
		t.Fatal("tampering fork target policy must not change installed policy")
	}
}

func TestAdversaryForkTargetDirSymlinkRefused(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink test skipped on windows")
	}
	stateRoot, _, ref := materializeInstalledFixture(t)
	realDir := filepath.Join(t.TempDir(), "real-empty")
	if err := os.MkdirAll(realDir, 0o700); err != nil {
		t.Fatal(err)
	}
	linkTarget := filepath.Join(t.TempDir(), "link-as-target")
	if err := os.Symlink(realDir, linkTarget); err != nil {
		t.Fatal(err)
	}
	err := ForkInstalled(stateRoot, ref, linkTarget, nil)
	if err == nil {
		t.Fatal("want error")
	}
	if !errors.Is(err, ErrForkRefused) {
		t.Fatalf("err = %v", err)
	}
}

func TestAdversaryForkTargetPathWithDotDotResolvesSafely(t *testing.T) {
	stateRoot, _, ref := materializeInstalledFixture(t)
	base := filepath.Join(t.TempDir(), "nested", "child")
	if err := os.MkdirAll(base, 0o700); err != nil {
		t.Fatal(err)
	}
	// Resolve to sibling empty dir via ..
	sibling := filepath.Join(filepath.Dir(base), "fork-sibling")
	target := filepath.Join(base, "..", "fork-sibling")
	if err := ForkInstalled(stateRoot, ref, target, nil); err != nil {
		t.Fatalf("ForkInstalled: %v", err)
	}
	absSibling, _ := filepath.Abs(sibling)
	absGot, _ := filepath.Abs(filepath.Join(target, "lineage.json"))
	absWant, _ := filepath.Abs(filepath.Join(absSibling, "lineage.json"))
	if absGot != absWant {
		t.Fatalf("fork landed at %q want under %q", absGot, absWant)
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return b
}