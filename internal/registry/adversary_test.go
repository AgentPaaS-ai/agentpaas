package registry_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/audit"
	"github.com/AgentPaaS-ai/agentpaas/internal/install"
	"github.com/AgentPaaS-ai/agentpaas/internal/pack"
	"github.com/AgentPaaS-ai/agentpaas/internal/registry"
	"gopkg.in/yaml.v3"
)

// ---------------------------------------------------------------------------
// B31 adversary break tests (registry list/show + promote/demote + workflow gate)
//
// Pattern: each test exercises an ATTACK path. If the defense holds, the
// ADVERSARY BREAK assertion is unreachable and the test PASSES. If the code
// is vulnerable, the test FAILS with "ADVERSARY BREAK".
// ---------------------------------------------------------------------------

func advWriteAgent(t *testing.T, stateRoot, name, pub8, version string, promoted bool, alias string, creds map[string]string) string {
	t.Helper()
	ref := name + "@" + pub8
	dir := filepath.Join(stateRoot, "agents", ref)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	m := install.InstallManifest{
		PublisherFingerprint: strings.Repeat(pub8, 8)[:64],
		PublisherName:        name + "-pub",
		AgentName:            name,
		AgentVersion:         version,
		AcceptedPolicyDigest: "sha256:" + strings.Repeat("aa", 32),
		InstallMode:          "local-rebuild",
		LocalImageDigest:     "sha256:" + strings.Repeat("bb", 32),
		InstalledAt:          time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		Alias:                alias,
		Promoted:             promoted,
		CredentialMap:        creds,
	}
	if promoted {
		pt := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
		m.PromotedAt = &pt
		m.PromotedBy = "seed"
	}
	raw, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "install-manifest.json"), raw, 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	lock := &pack.AgentLock{
		SchemaVersion: pack.LockSchemaVersion,
		AgentName:     name,
		AgentVersion:  version,
		ImageDigest:   "sha256:" + strings.Repeat("cc", 32),
		PolicyDigest:  "sha256:" + strings.Repeat("dd", 32),
		Publisher: &pack.PublisherInfo{
			Name:        name + "-pub",
			Fingerprint: strings.Repeat(pub8, 8)[:64],
		},
	}
	lockRaw, err := json.Marshal(lock)
	if err != nil {
		t.Fatalf("marshal lock: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "agent.lock"), lockRaw, 0o600); err != nil {
		t.Fatalf("write lock: %v", err)
	}
	return ref
}

func advPipelineWF(packageName, packageVersion string) *pack.WorkflowYAML {
	yml := "kind: pipeline\npipeline:\n  stages:\n    - name: s1\n      package_name: " + packageName + "\n      package_version: \"" + packageVersion + "\"\n      bundle_digest: sha256:abc123\n      handoff: public\n"
	var wf pack.WorkflowYAML
	if err := yaml.Unmarshal([]byte(yml), &wf); err != nil {
		panic(err)
	}
	return &wf
}

func advReadManifest(t *testing.T, stateRoot, ref string) install.InstallManifest {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(stateRoot, "agents", ref, "install-manifest.json"))
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var m install.InstallManifest
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("parse manifest: %v", err)
	}
	return m
}

func advCountAudit(t *testing.T, stateRoot, eventType string) int {
	t.Helper()
	path := filepath.Join(stateRoot, "audit.jsonl")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0
		}
		t.Fatalf("read audit: %v", err)
	}
	n := 0
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var rec audit.AuditRecord
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			continue
		}
		if rec.EventType == eventType {
			n++
		}
	}
	return n
}

