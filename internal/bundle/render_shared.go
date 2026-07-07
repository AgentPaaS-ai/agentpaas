package bundle

import (
	"fmt"
	"strings"
)

// AppendPublisherSection writes the publisher block to b (shared by inspect and consent).
func AppendPublisherSection(b *strings.Builder, pub *InspectPublisher) {
	if pub == nil {
		return
	}
	fmt.Fprintf(b, "Publisher\n")
	fmt.Fprintf(b, "  Name:        %s\n", pub.Name)
	fmt.Fprintf(b, "  Fingerprint: %s\n", pub.FingerprintDisplay)
	fmt.Fprintf(b, "  %s\n", strings.ReplaceAll(pub.TrustDisclaimer, "\n", "\n  "))
}

// AppendProvenanceSection writes provenance text to b.
func AppendProvenanceSection(b *strings.Builder, provenanceText string) {
	if provenanceText == "" {
		return
	}
	fmt.Fprintf(b, "\n%s\n", provenanceText)
}

// AppendPolicySummarySection writes policy summary lines to b.
func AppendPolicySummarySection(b *strings.Builder, lines []PolicySummaryLine) {
	if len(lines) == 0 {
		return
	}
	fmt.Fprintf(b, "\nPolicy summary\n")
	for _, line := range lines {
		fmt.Fprintf(b, "  [%s] %s\n", line.Section, line.Detail)
	}
}

// AppendPolicyLintsSection writes policy lint warnings to b.
func AppendPolicyLintsSection(b *strings.Builder, lints []PolicyLint) {
	if len(lints) == 0 {
		return
	}
	fmt.Fprintf(b, "\nPolicy lints (warnings)\n")
	for _, lint := range lints {
		fmt.Fprintf(b, "  - [%s] %s\n", lint.Code, lint.Message)
	}
}

// AppendRequirementsSection writes install requirements to b.
func AppendRequirementsSection(b *strings.Builder, req *InspectRequirements) {
	if req == nil {
		return
	}
	fmt.Fprintf(b, "\nRequirements\n")
	fmt.Fprintf(b, "  Platform:    %s\n", req.Platform)
	fmt.Fprintf(b, "  LLM:         %s\n", req.LLMProvider)
	fmt.Fprintf(b, "  Image:       %s\n", req.Image)
	if len(req.Credentials) > 0 {
		fmt.Fprintf(b, "  Credentials:\n")
		for _, c := range req.Credentials {
			line := fmt.Sprintf("    - %s (%s)", c.ID, c.Type)
			if c.Header != "" {
				line += fmt.Sprintf(" header=%s", c.Header)
			}
			if c.Destination != "" {
				line += fmt.Sprintf(" destination=%s", c.Destination)
			}
			fmt.Fprintf(b, "%s\n", line)
		}
	} else {
		fmt.Fprintf(b, "  Credentials: (none declared)\n")
	}
}

// AppendSBOMSection writes SBOM summary to b.
func AppendSBOMSection(b *strings.Builder, sbom *SBOMSummary) {
	if sbom == nil {
		return
	}
	fmt.Fprintf(b, "\nSBOM\n")
	fmt.Fprintf(b, "  Packages: %d\n", sbom.PackageCount)
	if sbom.ParseWarning != "" {
		fmt.Fprintf(b, "  Warning: %s\n", sbom.ParseWarning)
	}
	if len(sbom.TopLevelDeps) > 0 {
		fmt.Fprintf(b, "  Top-level deps: %s\n", strings.Join(sbom.TopLevelDeps, ", "))
	}
}

// AppendD3Disclaimer writes the fixed D3 disclaimer line block.
func AppendD3Disclaimer(b *strings.Builder) {
	fmt.Fprintf(b, "\n%s\n", strings.ReplaceAll(D3TrustDisclaimer, "\n", "\n"))
}