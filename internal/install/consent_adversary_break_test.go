package install

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AgentPaaS-ai/agentpaas/internal/audit"
	"github.com/AgentPaaS-ai/agentpaas/internal/bundle"
	"github.com/AgentPaaS-ai/agentpaas/internal/pack"
)

const adversarySecretSentinel = "ADVERSARY_B23T02_SECRET_DO_NOT_LEAK_X9K2"

func consentOptsBase(fix consentBundleFixture, state InstallStateStore) PolicyConsentOpts {
	return PolicyConsentOpts{
		Report:               fix.InspectReport,
		PolicyDigest:         fix.PolicyDigest,
		PolicyYAML:           fix.PolicyYAML,
		PublisherFingerprint: fix.PublisherFP,
		PublisherName:        fix.PublisherName,
		AgentName:            fix.AgentName,
		AgentVersion:         fix.AgentVersion,
		State:                state,
	}
}

func assertSentinelAbsent(t *testing.T, sentinel string, blobs ...string) {
	t.Helper()
	for i, b := range blobs {
		if strings.Contains(b, sentinel) {
			t.Fatalf("sentinel leaked in output blob %d", i)
		}
	}
}

func readFileString(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(raw)
}

func trustStoreSnapshot(t *testing.T, storePath string) []byte {
	t.Helper()
	raw, err := os.ReadFile(storePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatalf("read trust store: %v", err)
	}
	return append([]byte(nil), raw...)
}

