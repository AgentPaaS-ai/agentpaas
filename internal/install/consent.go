package install

import (
	"errors"
	"fmt"
	"strings"

	"github.com/AgentPaaS-ai/agentpaas/internal/audit"
	"github.com/AgentPaaS-ai/agentpaas/internal/bundle"
	"github.com/AgentPaaS-ai/agentpaas/internal/trust"
)

// ErrPolicyRefused is returned when the operator declines policy approval.
var ErrPolicyRefused = errors.New("policy approval refused")

// ErrPolicyMismatch is returned when --accept-policy does not match lock.policy_digest (exit 2).
var ErrPolicyMismatch = errors.New("--accept-policy does not match the bundle policy digest")

// ErrDowngradeRefused is returned when agent version decreases without --allow-downgrade (A6).
var ErrDowngradeRefused = errors.New("install refused: version downgrade requires --allow-downgrade")

// PolicyRefusedError wraps ErrPolicyRefused with operator-facing context.
type PolicyRefusedError struct {
	Reason string
	Digest string
}

func (e *PolicyRefusedError) Error() string  { return ErrPolicyRefused.Error() }
func (e *PolicyRefusedError) Unwrap() error { return ErrPolicyRefused }

// DisplayMessage returns instructions for the operator.
func (e *PolicyRefusedError) DisplayMessage() string {
	if e.Digest != "" {
		return fmt.Sprintf(
			"Non-interactive mode requires explicit policy approval.\n"+
				"Run 'agentpaas inspect' to view the policy digest, then re-run with\n"+
				"--accept-policy <policy-digest>.\n\n"+
				"Policy digest: %s",
			e.Digest,
		)
	}
	return e.Reason
}

// PolicyMismatchError wraps ErrPolicyMismatch with provided vs expected digests.
type PolicyMismatchError struct {
	Provided string
	Expected string
}

func (e *PolicyMismatchError) Error() string  { return ErrPolicyMismatch.Error() }
func (e *PolicyMismatchError) Unwrap() error   { return ErrPolicyMismatch }

// DisplayMessage returns mismatch details for the operator.
func (e *PolicyMismatchError) DisplayMessage() string {
	return fmt.Sprintf(
		"--accept-policy does not match the bundle policy digest.\n\n"+
			"Provided:  %s\n"+
			"Expected:  %s",
		e.Provided, e.Expected,
	)
}

// DowngradeRefusedError wraps ErrDowngradeRefused with version context.
type DowngradeRefusedError struct {
	PriorVersion string
	NewVersion   string
}

func (e *DowngradeRefusedError) Error() string  { return ErrDowngradeRefused.Error() }
func (e *DowngradeRefusedError) Unwrap() error   { return ErrDowngradeRefused }

func (e *DowngradeRefusedError) DisplayMessage() string {
	return fmt.Sprintf(
		"Refusing install: version %s is older than installed %s.\n"+
			"Pass --allow-downgrade to proceed (audited).",
		e.NewVersion, e.PriorVersion,
	)
}

const policyApprovePrompt = "Approve this policy? [type 'approve']"
const policyPromptLimit = 3

// PolicyConsentOpts carries explicit inputs for policy consent (no os.Args reads).
type PolicyConsentOpts struct {
	Report *bundle.InspectReport

	PolicyDigest string
	PolicyYAML   []byte

	PublisherFingerprint string
	PublisherName        string
	AgentName            string
	AgentVersion         string

	State InstallStateStore

	IsTTY bool
	// AcceptPolicyDigest is the caller-provided --accept-policy value (non-TTY).
	AcceptPolicyDigest string
	AllowDowngrade     bool

	Prompt func(prompt string) (string, error)
	EmitAudit func(eventType string, payload map[string]string)
}

// PolicyConsentResult holds a successful policy approval outcome.
type PolicyConsentResult struct {
	Manifest     InstallManifest
	CardText     string
	DisplayLines []string
}

