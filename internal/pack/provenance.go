package pack

import (
	"crypto/ecdsa"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/identity"
)

const chainSemantics = "each entry is a signed claim by its signer about its parent; intermediate artifacts are not independently verifiable from this lock alone"

// ProvenanceReport is the result of verifying a provenance chain.
type ProvenanceReport struct {
	// Verified is true when all structural and cryptographic rules pass.
	Verified bool `json:"verified"`
	// Warnings holds non-fatal issues (e.g. clock skew).
	Warnings []string `json:"warnings,omitempty"`
	// Entries is the list of provenance entry summaries.
	Entries []ProvenanceEntrySummary `json:"entries"`
	// ChainSemantics is a fixed string explaining what provenance entries mean.
	ChainSemantics string `json:"chain_semantics"`
}

// ProvenanceEntrySummary is a display-oriented summary of a provenance entry.
type ProvenanceEntrySummary struct {
	Index               int          `json:"index"`
	Action              string       `json:"action"`
	PublisherName       string       `json:"publisher_name"`
	PublisherFingerprint string      `json:"publisher_fingerprint"`
	AgentName           string       `json:"agent_name"`
	AgentVersion        string       `json:"agent_version"`
	Timestamp           time.Time    `json:"timestamp"`
	PolicyDelta         *PolicyDelta `json:"policy_delta,omitempty"`
	// ParentLockDigest is the parent lock digest for forked entries (local verification).
	ParentLockDigest string `json:"parent_lock_digest,omitempty"`
}

// VerifyProvenance performs full structural and cryptographic verification
// of a lock's provenance chain. It returns a report; the error return is
// reserved for programming errors (nil lock, etc.). Structural and signature
// failures set Verified=false.
func VerifyProvenance(lock *AgentLock) (*ProvenanceReport, error) {
	if lock == nil {
		return nil, fmt.Errorf("lock must not be nil")
	}

	report := &ProvenanceReport{
		ChainSemantics: chainSemantics,
		Verified:       true,
	}

	noPublisher := lock.Publisher == nil

	// Local-only lock: no publisher, empty provenance → valid.
	if noPublisher && len(lock.Provenance) == 0 {
		return report, nil
	}

	// Published lock: publisher present, provenance must be non-empty.
	if !noPublisher && len(lock.Provenance) == 0 {
		report.Verified = false
		report.Warnings = append(report.Warnings,
			"published lock (has publisher block) must have non-empty provenance")
		return report, nil
	}

	// Build entry summaries.
	entries := lock.Provenance
	for i := range entries {
		e := &entries[i]
		report.Entries = append(report.Entries, ProvenanceEntrySummary{
			Index:                i,
			Action:               e.Action,
			PublisherName:        e.PublisherName,
			PublisherFingerprint: e.PublisherFingerprint,
			AgentName:            e.AgentName,
			AgentVersion:         e.AgentVersion,
			Timestamp:            e.Timestamp,
			PolicyDelta:          e.PolicyDelta,
			ParentLockDigest:     e.ParentLockDigest,
		})
	}

	// Rule 1: entry[0].Action == "created" with empty parent digests.
	if entries[0].Action != "created" {
		report.Verified = false
		report.Warnings = append(report.Warnings,
			"entry[0]: action must be \"created\", got "+entries[0].Action)
	}
	if entries[0].ParentLockDigest != "" || entries[0].ParentBundleDigest != "" || entries[0].ParentPolicyDigest != "" {
		report.Verified = false
		report.Warnings = append(report.Warnings,
			"entry[0]: created entry must have empty parent digests")
	}

	// Rule 2: entries[1..n].Action == "forked" with non-empty parent_lock_digest.
	for i := 1; i < len(entries); i++ {
		e := &entries[i]
		if e.Action != "forked" {
			report.Verified = false
			report.Warnings = append(report.Warnings,
				fmt.Sprintf("entry[%d]: action must be \"forked\", got %q", i, e.Action))
		}
		if e.ParentLockDigest == "" {
			report.Verified = false
			report.Warnings = append(report.Warnings,
				fmt.Sprintf("entry[%d]: forked entry must have non-empty parent_lock_digest", i))
		}
	}

	// Rule 3: Timestamps non-decreasing (warn only — clocks lie).
	for i := 1; i < len(entries); i++ {
		if entries[i].Timestamp.Before(entries[i-1].Timestamp) {
			report.Warnings = append(report.Warnings,
				fmt.Sprintf("entry[%d]: timestamp %s is before entry[%d] timestamp %s (clock skew?)",
					i, entries[i].Timestamp.Format(time.RFC3339),
					i-1, entries[i-1].Timestamp.Format(time.RFC3339)))
		}
	}

	// Rule 4: Last entry's publisher fingerprint MUST equal lock.Publisher.Fingerprint.
	lastIdx := len(entries) - 1
	if !noPublisher {
		if entries[lastIdx].PublisherFingerprint != lock.Publisher.Fingerprint {
			report.Verified = false
			report.Warnings = append(report.Warnings,
				fmt.Sprintf("entry[%d]: last signer fingerprint %q does not match lock publisher fingerprint %q",
					lastIdx, entries[lastIdx].PublisherFingerprint, lock.Publisher.Fingerprint))
		}
	}

	// Rule 5: Verify every entry's signature and fingerprint/PEM consistency.
	for i := range entries {
		e := &entries[i]
		if err := verifyEntrySignatureAndFingerprint(e); err != nil {
			report.Verified = false
			report.Warnings = append(report.Warnings,
				fmt.Sprintf("entry[%d]: %v", i, err))
		}
	}

	return report, nil
}

