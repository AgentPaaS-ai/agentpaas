package cli

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/AgentPaaS-ai/agentpaas/internal/bundle"
	"github.com/AgentPaaS-ai/agentpaas/internal/home"
	"github.com/AgentPaaS-ai/agentpaas/internal/identity"
	"github.com/AgentPaaS-ai/agentpaas/internal/install"
	"github.com/AgentPaaS-ai/agentpaas/internal/secrets"
	"github.com/spf13/cobra"
)

func sharingDocPath(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return filepath.Join(dir, "docs", "sharing.md")
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("repo root not found")
		}
		dir = parent
	}
}

var sharingForbiddenPhrases = []string{
	"this agent is safe",
	"signature guarantees safety",
	"trusted publisher means safe",
	"verified means safe",
	"signature means safe",
}

func TestSharingDoc_ForbiddenSafetyPhrases(t *testing.T) {
	data, err := os.ReadFile(sharingDocPath(t))
	if err != nil {
		t.Fatalf("read sharing.md: %v", err)
	}
	lower := strings.ToLower(string(data))
	for _, phrase := range sharingForbiddenPhrases {
		if strings.Contains(lower, phrase) {
			t.Errorf("forbidden safety-claim phrase %q found in docs/sharing.md", phrase)
		}
	}
	if !strings.Contains(string(data), "A valid signature proves who signed this and that it is unmodified.") ||
		!strings.Contains(string(data), "It does not mean the agent is safe. Review the policy below.") {
		t.Fatal("docs/sharing.md must include the exact D3 disclaimer sentences")
	}
}

func TestSharingDoc_LocalLinksResolve(t *testing.T) {
	mdPath := sharingDocPath(t)
	data, err := os.ReadFile(mdPath)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	linkRe := regexp.MustCompile(`\[([^\]]*)\]\(([^)]+)\)`)
	mdDir := filepath.Dir(mdPath)
	for _, m := range linkRe.FindAllStringSubmatch(string(data), -1) {
		target := m[2]
		if strings.HasPrefix(target, "http://") || strings.HasPrefix(target, "https://") || strings.HasPrefix(target, "#") {
			continue
		}
		if idx := strings.Index(target, "#"); idx >= 0 {
			target = target[:idx]
		}
		if target == "" {
			continue
		}
		full := filepath.Join(mdDir, target)
		if _, err := os.Stat(full); err != nil {
			t.Errorf("broken link [%s](%s) → %s", m[1], m[2], full)
		}
	}
}

func extractSharingDocAgentpaasLines(md string) []string {
	var out []string
	inBash := false
	for _, raw := range strings.Split(md, "\n") {
		line := strings.TrimSpace(raw)
		if strings.HasPrefix(line, "```") {
			if inBash && line == "```" {
				inBash = false
				continue
			}
			if strings.HasPrefix(line, "```bash") {
				inBash = true
			}
			continue
		}
		if inBash && strings.HasPrefix(line, "agentpaas ") {
			out = append(out, line)
		}
	}
	return out
}

var sharingDaemonOnly = map[string]bool{
	"pack": true, "export": true, "run": true, "daemon": true,
	"import": true, // identity import
	"fork":   true, // requires installed agent ref; smoke test materializes install separately
}