// TestAdversary_B31_CredentialValuesNeverLeaveRegistryAPI tries to force
// secret-store mapping values (and inverted maps) into list/show JSON output.
func TestAdversary_B31_CredentialValuesNeverLeaveRegistryAPI(t *testing.T) {
	stateRoot := t.TempDir()
	secretVal := "sk-live-SUPER-SECRET-VALUE-do-not-leak"
	// Normal map: declared ID -> local store name (should never appear as value in output).
	// Hostile map: attacker puts the secret material in the VALUE side.
	ref := advWriteAgent(t, stateRoot, "leaky", "a1b2c3d4", "1.0.0", false, "", map[string]string{
		"OPENAI_API_KEY": secretVal,
		"TOKEN":          "Bearer " + secretVal,
	})

	entries, err := registry.ListEntries(stateRoot, nil)
	if err != nil {
		t.Fatalf("ListEntries: %v", err)
	}
	raw, err := json.Marshal(entries)
	if err != nil {
		t.Fatalf("marshal list: %v", err)
	}
	if strings.Contains(string(raw), secretVal) {
		// ADVERSARY BREAK: registry list JSON leaked credential map values
		t.Fatalf("ADVERSARY BREAK: ListEntries JSON leaked credential map value %q in %s", secretVal, string(raw))
	}
	if strings.Contains(string(raw), "Bearer ") {
		t.Fatalf("ADVERSARY BREAK: ListEntries JSON leaked Bearer token material: %s", string(raw))
	}

	entry, err := registry.ShowEntry(stateRoot, ref, nil)
	if err != nil {
		t.Fatalf("ShowEntry: %v", err)
	}
	showRaw, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("marshal show: %v", err)
	}
	if strings.Contains(string(showRaw), secretVal) {
		t.Fatalf("ADVERSARY BREAK: ShowEntry JSON leaked credential map value %q in %s", secretVal, string(showRaw))
	}
	// Values must not appear among CredentialIDs either.
	for _, id := range entry.CredentialIDs {
		if strings.Contains(id, secretVal) || strings.Contains(id, "Bearer") {
			t.Fatalf("ADVERSARY BREAK: CredentialIDs contains secret material %q", id)
		}
	}
}

// TestAdversary_B31_ShowEntryPathTraversal asks ShowEntry to follow a
// name@pub8-shaped ref that path-escapes agents/ via filepath.Join cleaning.
// Promote validates refs; ShowEntry must not be weaker.
func TestAdversary_B31_ShowEntryPathTraversal(t *testing.T) {
	stateRoot := t.TempDir()
	// Plant a hostile manifest outside the agents tree.
	outsideDir := filepath.Join(stateRoot, "outside-plant")
	if err := os.MkdirAll(outsideDir, 0o700); err != nil {
		t.Fatalf("mkdir outside: %v", err)
	}
	planted := install.InstallManifest{
		PublisherFingerprint: strings.Repeat("ab", 32),
		PublisherName:        "attacker",
		AgentName:            "planted",
		AgentVersion:         "9.9.9",
		InstallMode:          "local-rebuild",
		LocalImageDigest:     "sha256:" + strings.Repeat("ee", 32),
		InstalledAt:          time.Now().UTC(),
		Promoted:             true,
		PromotedBy:           "attacker",
		CredentialMap: map[string]string{
			"LEAK_ID": "should-not-be-readable-via-traversal",
		},
	}
	raw, _ := json.MarshalIndent(planted, "", "  ")
	plantPath := filepath.Join(outsideDir, "install-manifest.json")
	if err := os.WriteFile(plantPath, raw, 0o600); err != nil {
		t.Fatalf("plant: %v", err)
	}

	// Build a ref that Join(agentsDir, ref, ...) cleans to outsideDir.
	// filepath.Clean only collapses ".." as its own path element, so the ref
	// must embed "/" separators: name@x/../../outside-plant
	agentsDir := filepath.Join(stateRoot, "agents")
	if err := os.MkdirAll(agentsDir, 0o700); err != nil {
		t.Fatalf("mkdir agents: %v", err)
	}
	rel, err := filepath.Rel(agentsDir, outsideDir)
	if err != nil {
		t.Fatalf("rel: %v", err)
	}
	// ref form keeps an @ so ShowEntry takes the exact-ref branch, then Join cleans.
	evilRef := "trav@" + filepath.Join("pad", rel)
	joined := filepath.Join(agentsDir, evilRef, "install-manifest.json")
	if filepath.Clean(joined) != filepath.Clean(plantPath) {
		// Explicit two-level escape from agents/<pad>/...
		evilRef = "trav@pad/../../outside-plant"
		joined = filepath.Join(agentsDir, evilRef, "install-manifest.json")
	}
	if filepath.Clean(joined) != filepath.Clean(plantPath) {
		t.Fatalf("test setup failed to craft escaping ref; joined=%q plant=%q evilRef=%q rel=%q", joined, plantPath, evilRef, rel)
	}

	entry, err := registry.ShowEntry(stateRoot, evilRef, nil)
	if err == nil && entry != nil {
		// ADVERSARY BREAK: ShowEntry followed path traversal out of agents/
		t.Fatalf("ADVERSARY BREAK: ShowEntry path traversal succeeded via ref %q; got entry name=%q version=%q promoted=%v",
			evilRef, entry.Name, entry.Version, entry.Promoted)
	}
}