// verifyEntrySignatureAndFingerprint verifies one provenance entry's signature
// and checks that its embedded PEM fingerprint matches its publisher_fingerprint.
func verifyEntrySignatureAndFingerprint(e *ProvenanceEntry) error {
	// Check fingerprint matches PEM.
	pub, err := PublicKeyFromPEM([]byte(e.PublisherPublicKeyPEM))
	if err != nil {
		return fmt.Errorf("parse publisher public key: %w", err)
	}
	computed := PublicKeyFingerprint(pub)
	if computed != e.PublisherFingerprint {
		return fmt.Errorf("publisher_fingerprint %q does not match key fingerprint %q",
			e.PublisherFingerprint, computed)
	}

	// Verify entry signature.
	if e.EntrySignature == "" {
		return fmt.Errorf("entry_signature is empty")
	}
	signature, err := base64.StdEncoding.DecodeString(e.EntrySignature)
	if err != nil {
		return fmt.Errorf("decode entry signature: %w", err)
	}
	canonical, err := provenanceEntryCanonical(e)
	if err != nil {
		return fmt.Errorf("canonical: %w", err)
	}
	digest := sha256.Sum256(canonical)
	if !ecdsa.VerifyASN1(pub, digest[:], signature) {
		return fmt.Errorf("signature verification failed")
	}
	return nil
}

// FormatProvenance renders a ProvenanceReport as a terminal-display string.
func FormatProvenance(report *ProvenanceReport) string {
	if report == nil || len(report.Entries) == 0 {
		return "Provenance: (none)"
	}

	var b strings.Builder
	b.WriteString("Provenance:\n")
	for i, s := range report.Entries {
		fpDisplay := identity.FormatFingerprintDisplay(s.PublisherFingerprint)
		if len(fpDisplay) > 0 {
			// Take first 8 chars of the bare fingerprint for the parenthesized shortcut.
			bare := s.PublisherFingerprint
			short := bare
			if len(bare) > 8 {
				short = bare[:8]
			}
			_ = fpDisplay // full display saved for longer form; here we show first 8 chars
			fpDisplay = short
		}

		switch s.Action {
		case "created":
			fmt.Fprintf(&b, "  %d. created  %s %s  by %s  (%s)  %s\n",
				i+1, s.AgentName, s.AgentVersion, s.PublisherName,
				fpDisplay[:min(8, len(fpDisplay))],
				s.Timestamp.Format("2006-01-02"))
		case "forked":
			fmt.Fprintf(&b, "  %d. forked   %s %s  by %s  (%s)  %s\n",
				i+1, s.AgentName, s.AgentVersion, s.PublisherName,
				fpDisplay[:min(8, len(fpDisplay))],
				s.Timestamp.Format("2006-01-02"))
		default:
			fmt.Fprintf(&b, "  %d. %s  %s %s  by %s  (%s)  %s\n",
				i+1, s.Action, s.AgentName, s.AgentVersion, s.PublisherName,
				fpDisplay[:min(8, len(fpDisplay))],
				s.Timestamp.Format("2006-01-02"))
		}

		// Policy delta (signer-claimed).
		if s.PolicyDelta != nil {
			delta := s.PolicyDelta
			if len(delta.EgressAdded) > 0 {
				fmt.Fprintf(&b, "     policy delta (signer-claimed): +egress %s\n",
					strings.Join(delta.EgressAdded, ", "))
			}
			if len(delta.EgressRemoved) > 0 {
				fmt.Fprintf(&b, "     policy delta (signer-claimed): -egress %s\n",
					strings.Join(delta.EgressRemoved, ", "))
			}
			if len(delta.CredentialsAdded) > 0 {
				fmt.Fprintf(&b, "     policy delta (signer-claimed): +credentials %s\n",
					strings.Join(delta.CredentialsAdded, ", "))
			}
			if len(delta.CredentialsRemoved) > 0 {
				fmt.Fprintf(&b, "     policy delta (signer-claimed): -credentials %s\n",
					strings.Join(delta.CredentialsRemoved, ", "))
			}
			if len(delta.MCPToolsAdded) > 0 {
				fmt.Fprintf(&b, "     policy delta (signer-claimed): +mcp_tools %s\n",
					strings.Join(delta.MCPToolsAdded, ", "))
			}
			if len(delta.MCPToolsRemoved) > 0 {
				fmt.Fprintf(&b, "     policy delta (signer-claimed): -mcp_tools %s\n",
					strings.Join(delta.MCPToolsRemoved, ", "))
			}
			if len(delta.ModelRoutesAdded) > 0 {
				fmt.Fprintf(&b, "     policy delta (signer-claimed): +model_routes %s\n",
					strings.Join(delta.ModelRoutesAdded, ", "))
			}
			if len(delta.ModelRoutesRemoved) > 0 {
				fmt.Fprintf(&b, "     policy delta (signer-claimed): -model_routes %s\n",
					strings.Join(delta.ModelRoutesRemoved, ", "))
			}
			if delta.RoutedRunChanged {
				fmt.Fprintf(&b, "     policy delta (signer-claimed): routed_run changed\n")
			}
		}
	}
	return b.String()
}