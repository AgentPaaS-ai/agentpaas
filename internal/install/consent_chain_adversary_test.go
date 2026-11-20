package install

import (
	"strings"
	"testing"

	"github.com/AgentPaaS-ai/agentpaas/internal/bundle"
	"github.com/AgentPaaS-ai/agentpaas/internal/pack"
)

type adversaryStubState struct {
	prior *PriorInstallRecord
}

func (s *adversaryStubState) GetPriorInstall(publisherFingerprint, agentName string) (*PriorInstallRecord, error) {
	return s.prior, nil
}

func (s *adversaryStubState) SaveApprovedInstall(manifest InstallManifest, policyYAML []byte) error {
	return nil
}

func (s *adversaryStubState) GetInstallByRef(ref string) (*PriorInstallRecord, error) {
	return nil, nil
}

func TestAdversary_LocallyVerified_WrongParentDigest_SignerClaimed(t *testing.T) {
	parent := writeConsentFixtureBundle(t, nil, "0.1.0")
	state, root := newConsentState(t)
	pb, err := bundle.Open(parent.Path)
	if err != nil {
		t.Fatalf("open parent: %v", err)
	}
	seedInstalledAgentLock(t, root, parent, pb.Lock)
	if err := pb.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	wrongDigest := "sha256:0000000000000000000000000000000000000000000000000000000000000001"
	child := writeForkedConsentBundle(t, wrongDigest, "0.2.0")
	hops := ComputeLocallyVerifiedHops(child.InspectReport, state)
	if hops != nil && hops[1] {
		t.Fatalf("wrong parent digest must not verify: %+v", hops)
	}
	res, err := ResolvePolicyConsent(PolicyConsentOpts{
		Report: child.InspectReport, PolicyDigest: child.PolicyDigest, PolicyYAML: child.PolicyYAML,
		PublisherFingerprint: child.PublisherFP, PublisherName: child.PublisherName,
		AgentName: child.AgentName, AgentVersion: child.AgentVersion, State: state,
		IsTTY: false, AcceptPolicyDigest: child.PolicyDigest,
	})
	if err != nil {
		t.Fatalf("consent: %v", err)
	}
	if strings.Contains(res.CardText, "(locally verified)") {
		t.Fatalf("must be signer-claimed when digest mismatches despite parent install:\n%s", res.CardText)
	}
	if !strings.Contains(res.CardText, "(signer-claimed)") {
		t.Fatalf("expected signer-claimed:\n%s", res.CardText)
	}
}

func TestAdversary_LocallyVerified_MatchingDigest_MarkedVerified(t *testing.T) {
	parent := writeConsentFixtureBundle(t, nil, "0.1.0")
	state, root := newConsentState(t)
	pb, err := bundle.Open(parent.Path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	parentDigest := pack.LockDigest(pb.Lock)
	seedInstalledAgentLock(t, root, parent, pb.Lock)
	if err := pb.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	child := writeForkedConsentBundle(t, parentDigest, "0.2.0")
	hops := ComputeLocallyVerifiedHops(child.InspectReport, state)
	if hops == nil || !hops[1] {
		t.Fatalf("matching digest must verify hop 1: %+v", hops)
	}
}

func TestAdversary_LocallyVerified_NonFileState_NoVerification(t *testing.T) {
	parent := writeConsentFixtureBundle(t, nil, "0.1.0")
	pb, err := bundle.Open(parent.Path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	parentDigest := pack.LockDigest(pb.Lock)
	if err := pb.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	child := writeForkedConsentBundle(t, parentDigest, "0.2.0")
	stub := &adversaryStubState{}
	if got := ComputeLocallyVerifiedHops(child.InspectReport, stub); got != nil {
		t.Fatalf("non-FileInstallState must not mark hops: %+v", got)
	}
}

func TestAdversary_LocallyVerified_EmptyParentLockDigest_Skipped(t *testing.T) {
	report := &bundle.InspectReport{
		Provenance: &pack.ProvenanceReport{
			Entries: []pack.ProvenanceEntrySummary{
				{Index: 0, Action: "created"},
				{Index: 1, Action: "forked", ParentLockDigest: "", PublisherFingerprint: "fp", AgentName: "a"},
			},
		},
	}
	state, root := newConsentState(t)
	_ = root
	if got := ComputeLocallyVerifiedHops(report, state); got != nil {
		t.Fatalf("empty parent digest hop must not verify: %+v", got)
	}
}

func TestAdversary_ThreeHop_MixedVerification_Rendering(t *testing.T) {
	parent := writeConsentFixtureBundle(t, nil, "0.1.0")
	state, root := newConsentState(t)
	pb, err := bundle.Open(parent.Path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	parentDigest := pack.LockDigest(pb.Lock)
	seedInstalledAgentLock(t, root, parent, pb.Lock)
	if err := pb.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	middle := writeForkedConsentBundle(t, parentDigest, "0.2.0")
	mb, err := bundle.Open(middle.Path)
	if err != nil {
		t.Fatalf("open middle: %v", err)
	}
	middleDigest := pack.LockDigest(mb.Lock)
	if err := mb.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	child := writeThreeHopConsentBundle(t, parentDigest, middleDigest, "0.3.0")
	hops := ComputeLocallyVerifiedHops(child.InspectReport, state)
	if hops == nil || !hops[1] || hops[2] {
		t.Fatalf("want hop1 verified hop2 claimed: %+v", hops)
	}
}

func TestAdversary_SingleHop_ComputeLocallyVerifiedHopsNil(t *testing.T) {
	fix := writeConsentFixtureBundle(t, nil, "0.1.0")
	state, _ := newConsentState(t)
	if got := ComputeLocallyVerifiedHops(fix.InspectReport, state); got != nil {
		t.Fatalf("single-hop must return nil map: %+v", got)
	}
}