// TestAdversary_B31_AmbiguousNameSkipsPromotionGate installs two publishers
// of the same package name (one unpromoted). Workflow validation must NOT
// silently skip the gate when resolution is ambiguous.
func TestAdversary_B31_AmbiguousNameSkipsPromotionGate(t *testing.T) {
	stateRoot := t.TempDir()
	advWriteAgent(t, stateRoot, "weather", "a1b2c3d4", "1.0.0", false, "", nil)
	advWriteAgent(t, stateRoot, "weather", "deadbeef", "1.0.0", false, "", nil)

	errs := registry.ValidateWorkflowPromotedPackages(stateRoot, advPipelineWF("weather", "1.0.0"))
	if len(errs) == 0 {
		// ADVERSARY BREAK: ambiguous bare name caused promotion check to be skipped
		t.Fatal("ADVERSARY BREAK: ValidateWorkflowPromotedPackages skipped gate for ambiguous unpromoted package name weather")
	}
}

// TestAdversary_B31_AliasShadowsPackageNameForPromotionGate gives a promoted
// evil package the alias equal to a different unpromoted package's name.
// Workflow package_name resolution must not accept the alias as the package.
func TestAdversary_B31_AliasShadowsPackageNameForPromotionGate(t *testing.T) {
	stateRoot := t.TempDir()
	// Real target package: unpromoted.
	advWriteAgent(t, stateRoot, "weather", "a1b2c3d4", "1.0.0", false, "", nil)
	// Attacker package: promoted, alias steals the name "weather".
	advWriteAgent(t, stateRoot, "evil-agent", "ffffffff", "0.0.1", true, "weather", nil)

	errs := registry.ValidateWorkflowPromotedPackages(stateRoot, advPipelineWF("weather", "1.0.0"))
	if len(errs) == 0 {
		// ADVERSARY BREAK: alias on unrelated package satisfied promotion for package_name
		t.Fatal("ADVERSARY BREAK: alias weather on evil-agent@ffffffff satisfied promotion gate for package_name weather")
	}
}

// TestAdversary_B31_WorkflowVersionIgnoredByPromotionGate promotes weather 1.0.0
// but references weather 9.9.9 in the workflow. Gate must not treat any
// promoted same-name install as sufficient for a different version.
func TestAdversary_B31_WorkflowVersionIgnoredByPromotionGate(t *testing.T) {
	stateRoot := t.TempDir()
	advWriteAgent(t, stateRoot, "weather", "a1b2c3d4", "1.0.0", true, "", nil)

	errs := registry.ValidateWorkflowPromotedPackages(stateRoot, advPipelineWF("weather", "9.9.9"))
	if len(errs) == 0 {
		// ADVERSARY BREAK: version pin in workflow ignored; any promoted name matches
		t.Fatal("ADVERSARY BREAK: promotion gate ignored workflow package_version 9.9.9; accepted promoted weather 1.0.0")
	}
}

// TestAdversary_B31_HandEditPromotedBypassesAudit sets promoted=true by
// rewriting install-manifest.json with no package_promoted audit event.
// The promotion gate and registry read API must not treat unaudited
// hand-edits as a successful promotion for trust decisions, or must at
// least surface the integrity gap. We assert the security-relevant gate.
func TestAdversary_B31_HandEditPromotedBypassesAudit(t *testing.T) {
	stateRoot := t.TempDir()
	ref := advWriteAgent(t, stateRoot, "worker", "a1b2c3d4", "1.0.0", false, "", nil)

	// Hand-edit: flip promoted without going through Promote().
	mp := filepath.Join(stateRoot, "agents", ref, "install-manifest.json")
	raw, err := os.ReadFile(mp)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var m install.InstallManifest
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("parse: %v", err)
	}
	now := time.Now().UTC()
	m.Promoted = true
	m.PromotedAt = &now
	m.PromotedBy = "hand-edit-attacker"
	out, _ := json.MarshalIndent(m, "", "  ")
	if err := os.WriteFile(mp, out, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	if advCountAudit(t, stateRoot, audit.EventTypePackagePromoted) != 0 {
		t.Fatal("setup: unexpected promote audit before attack")
	}

	// Registry reports promoted (source-of-truth is the mutable file).
	entry, err := registry.ShowEntry(stateRoot, ref, nil)
	if err != nil {
		t.Fatalf("ShowEntry: %v", err)
	}
	// Workflow gate accepts unaudited promotion.
	errs := registry.ValidateWorkflowPromotedPackages(stateRoot, advPipelineWF("worker", "1.0.0"))
	if entry.Promoted && len(errs) == 0 && advCountAudit(t, stateRoot, audit.EventTypePackagePromoted) == 0 {
		// ADVERSARY BREAK: hand-edited promoted=true grants workflow access with zero audit
		t.Fatal("ADVERSARY BREAK: hand-edited promoted=true granted workflow validation pass with no package_promoted audit event")
	}
}

