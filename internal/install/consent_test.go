package install

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/AgentPaaS-ai/agentpaas/internal/audit"
	"github.com/AgentPaaS-ai/agentpaas/internal/bundle"
)

func TestFormatConsentCard_Golden(t *testing.T) {
	fix := writeConsentFixtureBundle(t, nil, "0.1.0")
	card := bundle.FormatConsentCard(fix.InspectReport, bundle.ConsentCardOpts{
		Mode:         bundle.ConsentCardFull,
		AgentName:    fix.AgentName,
		AgentVersion: fix.AgentVersion,
	})
	if card == "" {
		t.Fatal("empty card")
	}
	wantSubstrings := []string{
		"INSTALL POLICY APPROVAL",
		"consent-publisher",
		"Policy summary",
		"api.example.com:443",
		"Install mode:",
		"SBOM package count:",
		bundle.D3TrustDisclaimer,
	}
	for _, s := range wantSubstrings {
		if !strings.Contains(card, s) {
			t.Fatalf("golden card missing %q\n%s", s, card)
		}
	}
	// Deterministic: same inputs produce identical card text.
	card2 := bundle.FormatConsentCard(fix.InspectReport, bundle.ConsentCardOpts{
		Mode: bundle.ConsentCardFull, AgentName: fix.AgentName, AgentVersion: fix.AgentVersion,
	})
	if card != card2 {
		t.Fatalf("card not deterministic")
	}
}

