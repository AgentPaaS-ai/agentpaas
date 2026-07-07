package bundle

import (
	"fmt"
	"strings"
	"testing"

	"github.com/AgentPaaS-ai/agentpaas/internal/pack"
)

func adversaryMinimalReport(t *testing.T, entries []pack.ProvenanceEntrySummary) *InspectReport {
	t.Helper()
	prov := &pack.ProvenanceReport{Entries: entries, Verified: true}
	lints := ComputeChainLints(prov)
	return &InspectReport{
		Verified: true,
		Header:   InspectHeader{AgentName: "adv-agent", AgentVersion: "1.0.0"},
		Publisher: &InspectPublisher{
			Name: "tail-pub", FingerprintDisplay: "ABCD", TrustDisclaimer: D3TrustDisclaimer,
		},
		Provenance:    prov,
		PolicyLints:   lints,
		PolicySummary: []PolicySummaryLine{{Section: "egress", Detail: "none"}},
		Requirements:  &InspectRequirements{Image: "rebuild"},
	}
}

func adversaryCard(t *testing.T, r *InspectReport, hops map[int]bool) string {
	t.Helper()
	return FormatConsentCard(r, ConsentCardOpts{
		Mode: ConsentCardFull, AgentName: r.Header.AgentName, AgentVersion: r.Header.AgentVersion,
		LocallyVerifiedHops: hops,
	})
}

func TestAdversary_SingleHop_NoTailAnchorChainOrEgressLint(t *testing.T) {
	report := openInspectReport(t, writeInspectFixtureBundle(t, 1, nil, nil))
	card := adversaryCard(t, report, nil)
	if strings.Contains(card, "Earlier signers are lineage claims.") {
		t.Fatalf("tail-anchor must not appear:\n%s", card)
	}
	if strings.Contains(card, "Provenance chain\n") {
		t.Fatalf("chain section must not appear:\n%s", card)
	}
	for _, lint := range report.PolicyLints {
		if lint.Code == LintChainAddsEgress {
			t.Fatalf("single-hop must not have chain egress lint: %+v", lint)
		}
	}
	if strings.Contains(card, LintChainAddsEgress) || strings.Contains(card, "chain adds egress") {
		t.Fatalf("card must not show chain egress lint:\n%s", card)
	}
}

func TestAdversary_TwoHop_NilDelta_NoCrashShowsNoPolicyChanges(t *testing.T) {
	report := adversaryMinimalReport(t, []pack.ProvenanceEntrySummary{
		{Index: 0, Action: "created", PublisherName: "orig"},
		{Index: 1, Action: "forked", PublisherName: "forker", PolicyDelta: nil},
	})
	card := adversaryCard(t, report, nil)
	if !strings.Contains(card, "hop 1: forked by forker — no policy changes (signer-claimed)") {
		t.Fatalf("expected nil delta rendering:\n%s", card)
	}
}

func TestAdversary_TwoHop_Hop1Egress_LintListsDomains(t *testing.T) {
	report := adversaryMinimalReport(t, []pack.ProvenanceEntrySummary{
		{Index: 0, Action: "created", PublisherName: "orig"},
		{Index: 1, Action: "forked", PublisherName: "forker", PolicyDelta: &pack.PolicyDelta{
			EgressAdded: []string{"added-one.example.com:443", "added-two.example.com:80"},
		}},
	})
	if len(report.PolicyLints) != 1 || report.PolicyLints[0].Code != LintChainAddsEgress {
		t.Fatalf("lints = %+v", report.PolicyLints)
	}
	msg := report.PolicyLints[0].Message
	if !strings.Contains(msg, "added-one.example.com:443") || !strings.Contains(msg, "added-two.example.com:80") {
		t.Fatalf("lint message missing domains: %s", msg)
	}
	card := adversaryCard(t, report, nil)
	if !strings.Contains(card, msg) {
		t.Fatalf("card missing lint line:\n%s", card)
	}
}

func TestAdversary_ThreeHop_Hop2OnlyEgress_LintStillFires(t *testing.T) {
	report := adversaryMinimalReport(t, []pack.ProvenanceEntrySummary{
		{Index: 0, Action: "created", PublisherName: "orig"},
		{Index: 1, Action: "forked", PublisherName: "mid", PolicyDelta: &pack.PolicyDelta{EgressRemoved: []string{"old.example.com:443"}}},
		{Index: 2, Action: "forked", PublisherName: "tail", PolicyDelta: &pack.PolicyDelta{EgressAdded: []string{"only-hop2.evil.com:443"}}},
	})
	lints := ComputeChainLints(report.Provenance)
	if len(lints) != 1 || !strings.Contains(lints[0].Message, "only-hop2.evil.com:443") {
		t.Fatalf("expected egress from hop 2 only, got %+v", lints)
	}
}

