package bundle

import (
	"strings"
	"testing"

	"github.com/AgentPaaS-ai/agentpaas/internal/pack"
)

func verifiedInspectReport(t *testing.T, provEntries int) *InspectReport {
	t.Helper()
	path := writeInspectFixtureBundle(t, provEntries, nil, nil)
	return openInspectReport(t, path)
}

func TestFormatConsentCard_ThreeHop_TailAnchorDeltasEgressLint(t *testing.T) {
	report := verifiedInspectReport(t, 3)
	card := FormatConsentCard(report, ConsentCardOpts{
		Mode: ConsentCardFull, AgentName: report.Header.AgentName, AgentVersion: report.Header.AgentVersion,
	})
	if !strings.Contains(card, "You are trusting test-publisher. Earlier signers are lineage claims.") {
		t.Fatalf("missing tail anchor:\n%s", card)
	}
	if !strings.Contains(card, "Provenance chain") {
		t.Fatal("missing provenance chain section")
	}
	if !strings.Contains(card, "+egress api.slack.com:443") {
		t.Fatalf("missing hop delta:\n%s", card)
	}
	if !strings.Contains(card, "(signer-claimed)") {
		t.Fatal("expected signer-claimed suffix")
	}
	found := false
	for _, lint := range report.PolicyLints {
		if lint.Code == LintChainAddsEgress {
			found = true
			if !strings.Contains(lint.Message, "api.slack.com:443") {
				t.Fatalf("lint message: %s", lint.Message)
			}
		}
	}
	if !found {
		t.Fatalf("missing %s lint: %+v", LintChainAddsEgress, report.PolicyLints)
	}
	if !strings.Contains(card, LintChainAddsEgress) {
		t.Fatalf("card missing chain egress lint:\n%s", card)
	}
}

func TestFormatConsentCard_TwoHop_TailAnchorPresent(t *testing.T) {
	report := verifiedInspectReport(t, 2)
	card := FormatConsentCard(report, ConsentCardOpts{Mode: ConsentCardFull})
	if !strings.Contains(card, "Earlier signers are lineage claims.") {
		t.Fatalf("missing tail anchor:\n%s", card)
	}
	if !strings.Contains(card, "hop 1:") {
		t.Fatalf("missing hop line:\n%s", card)
	}
}

func TestFormatConsentCard_SingleHop_NoTailAnchor(t *testing.T) {
	report := verifiedInspectReport(t, 1)
	card := FormatConsentCard(report, ConsentCardOpts{Mode: ConsentCardFull})
	if strings.Contains(card, "Earlier signers are lineage claims.") {
		t.Fatalf("tail anchor must not appear for single hop:\n%s", card)
	}
	if strings.Contains(card, "Provenance chain\n") {
		t.Fatalf("chain section must not appear for single hop:\n%s", card)
	}
}

func TestComputeChainLints_EgressAndClean(t *testing.T) {
	withEgress := &pack.ProvenanceReport{Entries: []pack.ProvenanceEntrySummary{
		{Index: 0, Action: "created"},
		{Index: 1, Action: "forked", PolicyDelta: &pack.PolicyDelta{EgressAdded: []string{"evil.com:80"}}},
	}}
	lints := ComputeChainLints(withEgress)
	if len(lints) != 1 || lints[0].Code != LintChainAddsEgress {
		t.Fatalf("lints = %+v", lints)
	}
	clean := &pack.ProvenanceReport{Entries: []pack.ProvenanceEntrySummary{
		{Index: 1, Action: "forked", PolicyDelta: &pack.PolicyDelta{EgressRemoved: []string{"old.example.com:80"}}},
	}}
	if got := ComputeChainLints(clean); len(got) != 0 {
		t.Fatalf("want no chain lint, got %+v", got)
	}
}

func TestFormatConsentCard_LocallyVerifiedSuffix(t *testing.T) {
	report := &InspectReport{
		Verified: true,
		Header:   InspectHeader{AgentName: "a", AgentVersion: "1"},
		Publisher: &InspectPublisher{Name: "tail", FingerprintDisplay: "abcd", TrustDisclaimer: D3TrustDisclaimer},
		Provenance: &pack.ProvenanceReport{Entries: []pack.ProvenanceEntrySummary{
			{Index: 0, Action: "created", PublisherName: "orig"},
			{Index: 1, Action: "forked", PublisherName: "forker", PolicyDelta: nil},
		}},
		PolicySummary: []PolicySummaryLine{{Section: "egress", Detail: "x"}},
		Requirements:  &InspectRequirements{Image: "rebuild"},
	}
	cardClaimed := FormatConsentCard(report, ConsentCardOpts{Mode: ConsentCardFull, LocallyVerifiedHops: nil})
	if !strings.Contains(cardClaimed, "no policy changes (signer-claimed)") {
		t.Fatalf("got:\n%s", cardClaimed)
	}
	cardVerified := FormatConsentCard(report, ConsentCardOpts{
		Mode: ConsentCardFull, LocallyVerifiedHops: map[int]bool{1: true},
	})
	if !strings.Contains(cardVerified, "(locally verified)") {
		t.Fatalf("got:\n%s", cardVerified)
	}
	if strings.Contains(cardVerified, "(signer-claimed)") {
		t.Fatal("should not contain signer-claimed when locally verified")
	}
}

func TestFormatPolicyDeltaSummary_NilAndEgress(t *testing.T) {
	if got := formatPolicyDeltaSummary(nil); got != "no policy changes" {
		t.Fatalf("nil delta = %q", got)
	}
	got := formatPolicyDeltaSummary(&pack.PolicyDelta{EgressAdded: []string{"api.slack.com:443"}})
	if got != "+egress api.slack.com:443" {
		t.Fatalf("got %q", got)
	}
}