func TestResolvePolicyConsent_NonTTY_CorrectDigest(t *testing.T) {
	fix := writeConsentFixtureBundle(t, nil, "0.1.0")
	state, root := newConsentState(t)
	before := stateFileCount(t, root)
	var events []auditEvent
	res, err := ResolvePolicyConsent(PolicyConsentOpts{
		Report:               fix.InspectReport,
		PolicyDigest:         fix.PolicyDigest,
		PolicyYAML:           fix.PolicyYAML,
		PublisherFingerprint: fix.PublisherFP,
		PublisherName:        fix.PublisherName,
		AgentName:            fix.AgentName,
		AgentVersion:         fix.AgentVersion,
		State:                state,
		IsTTY:                false,
		AcceptPolicyDigest:   fix.PolicyDigest,
		EmitAudit:            auditCollector(&events),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Manifest.AcceptedPolicyDigest != fix.PolicyDigest {
		t.Fatalf("digest = %q", res.Manifest.AcceptedPolicyDigest)
	}
	if stateFileCount(t, root) <= before {
		t.Fatal("expected state writes on approval")
	}
	found := false
	for _, e := range events {
		if e.EventType == audit.EventTypeInstallPolicyApproved {
			found = true
			if e.Payload["policy_digest"] != fix.PolicyDigest {
				t.Fatalf("audit digest: %+v", e.Payload)
			}
		}
	}
	if !found {
		t.Fatal("missing install_policy_approved audit")
	}
}

func TestResolvePolicyConsent_NonTTY_WrongDigest(t *testing.T) {
	fix := writeConsentFixtureBundle(t, nil, "0.1.0")
	state, root := newConsentState(t)
	before := stateFileCount(t, root)
	_, err := ResolvePolicyConsent(PolicyConsentOpts{
		Report: fix.InspectReport, PolicyDigest: fix.PolicyDigest, PolicyYAML: fix.PolicyYAML,
		PublisherFingerprint: fix.PublisherFP, PublisherName: fix.PublisherName,
		AgentName: fix.AgentName, AgentVersion: fix.AgentVersion, State: state,
		IsTTY: false, AcceptPolicyDigest: "deadbeef",
	})
	if !errors.Is(err, ErrPolicyMismatch) {
		t.Fatalf("want ErrPolicyMismatch, got %v", err)
	}
	assertNoStateGrowth(t, root, before)
}

func TestResolvePolicyConsent_NonTTY_AbsentDigest(t *testing.T) {
	fix := writeConsentFixtureBundle(t, nil, "0.1.0")
	state, root := newConsentState(t)
	before := stateFileCount(t, root)
	_, err := ResolvePolicyConsent(PolicyConsentOpts{
		Report: fix.InspectReport, PolicyDigest: fix.PolicyDigest, PolicyYAML: fix.PolicyYAML,
		PublisherFingerprint: fix.PublisherFP, PublisherName: fix.PublisherName,
		AgentName: fix.AgentName, AgentVersion: fix.AgentVersion, State: state, IsTTY: false,
	})
	if !errors.Is(err, ErrPolicyRefused) {
		t.Fatalf("want ErrPolicyRefused, got %v", err)
	}
	assertNoStateGrowth(t, root, before)
}

func TestResolvePolicyConsent_TTY_Approve(t *testing.T) {
	fix := writeConsentFixtureBundle(t, nil, "0.1.0")
	state, root := newConsentState(t)
	_, err := ResolvePolicyConsent(PolicyConsentOpts{
		Report: fix.InspectReport, PolicyDigest: fix.PolicyDigest, PolicyYAML: fix.PolicyYAML,
		PublisherFingerprint: fix.PublisherFP, PublisherName: fix.PublisherName,
		AgentName: fix.AgentName, AgentVersion: fix.AgentVersion, State: state,
		IsTTY: true, Prompt: promptSingle("approve"),
	})
	if err != nil {
		t.Fatalf("approve: %v", err)
	}
	if stateFileCount(t, root) == 0 {
		t.Fatal("expected state after approval")
	}
}

func TestResolvePolicyConsent_TTY_NoThenApprove(t *testing.T) {
	fix := writeConsentFixtureBundle(t, nil, "0.1.0")
	state, _ := newConsentState(t)
	_, err := ResolvePolicyConsent(PolicyConsentOpts{
		Report: fix.InspectReport, PolicyDigest: fix.PolicyDigest, PolicyYAML: fix.PolicyYAML,
		PublisherFingerprint: fix.PublisherFP, PublisherName: fix.PublisherName,
		AgentName: fix.AgentName, AgentVersion: fix.AgentVersion, State: state,
		IsTTY: true, Prompt: promptSequence("no", "approve"),
	})
	if err != nil {
		t.Fatalf("no then approve: %v", err)
	}
}

func TestResolvePolicyConsent_TTY_WrongThreeTimes(t *testing.T) {
	fix := writeConsentFixtureBundle(t, nil, "0.1.0")
	state, root := newConsentState(t)
	before := stateFileCount(t, root)
	_, err := ResolvePolicyConsent(PolicyConsentOpts{
		Report: fix.InspectReport, PolicyDigest: fix.PolicyDigest, PolicyYAML: fix.PolicyYAML,
		PublisherFingerprint: fix.PublisherFP, PublisherName: fix.PublisherName,
		AgentName: fix.AgentName, AgentVersion: fix.AgentVersion, State: state,
		IsTTY: true, Prompt: promptSequence("nope", "nope", "nope"),
	})
	if !errors.Is(err, ErrPolicyRefused) {
		t.Fatalf("want ErrPolicyRefused, got %v", err)
	}
	assertNoStateGrowth(t, root, before)
}

func TestResolvePolicyConsent_Update_SamePolicyAbbreviated(t *testing.T) {
	fix := writeConsentFixtureBundle(t, nil, "0.2.0")
	state, _ := newConsentState(t)
	priorManifest := InstallManifest{
		PublisherFingerprint: fix.PublisherFP, PublisherName: fix.PublisherName,
		AgentName: fix.AgentName, AgentVersion: "0.1.0", AcceptedPolicyDigest: fix.PolicyDigest,
	}
	if err := state.SaveApprovedInstall(priorManifest, fix.PolicyYAML); err != nil {
		t.Fatalf("seed prior: %v", err)
	}
	res, err := ResolvePolicyConsent(PolicyConsentOpts{
		Report: fix.InspectReport, PolicyDigest: fix.PolicyDigest, PolicyYAML: fix.PolicyYAML,
		PublisherFingerprint: fix.PublisherFP, PublisherName: fix.PublisherName,
		AgentName: fix.AgentName, AgentVersion: "0.2.0", State: state,
		IsTTY: false, AcceptPolicyDigest: fix.PolicyDigest,
	})
	if err != nil {
		t.Fatalf("update same policy: %v", err)
	}
	if !strings.Contains(res.CardText, "Policy unchanged since last approval") {
		t.Fatalf("want abbreviated card, got:\n%s", res.CardText)
	}
}

func TestResolvePolicyConsent_Update_ChangedPolicyDiffAndReapproval(t *testing.T) {
	oldPolicy := []byte(consentBasePolicyYAML)
	newPolicy := []byte(`version: "1.0"
agent:
  name: consent-test-agent
egress:
  - domain: "api.example.com"
    ports: [443]
  - domain: "added.example.com"
    ports: [443]
credentials:
  - id: "my-key"
    type: header
    header: "X-API-Key"
    value: "x"
`)
	fixOld := writeConsentFixtureBundle(t, oldPolicy, "0.1.0")
	fixNew := writeConsentFixtureBundle(t, newPolicy, "0.2.0")
	state, root := newConsentState(t)
	if err := state.SaveApprovedInstall(InstallManifest{
		PublisherFingerprint: fixOld.PublisherFP, PublisherName: fixOld.PublisherName,
		AgentName: fixOld.AgentName, AgentVersion: "0.1.0",
		AcceptedPolicyDigest: fixOld.PolicyDigest,
	}, oldPolicy); err != nil {
		t.Fatalf("seed: %v", err)
	}
	before := stateFileCount(t, root)
	_, err := ResolvePolicyConsent(PolicyConsentOpts{
		Report: fixNew.InspectReport, PolicyDigest: fixNew.PolicyDigest, PolicyYAML: newPolicy,
		PublisherFingerprint: fixNew.PublisherFP, PublisherName: fixNew.PublisherName,
		AgentName: fixNew.AgentName, AgentVersion: "0.2.0", State: state,
		IsTTY: false, AcceptPolicyDigest: fixOld.PolicyDigest,
	})
	if !errors.Is(err, ErrPolicyMismatch) {
		t.Fatalf("want mismatch on stale digest, got %v", err)
	}
	assertNoStateGrowth(t, root, before)

	res, err := ResolvePolicyConsent(PolicyConsentOpts{
		Report: fixNew.InspectReport, PolicyDigest: fixNew.PolicyDigest, PolicyYAML: newPolicy,
		PublisherFingerprint: fixNew.PublisherFP, PublisherName: fixNew.PublisherName,
		AgentName: fixNew.AgentName, AgentVersion: "0.2.0", State: state,
		IsTTY: false, AcceptPolicyDigest: fixNew.PolicyDigest,
	})
	if err != nil {
		t.Fatalf("re-approve: %v", err)
	}
	if !strings.Contains(res.CardText, "added.example.com") {
		t.Fatalf("diff missing added domain:\n%s", res.CardText)
	}
}

func TestResolvePolicyConsent_DowngradeRefused(t *testing.T) {
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

func TestResolvePolicyConsent_DowngradeAllowed(t *testing.T) {
	fix := writeConsentFixtureBundle(t, nil, "0.1.0")
	state, _ := newConsentState(t)
	if err := state.SaveApprovedInstall(InstallManifest{
		PublisherFingerprint: fix.PublisherFP, PublisherName: fix.PublisherName,
		AgentName: fix.AgentName, AgentVersion: "0.2.0", AcceptedPolicyDigest: fix.PolicyDigest,
	}, fix.PolicyYAML); err != nil {
		t.Fatalf("seed: %v", err)
	}
	var events []auditEvent
	_, err := ResolvePolicyConsent(PolicyConsentOpts{
		Report: fix.InspectReport, PolicyDigest: fix.PolicyDigest, PolicyYAML: fix.PolicyYAML,
		PublisherFingerprint: fix.PublisherFP, PublisherName: fix.PublisherName,
		AgentName: fix.AgentName, AgentVersion: "0.1.0", State: state,
		IsTTY: false, AcceptPolicyDigest: fix.PolicyDigest, AllowDowngrade: true,
		EmitAudit: auditCollector(&events),
	})
	if err != nil {
		t.Fatalf("downgrade allowed: %v", err)
	}
	found := false
	for _, e := range events {
		if e.EventType == audit.EventTypeInstallDowngradeAllowed {
			found = true
		}
	}
	if !found {
		t.Fatal("missing install_downgrade_allowed audit")
	}
}

func TestFileInstallState_DirMode0700(t *testing.T) {
	fix := writeConsentFixtureBundle(t, nil, "0.1.0")
	state, root := newConsentState(t)
	m := InstallManifest{
		PublisherFingerprint: fix.PublisherFP, PublisherName: fix.PublisherName,
		AgentName: fix.AgentName, AgentVersion: fix.AgentVersion,
		AcceptedPolicyDigest: fix.PolicyDigest,
	}
	if err := state.SaveApprovedInstall(m, fix.PolicyYAML); err != nil {
		t.Fatalf("save: %v", err)
	}
	installs := root + "/installs"
	info, err := os.Stat(installs)
	if err != nil {
		t.Fatalf("stat installs: %v", err)
	}
	if info.Mode().Perm() != 0o700 {
		t.Fatalf("installs dir mode = %o, want 0700", info.Mode().Perm())
	}
}