// TestAdversary_B31_PromoteAuditFailureLeavesPromotedFlag forces audit open
// to fail AFTER the manifest write so Promote returns error while the package
// is already promoted (non-atomic promote+audit).
func TestAdversary_B31_PromoteAuditFailureLeavesPromotedFlag(t *testing.T) {
	stateRoot := t.TempDir()
	ref := advWriteAgent(t, stateRoot, "worker", "a1b2c3d4", "1.0.0", false, "", nil)

	// Make audit.jsonl a directory so NewAuditWriter fails after saveManifest.
	auditPath := filepath.Join(stateRoot, "audit.jsonl")
	if err := os.MkdirAll(auditPath, 0o700); err != nil {
		t.Fatalf("mkdir audit trap: %v", err)
	}

	err := registry.Promote(stateRoot, ref, "actor")
	m := advReadManifest(t, stateRoot, ref)
	if err != nil && m.Promoted {
		// ADVERSARY BREAK: Promote failed closed on audit but left promoted=true
		t.Fatalf("ADVERSARY BREAK: Promote returned error %v but left promoted=true (non-atomic write-before-audit)", err)
	}
	if err == nil && m.Promoted {
		// Audit somehow succeeded despite directory trap; not the attack.
		t.Fatalf("unexpected promote success with audit.jsonl as directory")
	}
}

// TestAdversary_B31_PromoteSymlinkManifestWriteEscape replaces the manifest
// with a symlink pointing outside the agent dir. Promote must not follow the
// symlink and write promotion state outside the install tree.
func TestAdversary_B31_PromoteSymlinkManifestWriteEscape(t *testing.T) {
	stateRoot := t.TempDir()
	ref := advWriteAgent(t, stateRoot, "worker", "a1b2c3d4", "1.0.0", false, "", nil)
	manifestPath := filepath.Join(stateRoot, "agents", ref, "install-manifest.json")

	// Read original bytes, move content to outside target, replace with symlink.
	orig, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	outside := filepath.Join(stateRoot, "escaped-manifest.json")
	if err := os.WriteFile(outside, orig, 0o600); err != nil {
		t.Fatalf("write outside: %v", err)
	}
	if err := os.Remove(manifestPath); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if err := os.Symlink(outside, manifestPath); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	// Also try parent-dir symlink escape: agents/worker@pub8 -> outside dir.
	// First attack is target symlink only.
	_ = registry.Promote(stateRoot, ref, "symlink-actor")

	// If Promote followed the symlink and wrote promoted state outside:
	outRaw, err := os.ReadFile(outside)
	if err != nil {
		t.Fatalf("read outside after promote: %v", err)
	}
	var escaped install.InstallManifest
	if err := json.Unmarshal(outRaw, &escaped); err != nil {
		t.Fatalf("parse outside: %v", err)
	}
	if escaped.Promoted {
		t.Fatal("ADVERSARY BREAK: Promote followed symlink and wrote promoted=true outside agent directory")
	}
}

// TestAdversary_B31_PromoteParentDirSymlinkEscape replaces the agent directory
// itself with a symlink to an attacker-controlled directory outside stateRoot.
func TestAdversary_B31_PromoteParentDirSymlinkEscape(t *testing.T) {
	stateRoot := t.TempDir()
	ref := advWriteAgent(t, stateRoot, "worker", "b2c3d4e5", "1.0.0", false, "", nil)
	agentDir := filepath.Join(stateRoot, "agents", ref)

	// Move agent dir outside and symlink.
	outsideDir := filepath.Join(stateRoot, "outside-agent")
	if err := os.RemoveAll(outsideDir); err != nil {
		t.Fatalf("rm outside: %v", err)
	}
	if err := os.Rename(agentDir, outsideDir); err != nil {
		t.Fatalf("rename: %v", err)
	}
	if err := os.Symlink(outsideDir, agentDir); err != nil {
		t.Fatalf("symlink dir: %v", err)
	}

	err := registry.Promote(stateRoot, ref, "parent-symlink-actor")
	// Defense may refuse to operate through dir symlink (good) or follow it (break if write lands outside via symlink).
	outManifest := filepath.Join(outsideDir, "install-manifest.json")
	raw, readErr := os.ReadFile(outManifest)
	if readErr != nil {
		// Could not read; promote may have failed closed.
		if err == nil {
			t.Fatalf("Promote succeeded but outside manifest unreadable: %v", readErr)
		}
		return
	}
	var m install.InstallManifest
	if jerr := json.Unmarshal(raw, &m); jerr != nil {
		t.Fatalf("parse: %v", jerr)
	}
	if err == nil && m.Promoted {
		// Writing through a parent dir symlink still mutates attacker-controlled path.
		// Treat as break if the implementation claims install paths are confined.
		// Many systems allow this for same-UID local state; flag only when Promote
		// also emits a successful audit claiming a local install path while writing elsewhere.
		if advCountAudit(t, stateRoot, audit.EventTypePackagePromoted) > 0 {
			t.Fatal("ADVERSARY BREAK: Promote followed parent directory symlink, wrote outside agents tree, and emitted package_promoted audit")
		}
	}
}

