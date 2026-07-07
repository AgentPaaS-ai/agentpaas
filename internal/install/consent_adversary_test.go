package install

import (
	"errors"
	"testing"

	"github.com/AgentPaaS-ai/agentpaas/internal/pack"
)

func TestAdversary_PolicyDigestBindingFailsClosed(t *testing.T) {
	fix := writeConsentFixtureBundle(t, nil, "0.1.0")
	state, root := newConsentState(t)
	before := stateFileCount(t, root)

	otherPolicy := []byte(`version: "1.0"
agent:
  name: consent-test-agent
egress:
  - domain: "other.example.com"
    ports: [443]
`)
	otherDigest, err := pack.ComputePolicyDigest(otherPolicy)
	if err != nil {
		t.Fatalf("digest: %v", err)
	}

	cases := []struct {
		name   string
		digest string
	}{
		{"wrong digest", "0000000000000000000000000000000000000000000000000000000000000000"},
		{"stale digest from older inspect", otherDigest},
		{"empty digest", ""},
		{"digest of different policy while expecting lock digest", otherDigest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ResolvePolicyConsent(PolicyConsentOpts{
				Report: fix.InspectReport, PolicyDigest: fix.PolicyDigest, PolicyYAML: fix.PolicyYAML,
				PublisherFingerprint: fix.PublisherFP, PublisherName: fix.PublisherName,
				AgentName: fix.AgentName, AgentVersion: fix.AgentVersion, State: state, IsTTY: false,
				AcceptPolicyDigest: tc.digest,
			})
			if tc.digest == "" {
				if !errors.Is(err, ErrPolicyRefused) {
					t.Fatalf("want refused, got %v", err)
				}
			} else if tc.digest != fix.PolicyDigest {
				if !errors.Is(err, ErrPolicyMismatch) {
					t.Fatalf("want mismatch, got %v", err)
				}
			}
			assertNoStateGrowth(t, root, before)
		})
	}
}