func TestSharingDoc_CommandsSmoke(t *testing.T) {
	t.Setenv("AGENTPAAS_SOCKET", filepath.Join(t.TempDir(), "offline.sock"))

	bundlePath := writeCLITestBundle(t)
	b, err := bundle.Open(bundlePath)
	if err != nil {
		t.Fatalf("open bundle: %v", err)
	}
	defer func() { _ = b.Close() }()
	manifest := b.Manifest
	if manifest == nil {
		t.Fatal("nil manifest")
	}
	pubFP := manifest.Publisher.Fingerprint
	pubName := manifest.Publisher.Name
	keyFile := filepath.Join(t.TempDir(), "publisher.pem")
	if err := os.WriteFile(keyFile, []byte(manifest.Publisher.PublicKeyPEM), 0o600); err != nil {
		t.Fatalf("write pem: %v", err)
	}

	homeDir := t.TempDir()
	stateRoot := filepath.Join(homeDir, "state")
	paths := home.NewHomePaths(homeDir)
	if err := os.MkdirAll(stateRoot, 0o700); err != nil {
		t.Fatal(err)
	}

	// Identity (offline).
	fakeKS := identity.NewFakeKeyStore()
	oldID := identityStoreFactory
	identityStoreFactory = func(*cobra.Command) (identity.KeyStore, error) { return fakeKS, nil }
	t.Cleanup(func() { identityStoreFactory = oldID })

	if _, _, err := executeCmd("identity", "init", "--name", "my-publisher", "--home", homeDir); err != nil {
		t.Fatalf("identity init: %v", err)
	}
	if _, _, err := executeCmd("identity", "show", "--home", homeDir); err != nil {
		t.Fatalf("identity show: %v", err)
	}

	// Bundle inspect + provenance (offline).
	if _, _, err := executeCmd("bundle", "inspect", bundlePath, "--home", homeDir); err != nil {
		t.Fatalf("bundle inspect: %v", err)
	}
	if _, _, err := executeCmd("provenance", "show", bundlePath, "--home", homeDir); err != nil {
		t.Fatalf("provenance show bundle: %v", err)
	}

	tampered := filepath.Join(t.TempDir(), "tampered.agentpaas")
	if err := bundle.TamperManifestCreatedAt(bundlePath, tampered); err != nil {
		t.Fatalf("tamper: %v", err)
	}
	if _, _, err := executeCmd("bundle", "inspect", tampered, "--home", homeDir); err == nil {
		t.Fatal("expected error for tampered bundle inspect")
	}

	// Trust.
	if _, _, err := executeCmd("trust", "list", "--home", homeDir); err != nil {
		t.Fatalf("trust list: %v", err)
	}
	if _, _, err := executeCmd("trust", "add", pubFP, "--key", keyFile, "--alias", pubName, "--home", homeDir); err != nil {
		t.Fatalf("trust add: %v", err)
	}
	if _, _, err := executeCmd("trust", "remove", pubName, "--yes", "--home", homeDir); err != nil {
		t.Fatalf("trust remove: %v", err)
	}

	// Materialize install for installed/* and provenance on ref.
	lock := b.Lock
	if lock == nil {
		t.Fatal("nil lock")
	}
	polDigest := lock.PolicyDigest
	res, err := install.MaterializeInstall(context.Background(), install.MaterializeOpts{
		StateRoot: stateRoot,
		Bundle:    b,
		Manifest: install.InstallManifest{
			PublisherFingerprint: pubFP,
			PublisherName:        pubName,
			AgentName:            lock.AgentName,
			AgentVersion:         lock.AgentVersion,
			AcceptedPolicyDigest: polDigest,
		},
		AllowUnlockedDeps: true,
		Builder:           provenanceTestImageBuilder{},
	})
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}
	ref := res.AgentRef

	brokeredPolicy := []byte(`version: "1.0"
agent:
  name: cli-bundle-test
egress:
  - domain: "api.example.com"
    ports: [443]
    credential: "api-token"
credentials:
  - id: "api-token"
    type: brokered
    header: Authorization
`)
	fileState := &install.FileInstallState{StateRoot: stateRoot}
	if err := fileState.SaveApprovedInstall(res.Manifest, brokeredPolicy); err != nil {
		t.Fatalf("SaveApprovedInstall: %v", err)
	}

	installedListFactory = func(*cobra.Command) ([]install.InstalledAgentEntry, error) {
		return install.ListInstalledAgents(paths.State)
	}
	t.Cleanup(func() { installedListFactory = defaultListInstalled })

	if _, _, err := executeCmd("installed", "list", "--home", homeDir); err != nil {
		t.Fatalf("installed list: %v", err)
	}
	if _, _, err := executeCmd("installed", "alias", ref, "my-alias", "--home", homeDir); err != nil {
		t.Fatalf("installed alias: %v", err)
	}

	store := secrets.NewFakeKeyStore()
	_ = store.Set(t.Context(), "OPENROUTER_KEY", []byte("x"))
	oldSecret := secretStoreFactory
	secretStoreFactory = func(*cobra.Command) (secrets.SecretStore, error) { return store, nil }
	t.Cleanup(func() { secretStoreFactory = oldSecret })
	installStateFactory = func(*cobra.Command) (install.InstallStateStore, error) {
		return &install.FileInstallState{StateRoot: paths.State}, nil
	}
	t.Cleanup(func() { installStateFactory = newDefaultInstallState })

	if _, _, err := executeCmd("secret", "list", "--home", homeDir); err != nil {
		t.Fatalf("secret list: %v", err)
	}
	if _, _, err := executeCmd("installed", "map-credential", ref, "api-token=OPENROUTER_KEY", "--home", homeDir); err != nil {
		t.Fatalf("map-credential: %v", err)
	}
	if _, _, err := executeCmd("provenance", "show", ref, "--home", homeDir); err != nil {
		t.Fatalf("provenance show ref: %v", err)
	}

	installedRemoveFactory = func(_ *cobra.Command, removedRef string) error {
		return install.RemoveInstalledAgent(context.Background(), paths.State, removedRef, &install.FakeContainerStopper{}, nil)
	}
	t.Cleanup(func() { installedRemoveFactory = defaultRemoveInstalled })
	if _, _, err := executeCmd("installed", "remove", ref, "--home", homeDir); err != nil {
		t.Fatalf("installed remove: %v", err)
	}

	// Parse every agentpaas line in bash fences; offline ones must match executed set or be daemon-only.
	data, err := os.ReadFile(sharingDocPath(t))
	if err != nil {
		t.Fatalf("read doc: %v", err)
	}
	lines := extractSharingDocAgentpaasLines(string(data))
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) < 2 || fields[0] != "agentpaas" {
			continue
		}
		sub := fields[1]
		if sharingDaemonOnly[sub] {
			t.Logf("documented daemon-only command skipped: %s", line)
			continue
		}
		if sub == "identity" && len(fields) > 2 && (fields[2] == "export" || fields[2] == "import") {
			t.Logf("documented interactive command skipped: %s", line)
			continue
		}
	}
}