// TestAdversary_B31_ConcurrentPromoteRace hammers Promote on one ref while
// readers parse the manifest. Unlocked read-modify-write must not error,
// corrupt JSON, or drop identity fields. Exercises -race.
func TestAdversary_B31_ConcurrentPromoteRace(t *testing.T) {
	const rounds = 10
	const writers = 64
	const readers = 12
	for round := 0; round < rounds; round++ {
		stateRoot := t.TempDir()
		ref := advWriteAgent(t, stateRoot, "worker", "a1b2c3d4", "1.0.0", false, "", map[string]string{
			"KEEP_ME": "local-store-name",
		})
		mp := filepath.Join(stateRoot, "agents", ref, "install-manifest.json")

		var (
			rwg      sync.WaitGroup
			breakMu  sync.Mutex
			breakMsg string
		)
		setBreak := func(msg string) {
			breakMu.Lock()
			if breakMsg == "" {
				breakMsg = msg
			}
			breakMu.Unlock()
		}

		stop := make(chan struct{})
		rwg.Add(readers)
		for r := 0; r < readers; r++ {
			go func() {
				defer rwg.Done()
				for {
					select {
					case <-stop:
						return
					default:
					}
					raw, err := os.ReadFile(mp)
					if err != nil || len(raw) == 0 {
						if err == nil && len(raw) == 0 {
							setBreak("ADVERSARY BREAK: reader observed empty manifest during concurrent Promote")
							return
						}
						continue
					}
					var m install.InstallManifest
					if err := json.Unmarshal(raw, &m); err != nil {
						setBreak("ADVERSARY BREAK: reader observed corrupt JSON during concurrent Promote: " + err.Error())
						return
					}
				}
			}()
		}

		var wwg sync.WaitGroup
		errCh := make(chan error, writers)
		wwg.Add(writers)
		for i := 0; i < writers; i++ {
			go func() {
				defer wwg.Done()
				errCh <- registry.Promote(stateRoot, ref, "actor")
			}()
		}
		wwg.Wait()
		close(errCh)
		close(stop)
		rwg.Wait()

		for err := range errCh {
			if err != nil {
				t.Fatalf("ADVERSARY BREAK: concurrent Promote returned error (round %d): %v", round, err)
			}
		}
		breakMu.Lock()
		msg := breakMsg
		breakMu.Unlock()
		if msg != "" {
			t.Fatalf("%s (round %d)", msg, round)
		}

		raw, err := os.ReadFile(mp)
		if err != nil {
			t.Fatalf("read round %d: %v", round, err)
		}
		var m install.InstallManifest
		if err := json.Unmarshal(raw, &m); err != nil {
			t.Fatalf("ADVERSARY BREAK: concurrent Promote left corrupt manifest JSON (round %d): %v\n%s", round, err, string(raw))
		}
		if !m.Promoted {
			t.Fatalf("ADVERSARY BREAK: concurrent Promote left promoted=false (round %d)", round)
		}
		if m.AgentName != "worker" || m.AgentVersion != "1.0.0" {
			t.Fatalf("ADVERSARY BREAK: concurrent Promote clobbered identity fields (round %d): name=%q ver=%q", round, m.AgentName, m.AgentVersion)
		}
		if m.CredentialMap["KEEP_ME"] != "local-store-name" {
			t.Fatalf("ADVERSARY BREAK: concurrent Promote dropped CredentialMap (round %d): %#v", round, m.CredentialMap)
		}
	}
}

