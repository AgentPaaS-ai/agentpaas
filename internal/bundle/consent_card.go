package bundle

import (
	"fmt"
	"strings"
)

// ConsentCardMode describes which install consent presentation to use.
type ConsentCardMode int

const (
	ConsentCardFull ConsentCardMode = iota
	ConsentCardAbbreviated
)

// ConsentCardOpts controls consent card rendering from a verified inspect report.
type ConsentCardOpts struct {
	Mode       ConsentCardMode
	AgentName  string
	AgentVersion string
	// PolicyDiffLines are locally computed structural diff lines (changed-policy updates).
	PolicyDiffLines []string
	// LocallyVerifiedHops maps hop index → true for hops verified against local state.
	LocallyVerifiedHops map[int]bool
}

// FormatConsentCard renders the install policy approval card from verified bundle
// contents only (post-trust-resolution). Deterministic for a given report and opts.
func FormatConsentCard(r *InspectReport, opts ConsentCardOpts) string {
	if r == nil || !r.Verified {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "INSTALL POLICY APPROVAL\n\n")
	if opts.Mode == ConsentCardAbbreviated {
		fmt.Fprintf(&b, "Policy unchanged since last approval for %s@%s.\n\n", opts.AgentName, opts.AgentVersion)
	} else {
		if opts.AgentName != "" {
			fmt.Fprintf(&b, "Agent: %s@%s\n\n", opts.AgentName, opts.AgentVersion)
		}
		if r.Publisher != nil {
			AppendPublisherSection(&b, r.Publisher)
			fmt.Fprintf(&b, "\n")
		}
		AppendTailAnchorSection(&b, r.Provenance)
		AppendChainDeltasSection(&b, r.Provenance, opts.LocallyVerifiedHops)
		AppendProvenanceSection(&b, r.ProvenanceText)
		AppendPolicySummarySection(&b, r.PolicySummary)
		AppendPolicyLintsSection(&b, r.PolicyLints)
		AppendRequirementsSection(&b, r.Requirements)
		if r.Requirements != nil {
			fmt.Fprintf(&b, "\nInstall mode: %s\n", r.Requirements.Image)
		}
		if r.SBOM != nil {
			fmt.Fprintf(&b, "\nSBOM package count: %d\n", r.SBOM.PackageCount)
		}
		if len(opts.PolicyDiffLines) > 0 {
			fmt.Fprintf(&b, "\nPolicy changes since last install (local diff)\n")
			for _, line := range opts.PolicyDiffLines {
				fmt.Fprintf(&b, "  %s\n", line)
			}
		}
		AppendD3Disclaimer(&b)
	}
	return b.String()
}