func TestAdversary_DeltaRendering_AddAndRemoveEgress(t *testing.T) {
	summary := formatPolicyDeltaSummary(&pack.PolicyDelta{
		EgressAdded:   []string{"new.example.com:443"},
		EgressRemoved: []string{"gone.example.com:80"},
	})
	if !strings.Contains(summary, "+egress new.example.com:443") || !strings.Contains(summary, "-egress gone.example.com:80") {
		t.Fatalf("delta summary = %q", summary)
	}
}

func TestAdversary_TamperProvenanceDelta_LintsUnchangedFromInspect(t *testing.T) {
	report := openInspectReport(t, writeInspectFixtureBundle(t, 2, nil, nil))
	var inspectLint PolicyLint
	for _, lint := range report.PolicyLints {
		if lint.Code == LintChainAddsEgress {
			inspectLint = lint
			break
		}
	}
	if inspectLint.Code == "" {
		t.Fatal("fixture 2-hop should have chain egress lint at inspect time")
	}
	if len(report.Provenance.Entries) >= 2 {
		report.Provenance.Entries[1].PolicyDelta = nil
	}
	card := adversaryCard(t, report, nil)
	if !strings.Contains(card, inspectLint.Message) {
		t.Fatalf("card must still show inspect-time lint %q after provenance tamper:\n%s", inspectLint.Message, card)
	}
	for _, lint := range report.PolicyLints {
		if lint.Code == LintChainAddsEgress && lint.Message != inspectLint.Message {
			t.Fatalf("PolicyLints mutated: %+v vs %+v", lint, inspectLint)
		}
	}
	if strings.Contains(card, "hop 1: forked by test-publisher — +egress") {
		t.Fatalf("hop line must follow tampered provenance (no +egress in hop line):\n%s", card)
	}
}

func TestAdversary_EmptyProvenance_NoChainSectionNoPanic(t *testing.T) {
	report := &InspectReport{
		Verified: true,
		Header:   InspectHeader{AgentName: "a", AgentVersion: "1"},
		Publisher: &InspectPublisher{Name: "p", FingerprintDisplay: "x", TrustDisclaimer: D3TrustDisclaimer},
		Provenance: &pack.ProvenanceReport{Entries: nil},
		Requirements: &InspectRequirements{Image: "rebuild"},
	}
	card := adversaryCard(t, report, nil)
	if strings.Contains(card, "Provenance chain") || strings.Contains(card, "Earlier signers") {
		t.Fatalf("empty provenance must not show chain UI:\n%s", card)
	}
	if ComputeChainLints(report.Provenance) != nil {
		t.Fatal("empty provenance must not produce chain lints")
	}
}

func TestAdversary_TenHop_AllHopsRenderedInOrder(t *testing.T) {
	report := openInspectReport(t, writeInspectFixtureBundle(t, 10, nil, nil))
	if report.Provenance == nil || len(report.Provenance.Entries) != 10 {
		t.Fatalf("entries = %d", len(report.Provenance.Entries))
	}
	card := adversaryCard(t, report, nil)
	for i := 1; i < 10; i++ {
		needle := fmt.Sprintf("hop %d:", i)
		if !strings.Contains(card, needle) {
			t.Fatalf("missing %s in card:\n%s", needle, card)
		}
	}
	if !strings.Contains(card, "You are trusting test-publisher. Earlier signers are lineage claims.") {
		t.Fatal("missing tail anchor for 10-hop")
	}
}

func TestAdversary_MultiHop_TailAnchorUsesTailPublisher(t *testing.T) {
	report := adversaryMinimalReport(t, []pack.ProvenanceEntrySummary{
		{Index: 0, Action: "created", PublisherName: "first"},
		{Index: 1, Action: "forked", PublisherName: "middle"},
		{Index: 2, Action: "forked", PublisherName: "tail-name"},
	})
	card := adversaryCard(t, report, nil)
	if !strings.Contains(card, "You are trusting tail-name. Earlier signers are lineage claims.") {
		t.Fatalf("tail anchor must name last hop publisher:\n%s", card)
	}
}

func TestAdversary_LocallyVerifiedSuffix_OnlyWhenMapSaysSo(t *testing.T) {
	report := adversaryMinimalReport(t, []pack.ProvenanceEntrySummary{
		{Index: 0, Action: "created", PublisherName: "o"},
		{Index: 1, Action: "forked", PublisherName: "f1"},
		{Index: 2, Action: "forked", PublisherName: "f2"},
	})
	mixed := map[int]bool{1: true}
	card := adversaryCard(t, report, mixed)
	if !strings.Contains(card, "hop 1:") || !strings.Contains(card, "(locally verified)") {
		t.Fatalf("hop 1 should be locally verified:\n%s", card)
	}
	if !strings.Contains(card, "hop 2:") || !strings.Contains(card, "hop 2: forked by f2") {
		t.Fatalf("hop 2 missing:\n%s", card)
	}
	if strings.Count(card, "(locally verified)") != 1 {
		t.Fatalf("only hop 1 verified, card:\n%s", card)
	}
	if !strings.Contains(card, "hop 2: forked by f2") || !strings.Contains(card, "(signer-claimed)") {
		t.Fatalf("hop 2 must be signer-claimed:\n%s", card)
	}
}