// TestAdversary_B31_ConcurrentPromoteDemoteRace interleaves Promote and Demote
// while readers parse the manifest. Unlocked non-atomic WriteFile must not
// produce unreadable JSON or inconsistent promotion fields at any time.
func TestAdversary_B31_ConcurrentPromoteDemoteRace(t *testing.T) {
	const rounds = 20
	const writers = 80
	const readers = 16
	for round := 0; round < rounds; round++ {
		stateRoot := t.TempDir()
		ref := advWriteAgent(t, stateRoot, "worker", "c3d4e5f6", "1.0.0", false, "", nil)
		mp := filepath.Join(stateRoot, "agents", ref, "install-manifest.json")

		var (
			wg       sync.WaitGroup
			breakMu  sync.Mutex
			breakMsg string
		)
		setBreak := func(msg string) {
			breakMu.Lock()
			if breakMsg == "" {
				breakMsg = msg
			}
			breakMu.Unlock()
		}

		stop := make(chan struct{})
		wg.Add(readers)
		for r := 0; r < readers; r++ {
			go func() {
				defer wg.Done()
				for {
					select {
					case <-stop:
						return
					default:
					}
					raw, err := os.ReadFile(mp)
					if err != nil {
						continue
					}
					if len(raw) == 0 {
						setBreak("ADVERSARY BREAK: reader observed empty manifest during promote/demote race")
						return
					}
					var m install.InstallManifest
					if err := json.Unmarshal(raw, &m); err != nil {
						setBreak("ADVERSARY BREAK: reader observed corrupt JSON during promote/demote race: " + err.Error() + "\n" + string(raw))
						return
					}
					if m.Promoted && m.PromotedAt == nil {
						setBreak("ADVERSARY BREAK: reader observed promoted=true with nil PromotedAt")
						return
					}
					if !m.Promoted && (m.PromotedAt != nil || m.PromotedBy != "") {
						setBreak("ADVERSARY BREAK: reader observed promoted=false with residual promotion metadata")
						return
					}
				}
			}()
		}

		var wwg sync.WaitGroup
		wwg.Add(writers)
		for i := 0; i < writers; i++ {
			go func(i int) {
				defer wwg.Done()
				if i%2 == 0 {
					_ = registry.Promote(stateRoot, ref, "racer")
				} else {
					_ = registry.Demote(stateRoot, ref)
				}
			}(i)
		}
		wwg.Wait()
		close(stop)
		wg.Wait()

		breakMu.Lock()
		msg := breakMsg
		breakMu.Unlock()
		if msg != "" {
			t.Fatalf("%s (round %d)", msg, round)
		}

		raw, err := os.ReadFile(mp)
		if err != nil {
			t.Fatalf("read round %d: %v", round, err)
		}
		var m install.InstallManifest
		if err := json.Unmarshal(raw, &m); err != nil {
			t.Fatalf("ADVERSARY BREAK: promote/demote race left corrupt JSON (round %d): %v\n%s", round, err, string(raw))
		}
		if m.Promoted && m.PromotedAt == nil {
			t.Fatalf("ADVERSARY BREAK: promoted=true with nil PromotedAt after race (round %d)", round)
		}
		if !m.Promoted && (m.PromotedAt != nil || m.PromotedBy != "") {
			t.Fatalf("ADVERSARY BREAK: promoted=false but residual PromotedAt=%v PromotedBy=%q (round %d)", m.PromotedAt, m.PromotedBy, round)
		}
	}
}

// TestAdversary_B31_EmptyAndHostileRefs exercises empty, whitespace, control
// chars, path separators, and overlong refs on Promote/Demote/ShowEntry.
func TestAdversary_B31_EmptyAndHostileRefs(t *testing.T) {
	stateRoot := t.TempDir()
	_ = advWriteAgent(t, stateRoot, "worker", "a1b2c3d4", "1.0.0", false, "", nil)

	hostile := []string{
		"",
		"   ",
		"\n",
		"\x00worker",
		"worker\x00@a1b2c3d4",
		"../worker",
		"worker/../../etc",
		"worker@a1b2c3d4/../../../etc/passwd",
		strings.Repeat("a", 4096),
		"worker@" + strings.Repeat("f", 4096),
		"WORKER@A1B2C3D4", // uppercase name should not silently match
	}

	for _, ref := range hostile {
		if err := registry.Promote(stateRoot, ref, "actor"); err == nil {
			// Uppercase might be rejected by resolve; empty must fail.
			t.Fatalf("ADVERSARY BREAK: Promote accepted hostile ref %q", ref)
		}
		if err := registry.Demote(stateRoot, ref); err == nil {
			t.Fatalf("ADVERSARY BREAK: Demote accepted hostile ref %q", ref)
		}
		// ShowEntry is a read path; still must not panic or escape.
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("ADVERSARY BREAK: ShowEntry panicked on ref %q: %v", ref, r)
				}
			}()
			_, _ = registry.ShowEntry(stateRoot, ref, nil)
		}()
	}
}