// ResolvePolicyConsent renders the consent card and enforces policy digest binding.
//
// If the operator approved the publisher fingerprint (T01) but declines policy here,
// the trust pin intentionally remains — do not remove trust store entries on decline.
func ResolvePolicyConsent(opts PolicyConsentOpts) (*PolicyConsentResult, error) {
	if opts.Report == nil || !opts.Report.Verified {
		return nil, fmt.Errorf("policy consent requires a verified inspect report")
	}
	if strings.TrimSpace(opts.PolicyDigest) == "" {
		return nil, fmt.Errorf("policy consent requires lock policy_digest")
	}
	if opts.State == nil {
		return nil, fmt.Errorf("policy consent requires install state store")
	}

	prior, err := opts.State.GetPriorInstall(opts.PublisherFingerprint, opts.AgentName)
	if err != nil {
		return nil, err
	}

	downgrade := prior != nil && isVersionDecrease(prior.Manifest.AgentVersion, opts.AgentVersion)
	if downgrade && !opts.AllowDowngrade {
		return nil, &DowngradeRefusedError{
			PriorVersion: prior.Manifest.AgentVersion,
			NewVersion:   opts.AgentVersion,
		}
	}

	cardMode := bundle.ConsentCardFull
	var diffLines []string
	if prior != nil && prior.Manifest.AcceptedPolicyDigest == opts.PolicyDigest {
		cardMode = bundle.ConsentCardAbbreviated
	} else if prior != nil && len(prior.PolicyYAML) > 0 && len(opts.PolicyYAML) > 0 {
		diff, derr := ComputeStructuralPolicyDiff(prior.PolicyYAML, opts.PolicyYAML)
		if derr != nil {
			return nil, derr
		}
		diffLines = FormatPolicyStructuralDiff(diff)
	}

	card := bundle.FormatConsentCard(opts.Report, bundle.ConsentCardOpts{
		Mode:                cardMode,
		AgentName:           opts.AgentName,
		AgentVersion:        opts.AgentVersion,
		PolicyDiffLines:     diffLines,
		LocallyVerifiedHops: ComputeLocallyVerifiedHops(opts.Report, opts.State),
	})

	if opts.IsTTY {
		if err := approvePolicyTTY(card, opts); err != nil {
			return nil, err
		}
	} else {
		if err := approvePolicyNonTTY(opts); err != nil {
			return nil, err
		}
	}

	manifest := InstallManifest{
		PublisherFingerprint: trust.NormalizeFingerprint(opts.PublisherFingerprint),
		PublisherName:        opts.PublisherName,
		AgentName:            opts.AgentName,
		AgentVersion:         opts.AgentVersion,
		AcceptedPolicyDigest: opts.PolicyDigest,
	}
	if err := opts.State.SaveApprovedInstall(manifest, opts.PolicyYAML); err != nil {
		return nil, err
	}

	emitAudit(opts.EmitAudit, audit.EventTypeInstallPolicyApproved, map[string]string{
		"agent":              opts.AgentName,
		"publisher_fingerprint": manifest.PublisherFingerprint,
		"policy_digest":      opts.PolicyDigest,
	})
	if downgrade && opts.AllowDowngrade {
		emitAudit(opts.EmitAudit, audit.EventTypeInstallDowngradeAllowed, map[string]string{
			"agent":           opts.AgentName,
			"prior_version":   prior.Manifest.AgentVersion,
			"new_version":     opts.AgentVersion,
			"policy_digest":   opts.PolicyDigest,
		})
	}

	return &PolicyConsentResult{
		Manifest: manifest,
		CardText: card,
		DisplayLines: []string{card},
	}, nil
}

func approvePolicyNonTTY(opts PolicyConsentOpts) error {
	if opts.AcceptPolicyDigest == "" {
		return &PolicyRefusedError{Digest: opts.PolicyDigest}
	}
	provided := strings.TrimSpace(opts.AcceptPolicyDigest)
	expected := strings.TrimSpace(opts.PolicyDigest)
	if provided != expected {
		return &PolicyMismatchError{Provided: provided, Expected: expected}
	}
	return nil
}

func approvePolicyTTY(card string, opts PolicyConsentOpts) error {
	if opts.Prompt == nil {
		return &PolicyRefusedError{Reason: "TTY policy approval requires a Prompt callback"}
	}
	prompt := card + "\n\n" + policyApprovePrompt + " "
	for attempt := 0; attempt < policyPromptLimit; attempt++ {
		response, err := opts.Prompt(prompt)
		if err != nil {
			return &PolicyRefusedError{Reason: err.Error()}
		}
		if strings.EqualFold(strings.TrimSpace(response), "approve") {
			return nil
		}
		prompt = "Type 'approve' to accept this policy: "
	}
	return ErrPolicyRefused
}