func assertTrustStoreUnchanged(t *testing.T, storePath string, before []byte) {
	t.Helper()
	after := trustStoreSnapshot(t, storePath)
	if string(before) != string(after) {
		t.Fatalf("trust store mutated on policy decline\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

func walkStatePermissions(t *testing.T, root string) {
	t.Helper()
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if path == root {
			return nil
		}
		mode := info.Mode().Perm()
		if info.IsDir() {
			if mode != 0o700 {
				t.Fatalf("dir %s mode %o want 0700", path, mode)
			}
		} else {
			if mode != 0o600 {
				t.Fatalf("file %s mode %o want 0600", path, mode)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk state: %v", err)
	}
}

// Claim 1: digest binding — case-variant accept must not succeed.
func TestAdversary_B23T02_DigestCaseVariantRejected(t *testing.T) {
	fix := writeConsentFixtureBundle(t, nil, "0.1.0")
	state, root := newConsentState(t)
	before := stateFileCount(t, root)
	upper := strings.ToUpper(fix.PolicyDigest)
	if upper == fix.PolicyDigest {
		t.Skip("digest has no case variance")
	}
	_, err := ResolvePolicyConsent(PolicyConsentOpts{
		Report: fix.InspectReport, PolicyDigest: fix.PolicyDigest, PolicyYAML: fix.PolicyYAML,
		PublisherFingerprint: fix.PublisherFP, PublisherName: fix.PublisherName,
		AgentName: fix.AgentName, AgentVersion: fix.AgentVersion, State: state,
		IsTTY: false, AcceptPolicyDigest: upper,
	})
	if !errors.Is(err, ErrPolicyMismatch) {
		t.Fatalf("want ErrPolicyMismatch for uppercase digest, got %v", err)
	}
	assertNoStateGrowth(t, root, before)
}

// Claim 1: digest prefix must not match full digest.
func TestAdversary_B23T02_DigestPrefixRejected(t *testing.T) {
	fix := writeConsentFixtureBundle(t, nil, "0.1.0")
	state, root := newConsentState(t)
	before := stateFileCount(t, root)
	if len(fix.PolicyDigest) < 8 {
		t.Fatal("digest too short")
	}
	prefix := fix.PolicyDigest[:len(fix.PolicyDigest)/2]
	_, err := ResolvePolicyConsent(PolicyConsentOpts{
		Report: fix.InspectReport, PolicyDigest: fix.PolicyDigest, PolicyYAML: fix.PolicyYAML,
		PublisherFingerprint: fix.PublisherFP, PublisherName: fix.PublisherName,
		AgentName: fix.AgentName, AgentVersion: fix.AgentVersion, State: state,
		IsTTY: false, AcceptPolicyDigest: prefix,
	})
	if !errors.Is(err, ErrPolicyMismatch) {
		t.Fatalf("want ErrPolicyMismatch for digest prefix, got %v", err)
	}
	assertNoStateGrowth(t, root, before)
}

// Claim 2: TTY abort after max wrong prompts — zero state writes.
func TestAdversary_B23T02_TTYAbortZeroWrites(t *testing.T) {
	fix := writeConsentFixtureBundle(t, nil, "0.1.0")
	state, root := newConsentState(t)
	before := stateFileCount(t, root)
	_, err := ResolvePolicyConsent(PolicyConsentOpts{
		Report: fix.InspectReport, PolicyDigest: fix.PolicyDigest, PolicyYAML: fix.PolicyYAML,
		PublisherFingerprint: fix.PublisherFP, PublisherName: fix.PublisherName,
		AgentName: fix.AgentName, AgentVersion: fix.AgentVersion, State: state,
		IsTTY: true, Prompt: promptSequence("no", "nope", "deny"),
	})
	if !errors.Is(err, ErrPolicyRefused) {
		t.Fatalf("want ErrPolicyRefused, got %v", err)
	}
	assertNoStateGrowth(t, root, before)
}

// Claim 4: downgrade without flag — ErrDowngradeRefused and no new state files.
func TestAdversary_B23T02_DowngradeRefusedZeroWrites(t *testing.T) {
	fix := writeConsentFixtureBundle(t, nil, "0.1.0")
	state, root := newConsentState(t)
	if err := state.SaveApprovedInstall(InstallManifest{
		PublisherFingerprint: fix.PublisherFP, PublisherName: fix.PublisherName,
		AgentName: fix.AgentName, AgentVersion: "0.2.0", AcceptedPolicyDigest: fix.PolicyDigest,
	}, fix.PolicyYAML); err != nil {
		t.Fatalf("seed: %v", err)
	}
	before := stateFileCount(t, root)
	_, err := ResolvePolicyConsent(PolicyConsentOpts{
		Report: fix.InspectReport, PolicyDigest: fix.PolicyDigest, PolicyYAML: fix.PolicyYAML,
		PublisherFingerprint: fix.PublisherFP, PublisherName: fix.PublisherName,
		AgentName: fix.AgentName, AgentVersion: "0.1.0", State: state,
		IsTTY: false, AcceptPolicyDigest: fix.PolicyDigest,
	})
	if !errors.Is(err, ErrDowngradeRefused) {
		t.Fatalf("want ErrDowngradeRefused, got %v", err)
	}
	assertNoStateGrowth(t, root, before)
}

// Claim 3: consent decline must not touch trust store (separate from install state).
func TestAdversary_B23T02_TrustStoreUntouchedOnPolicyDecline(t *testing.T) {
	fix := writeConsentFixtureBundle(t, nil, "0.1.0")
	store, storePath := newTestStore(t)
	tk := generateTestKey(t)
	prePin(t, store, "consent-publisher", tk)

	state, root := newConsentState(t)
	before := stateFileCount(t, root)
	trustBefore := trustStoreSnapshot(t, storePath)

	_, err := ResolvePolicyConsent(PolicyConsentOpts{
		Report: fix.InspectReport, PolicyDigest: fix.PolicyDigest, PolicyYAML: fix.PolicyYAML,
		PublisherFingerprint: fix.PublisherFP, PublisherName: fix.PublisherName,
		AgentName: fix.AgentName, AgentVersion: fix.AgentVersion, State: state,
		IsTTY: false, AcceptPolicyDigest: "deadbeef",
	})
	if !errors.Is(err, ErrPolicyMismatch) {
		t.Fatalf("want mismatch, got %v", err)
	}
	assertNoStateGrowth(t, root, before)
	assertTrustStoreUnchanged(t, storePath, trustBefore)
	_ = store // consent path never receives trust store
}

// Claim 5: structural diff follows stored policy bytes, not signer provenance narrative.
func TestAdversary_B23T02_ProvenanceCannotOverrideStructuralDiff(t *testing.T) {
	oldPolicy := []byte(consentBasePolicyYAML)
	newPolicy := []byte(`version: "1.0"
agent:
  name: consent-test-agent
egress:
  - domain: "api.example.com"
    ports: [443]
  - domain: "yaml-added.example.com"
    ports: [443]
credentials:
  - id: "my-key"
    type: header
    header: "X-API-Key"
    value: "x"
`)
	fixOld := writeConsentFixtureBundle(t, oldPolicy, "0.1.0")
	fixNew := writeConsentFixtureBundle(t, newPolicy, "0.2.0")
	state, _ := newConsentState(t)
	if err := state.SaveApprovedInstall(InstallManifest{
		PublisherFingerprint: fixOld.PublisherFP, PublisherName: fixOld.PublisherName,
		AgentName: fixOld.AgentName, AgentVersion: "0.1.0",
		AcceptedPolicyDigest: fixOld.PolicyDigest,
	}, oldPolicy); err != nil {
		t.Fatalf("seed: %v", err)
	}

	tampered := *fixNew.InspectReport
	tampered.ProvenanceText = "SIGNER CLAIMED DELTA:\n+ egress: provenance-only.evil.com\n- egress: api.example.com"

	res, err := ResolvePolicyConsent(PolicyConsentOpts{
		Report: &tampered, PolicyDigest: fixNew.PolicyDigest, PolicyYAML: newPolicy,
		PublisherFingerprint: fixOld.PublisherFP, PublisherName: fixOld.PublisherName,
		AgentName: fixOld.AgentName, AgentVersion: "0.2.0", State: state,
		IsTTY: false, AcceptPolicyDigest: fixNew.PolicyDigest,
	})
	if err != nil {
		t.Fatalf("approve: %v", err)
	}
	if !strings.Contains(res.CardText, "yaml-added.example.com") {
		t.Fatalf("want YAML-derived diff in card, got:\n%s", res.CardText)
	}
	idx := strings.Index(res.CardText, "Policy changes since last install")
	if idx < 0 {
		t.Fatalf("missing local diff section in card:\n%s", res.CardText)
	}
	diffSection := res.CardText[idx:]
	if strings.Contains(diffSection, "provenance-only.evil.com") {
		t.Fatalf("local diff section must not use signer provenance narrative:\n%s", diffSection)
	}
	if !strings.Contains(diffSection, "yaml-added") {
		t.Fatalf("local diff section missing YAML-derived change:\n%s", diffSection)
	}
	diff, err := ComputeStructuralPolicyDiff(oldPolicy, newPolicy)
	if err != nil {
		t.Fatalf("diff: %v", err)
	}
	formatted := strings.Join(FormatPolicyStructuralDiff(diff), "\n")
	if !strings.Contains(res.CardText, formatted) && !strings.Contains(res.CardText, "yaml-added") {
		t.Fatalf("card diff inconsistent with local ComputeStructuralPolicyDiff")
	}
}

// Claim 6: sentinel secrets must not appear in card, manifest, audit, or diff text.
func TestAdversary_B23T02_NoSecretsInOutputs(t *testing.T) {
	secretPolicy := []byte(`version: "1.0"
agent:
  name: consent-test-agent
egress:
  - domain: "api.example.com"
    ports: [443]
credentials:
  - id: "my-key"
    type: header
    header: "X-API-Key"
    value: "` + adversarySecretSentinel + `"
`)
	fix := writeConsentFixtureBundle(t, secretPolicy, "0.1.0")
	state, root := newConsentState(t)
	var events []auditEvent
	res, err := ResolvePolicyConsent(PolicyConsentOpts{
		Report: fix.InspectReport, PolicyDigest: fix.PolicyDigest, PolicyYAML: secretPolicy,
		PublisherFingerprint: fix.PublisherFP, PublisherName: fix.PublisherName,
		AgentName: fix.AgentName, AgentVersion: fix.AgentVersion, State: state,
		IsTTY: false, AcceptPolicyDigest: fix.PolicyDigest,
		EmitAudit: auditCollector(&events),
	})
	if err != nil {
		t.Fatalf("approve: %v", err)
	}

	diffSelf, err := ComputeStructuralPolicyDiff(secretPolicy, secretPolicy)
	if err != nil {
		t.Fatalf("diff: %v", err)
	}
	diffText := strings.Join(FormatPolicyStructuralDiff(diffSelf), "\n")

	manifestPath := filepath.Join(root, "installs", sanitizePathSegment(fix.PublisherFP), sanitizePathSegment(fix.AgentName), "manifest.json")
	manifestRaw := readFileString(t, manifestPath)
	policyPath := filepath.Join(root, "installs", sanitizePathSegment(fix.PublisherFP), sanitizePathSegment(fix.AgentName), "policy.yaml")
	policyRaw := readFileString(t, policyPath)

	var auditBlob strings.Builder
	for _, e := range events {
		b, _ := json.Marshal(e)
		auditBlob.Write(b)
	}

	// Policy YAML on disk intentionally contains the secret value; operator-facing outputs must not.
	assertSentinelAbsent(t, adversarySecretSentinel,
		res.CardText, manifestRaw, auditBlob.String(), diffText,
	)
	if strings.Contains(policyRaw, adversarySecretSentinel) {
		// ensure we actually stored the secret only on disk in policy.yaml
	} else {
		t.Fatal("fixture policy.yaml missing sentinel value")
	}
}

// Claim 7: non-TTY without --accept-policy returns refused with digest instruction.
func TestAdversary_B23T02_NonTTYMissingAcceptInstruction(t *testing.T) {
	fix := writeConsentFixtureBundle(t, nil, "0.1.0")
	state, root := newConsentState(t)
	before := stateFileCount(t, root)
	_, err := ResolvePolicyConsent(consentOptsBase(fix, state))
	if !errors.Is(err, ErrPolicyRefused) {
		t.Fatalf("want ErrPolicyRefused, got %v", err)
	}
	var pre *PolicyRefusedError
	if !errors.As(err, &pre) {
		t.Fatalf("want *PolicyRefusedError, got %T", err)
	}
	msg := pre.DisplayMessage()
	if !strings.Contains(msg, fix.PolicyDigest) {
		t.Fatalf("instruction missing digest: %q", msg)
	}
	if !strings.Contains(msg, "--accept-policy") {
		t.Fatalf("instruction missing flag: %q", msg)
	}
	assertNoStateGrowth(t, root, before)
}

// Claim 8: unverified inspect report must fail closed with no consent card.
func TestAdversary_B23T02_UnverifiedReportRejectedNoCard(t *testing.T) {
	fix := writeConsentFixtureBundle(t, nil, "0.1.0")
	state, root := newConsentState(t)
	before := stateFileCount(t, root)
	bad := *fix.InspectReport
	bad.Verified = false

	card := bundle.FormatConsentCard(&bad, bundle.ConsentCardOpts{
		Mode: bundle.ConsentCardFull, AgentName: fix.AgentName, AgentVersion: fix.AgentVersion,
	})
	if card != "" {
		t.Fatalf("FormatConsentCard must return empty for unverified report, got: %q", card)
	}

	_, err := ResolvePolicyConsent(PolicyConsentOpts{
		Report: &bad, PolicyDigest: fix.PolicyDigest, PolicyYAML: fix.PolicyYAML,
		PublisherFingerprint: fix.PublisherFP, PublisherName: fix.PublisherName,
		AgentName: fix.AgentName, AgentVersion: fix.AgentVersion, State: state,
		IsTTY: false, AcceptPolicyDigest: fix.PolicyDigest,
	})
	if err == nil {
		t.Fatal("expected error for unverified report")
	}
	if !strings.Contains(err.Error(), "verified inspect report") {
		t.Fatalf("unexpected error: %v", err)
	}
	assertNoStateGrowth(t, root, before)
}

// Claim 9: persisted manifest digest equals lock policy_digest exactly (length + value).
func TestAdversary_B23T02_ManifestDigestEqualsLockExact(t *testing.T) {
	fix := writeConsentFixtureBundle(t, nil, "0.1.0")
	state, root := newConsentState(t)
	res, err := ResolvePolicyConsent(PolicyConsentOpts{
		Report: fix.InspectReport, PolicyDigest: fix.PolicyDigest, PolicyYAML: fix.PolicyYAML,
		PublisherFingerprint: fix.PublisherFP, PublisherName: fix.PublisherName,
		AgentName: fix.AgentName, AgentVersion: fix.AgentVersion, State: state,
		IsTTY: false, AcceptPolicyDigest: fix.PolicyDigest,
	})
	if err != nil {
		t.Fatalf("approve: %v", err)
	}
	if res.Manifest.AcceptedPolicyDigest != fix.PolicyDigest {
		t.Fatalf("in-memory manifest digest %q != lock %q", res.Manifest.AcceptedPolicyDigest, fix.PolicyDigest)
	}
	recomputed, err := pack.ComputePolicyDigest(fix.PolicyYAML)
	if err != nil {
		t.Fatalf("recompute: %v", err)
	}
	if fix.PolicyDigest != recomputed {
		t.Fatalf("fixture lock digest != recompute (test setup)")
	}

	manifestPath := filepath.Join(root, "installs", sanitizePathSegment(fix.PublisherFP), sanitizePathSegment(fix.AgentName), "manifest.json")
	var onDisk InstallManifest
	if err := json.Unmarshal([]byte(readFileString(t, manifestPath)), &onDisk); err != nil {
		t.Fatalf("parse manifest: %v", err)
	}
	if onDisk.AcceptedPolicyDigest != fix.PolicyDigest {
		t.Fatalf("on-disk digest %q != lock %q", onDisk.AcceptedPolicyDigest, fix.PolicyDigest)
	}
	if len(onDisk.AcceptedPolicyDigest) != len(fix.PolicyDigest) {
		t.Fatalf("digest length mismatch (prefix attack?) on-disk=%d lock=%d", len(onDisk.AcceptedPolicyDigest), len(fix.PolicyDigest))
	}
}

// Claim 10: all paths under StateRoot use 0700 dirs and 0600 files after approval.
func TestAdversary_B23T02_StateTreePermissions0700_0600(t *testing.T) {
	fix := writeConsentFixtureBundle(t, nil, "0.1.0")
	state, root := newConsentState(t)
	_, err := ResolvePolicyConsent(PolicyConsentOpts{
		Report: fix.InspectReport, PolicyDigest: fix.PolicyDigest, PolicyYAML: fix.PolicyYAML,
		PublisherFingerprint: fix.PublisherFP, PublisherName: fix.PublisherName,
		AgentName: fix.AgentName, AgentVersion: fix.AgentVersion, State: state,
		IsTTY: false, AcceptPolicyDigest: fix.PolicyDigest,
	})
	if err != nil {
		t.Fatalf("approve: %v", err)
	}
	walkStatePermissions(t, root)
}

// Claim 2: prompt I/O error on TTY path — zero writes.
func TestAdversary_B23T02_TTYPromptErrorZeroWrites(t *testing.T) {
	fix := writeConsentFixtureBundle(t, nil, "0.1.0")
	state, root := newConsentState(t)
	before := stateFileCount(t, root)
	promptErr := func(string) (string, error) {
		return "", errors.New("input cancelled")
	}
	_, err := ResolvePolicyConsent(PolicyConsentOpts{
		Report: fix.InspectReport, PolicyDigest: fix.PolicyDigest, PolicyYAML: fix.PolicyYAML,
		PublisherFingerprint: fix.PublisherFP, PublisherName: fix.PublisherName,
		AgentName: fix.AgentName, AgentVersion: fix.AgentVersion, State: state,
		IsTTY: true, Prompt: promptErr,
	})
	if !errors.Is(err, ErrPolicyRefused) {
		t.Fatalf("want ErrPolicyRefused, got %v", err)
	}
	assertNoStateGrowth(t, root, before)
}

// Claim 1: empty lock policy_digest at API boundary — fail before writes.
func TestAdversary_B23T02_EmptyLockDigestRejected(t *testing.T) {
	fix := writeConsentFixtureBundle(t, nil, "0.1.0")
	state, root := newConsentState(t)
	before := stateFileCount(t, root)
	_, err := ResolvePolicyConsent(PolicyConsentOpts{
		Report: fix.InspectReport, PolicyDigest: "   ", PolicyYAML: fix.PolicyYAML,
		PublisherFingerprint: fix.PublisherFP, PublisherName: fix.PublisherName,
		AgentName: fix.AgentName, AgentVersion: fix.AgentVersion, State: state,
		IsTTY: false, AcceptPolicyDigest: fix.PolicyDigest,
	})
	if err == nil {
		t.Fatal("expected error for empty policy digest")
	}
	assertNoStateGrowth(t, root, before)
}

// Claim 6 extension: audit payload on approval must not echo sentinel from policy value field.
func TestAdversary_B23T02_AuditPayloadNoSecretOnApproval(t *testing.T) {
	secretPolicy := []byte(`version: "1.0"
agent:
  name: consent-test-agent
egress:
  - domain: "api.example.com"
    ports: [443]
credentials:
  - id: "k"
    type: header
    header: "X-API-Key"
    value: "` + adversarySecretSentinel + `"
`)
	fix := writeConsentFixtureBundle(t, secretPolicy, "0.1.0")
	state, _ := newConsentState(t)
	var events []auditEvent
	_, err := ResolvePolicyConsent(PolicyConsentOpts{
		Report: fix.InspectReport, PolicyDigest: fix.PolicyDigest, PolicyYAML: secretPolicy,
		PublisherFingerprint: fix.PublisherFP, PublisherName: fix.PublisherName,
		AgentName: fix.AgentName, AgentVersion: fix.AgentVersion, State: state,
		IsTTY: false, AcceptPolicyDigest: fix.PolicyDigest,
		EmitAudit: auditCollector(&events),
	})
	if err != nil {
		t.Fatalf("approve: %v", err)
	}
	for _, e := range events {
		if e.EventType != audit.EventTypeInstallPolicyApproved {
			continue
		}
		for k, v := range e.Payload {
			if strings.Contains(v, adversarySecretSentinel) || strings.Contains(k, adversarySecretSentinel) {
				t.Fatalf("audit leaked sentinel: %v", e.Payload)
			}
		}
	}
}