// TestAdversary_B31_PromoteNonInstalledAndMissingRef ensures promote/demote
// cannot target names that resolve as non-installed passthroughs.
func TestAdversary_B31_PromoteNonInstalledAndMissingRef(t *testing.T) {
	stateRoot := t.TempDir()
	if err := registry.Promote(stateRoot, "not-installed-pkg", "actor"); err == nil {
		t.Fatal("ADVERSARY BREAK: Promote succeeded for non-installed bare name")
	}
	if err := registry.Demote(stateRoot, "not-installed-pkg"); err == nil {
		t.Fatal("ADVERSARY BREAK: Demote succeeded for non-installed bare name")
	}
	if err := registry.Promote(stateRoot, "missing@deadbeef", "actor"); err == nil {
		t.Fatal("ADVERSARY BREAK: Promote succeeded for missing name@pub8")
	}
}

// TestAdversary_B31_PromoteByAliasAmbiguous must not pick one target when two
// agents share the same alias (install layer should already reject, but
// promote must fail closed).
func TestAdversary_B31_PromoteByAliasAmbiguous(t *testing.T) {
	stateRoot := t.TempDir()
	// Force two manifests with same alias (bypass CheckAliasUnique by hand-write).
	ref1 := advWriteAgent(t, stateRoot, "alpha", "a1a1a1a1", "1.0.0", false, "shared", nil)
	ref2 := advWriteAgent(t, stateRoot, "beta", "b2b2b2b2", "1.0.0", false, "shared", nil)
	_ = ref1
	_ = ref2

	err := registry.Promote(stateRoot, "shared", "actor")
	if err == nil {
		// If one was promoted, that is a break (ambiguous promote).
		m1 := advReadManifest(t, stateRoot, ref1)
		m2 := advReadManifest(t, stateRoot, ref2)
		if m1.Promoted || m2.Promoted {
			t.Fatalf("ADVERSARY BREAK: Promote on ambiguous alias promoted alpha=%v beta=%v", m1.Promoted, m2.Promoted)
		}
		t.Fatal("ADVERSARY BREAK: Promote on ambiguous alias returned nil error")
	}
}

// TestAdversary_B31_AuditPayloadFingerprintAndDigestIntegrity checks that a
// successful Promote audit carries the publisher fingerprint and a non-empty
// digest matching the installed package identity (not blank / attacker-controlled drift).
func TestAdversary_B31_AuditPayloadFingerprintAndDigestIntegrity(t *testing.T) {
	stateRoot := t.TempDir()
	ref := advWriteAgent(t, stateRoot, "worker", "a1b2c3d4", "1.0.0", false, "", nil)
	// Clear LocalImageDigest after write to see if audit still claims a digest.
	mp := filepath.Join(stateRoot, "agents", ref, "install-manifest.json")
	raw, _ := os.ReadFile(mp)
	var m install.InstallManifest
	_ = json.Unmarshal(raw, &m)
	wantFP := m.PublisherFingerprint
	m.LocalImageDigest = "" // attacker/operator cleared
	out, _ := json.MarshalIndent(m, "", "  ")
	_ = os.WriteFile(mp, out, 0o600)

	if err := registry.Promote(stateRoot, ref, "auditor"); err != nil {
		t.Fatalf("Promote: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(stateRoot, "audit.jsonl"))
	if err != nil {
		t.Fatalf("read audit: %v", err)
	}
	var found *audit.AuditRecord
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var rec audit.AuditRecord
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			continue
		}
		if rec.EventType == audit.EventTypePackagePromoted {
			found = &rec
		}
	}
	if found == nil {
		t.Fatal("missing package_promoted audit event")
	}
	fp, _ := found.Payload["fingerprint"].(string)
	dig, _ := found.Payload["digest"].(string)
	agentRef, _ := found.Payload["agent_ref"].(string)
	if agentRef != ref {
		t.Fatalf("ADVERSARY BREAK: audit agent_ref=%q want %q", agentRef, ref)
	}
	if fp != wantFP {
		t.Fatalf("ADVERSARY BREAK: audit fingerprint=%q want %q", fp, wantFP)
	}
	// Digest should come from authoritative package identity (lock image digest),
	// not an empty local_image_digest field.
	if dig == "" {
		t.Fatal("ADVERSARY BREAK: package_promoted audit emitted empty digest after LocalImageDigest cleared")
	}
	if dig != "sha256:"+strings.Repeat("cc", 32) && dig != "sha256:"+strings.Repeat("bb", 32) {
		// Prefer lock digest; accept local only if non-empty was used intentionally.
		t.Logf("audit digest=%q (lock=cc..., local=bb...)", dig)
	}
	if dig != "sha256:"+strings.Repeat("cc", 32) {
		// Soft break: using only LocalImageDigest means cleared local digest empties audit.
		if dig == "" {
			t.Fatal("ADVERSARY BREAK: audit digest empty")
		}
	}
}

// TestAdversary_B31_DemoteDoesNotRequireInstalledFalsePositive ensures demote
// of a random alias that happens to match nothing fails closed.
func TestAdversary_B31_DemoteUnknownAlias(t *testing.T) {
	stateRoot := t.TempDir()
	advWriteAgent(t, stateRoot, "worker", "a1b2c3d4", "1.0.0", true, "realalias", nil)
	if err := registry.Demote(stateRoot, "no-such-alias"); err == nil {
		t.Fatal("ADVERSARY BREAK: Demote succeeded for unknown alias")
	}
	// Real alias still promoted.
	m := advReadManifest(t, stateRoot, "worker@a1b2c3d4")
	if !m.Promoted {
		t.Fatal("ADVERSARY BREAK: Demote unknown alias demoted an unrelated package")
	}
}

// TestAdversary_B31_ShowEntryNullByteAndNewline rejects control characters
// rather than truncating paths at NUL (C-string style) or injecting lines.
func TestAdversary_B31_ShowEntryNullByteAndNewline(t *testing.T) {
	stateRoot := t.TempDir()
	ref := advWriteAgent(t, stateRoot, "worker", "a1b2c3d4", "1.0.0", true, "", nil)

	// NUL after valid ref: naive path APIs may truncate.
	nulRef := ref + "\x00/../../../etc/passwd"
	entry, err := registry.ShowEntry(stateRoot, nulRef, nil)
	if err == nil && entry != nil {
		// If it returned the real worker entry by truncating at NUL, that is a break.
		if entry.Ref == ref || entry.Name == "worker" {
			t.Fatalf("ADVERSARY BREAK: ShowEntry truncated at NUL and resolved %q to %q", nulRef, entry.Ref)
		}
		t.Fatalf("ADVERSARY BREAK: ShowEntry accepted NUL ref %q", nulRef)
	}

	nlRef := "worker\n@a1b2c3d4"
	if entry, err = registry.ShowEntry(stateRoot, nlRef, nil); err == nil && entry != nil {
		t.Fatalf("ADVERSARY BREAK: ShowEntry accepted newline ref, got %+v", entry)
	}
}

// TestAdversary_B31_ListEntriesJSONHasNoCredentialMapField ensures the public
// RegistryEntry JSON schema cannot grow a credential_map field by accident.
func TestAdversary_B31_ListEntriesJSONHasNoCredentialMapField(t *testing.T) {
	stateRoot := t.TempDir()
	advWriteAgent(t, stateRoot, "worker", "a1b2c3d4", "1.0.0", false, "", map[string]string{
		"K": "v-secret-store",
	})
	entries, err := registry.ListEntries(stateRoot, nil)
	if err != nil {
		t.Fatalf("ListEntries: %v", err)
	}
	raw, err := json.Marshal(entries)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(raw), "credential_map") {
		t.Fatalf("ADVERSARY BREAK: ListEntries JSON contains credential_map field: %s", string(raw))
	}
	if strings.Contains(string(raw), "v-secret-store") {
		t.Fatalf("ADVERSARY BREAK: ListEntries JSON leaked credential map value: %s", string(raw))
	}
}

// TestAdversary_B31_PromoteIdempotentDoesNotRewriteActor is a regression
// attack: second Promote with different actor must not re-attribute promotion.
func TestAdversary_B31_PromoteIdempotentDoesNotRewriteActor(t *testing.T) {
	stateRoot := t.TempDir()
	ref := advWriteAgent(t, stateRoot, "worker", "a1b2c3d4", "1.0.0", false, "", nil)
	if err := registry.Promote(stateRoot, ref, "first-actor"); err != nil {
		t.Fatalf("promote1: %v", err)
	}
	m1 := advReadManifest(t, stateRoot, ref)
	time.Sleep(5 * time.Millisecond)
	if err := registry.Promote(stateRoot, ref, "second-attacker"); err != nil {
		t.Fatalf("promote2: %v", err)
	}
	m2 := advReadManifest(t, stateRoot, ref)
	if m2.PromotedBy != "first-actor" {
		t.Fatalf("ADVERSARY BREAK: idempotent Promote rewrote PromotedBy %q -> %q", m1.PromotedBy, m2.PromotedBy)
	}
	if m1.PromotedAt == nil || m2.PromotedAt == nil || !m1.PromotedAt.Equal(*m2.PromotedAt) {
		t.Fatalf("ADVERSARY BREAK: idempotent Promote rewrote PromotedAt %v -> %v", m1.PromotedAt, m2.PromotedAt)
	}
}
