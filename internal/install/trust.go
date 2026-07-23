// Package install implements trust resolution and TOFU (Trust On First Use)
// consent for the AgentPaaS verified install flow (Block 23).
//
// After bundle verification passes, the caller invokes ResolveTrust to
// resolve the publisher's fingerprint against the trust store, handling
// pinned, unknown (TOFU), and key-conflict paths.
package install

import (
	"errors"
	"fmt"
	"strings"

	"github.com/AgentPaaS-ai/agentpaas/internal/audit"
	"github.com/AgentPaaS-ai/agentpaas/internal/trust"
)

// ---------------------------------------------------------------------------
// Error sentinels — distinguishable by the CLI layer for exit codes
// ---------------------------------------------------------------------------

// ErrTrustRefused is returned when the user declines to trust the publisher
// fingerprint (TTY abort after failed prompts, or non-TTY with no flag).
var ErrTrustRefused = errors.New("trust refused: fingerprint verification not completed")

// ErrConfirmMismatch is returned when --confirm-fingerprint is provided but
// does not match the publisher's fingerprint. The CLI layer must use exit
// code 2 for this error.
var ErrConfirmMismatch = errors.New("--confirm-fingerprint does not match the publisher fingerprint")

// ErrKeyConflict is returned when a publisher name is already pinned with a
// different key in the trust store (SSH-style hostile-key warning).
var ErrKeyConflict = errors.New("publisher key conflict: different key already trusted for this publisher name")

// ---------------------------------------------------------------------------
// Custom error types carrying user-facing display messages
// ---------------------------------------------------------------------------

// KeyConflictError wraps ErrKeyConflict with an SSH-style user-facing message.
type KeyConflictError struct {
	PublisherName string // name from the bundle that triggered the conflict
	ExpectedFP    string // fingerprint already trusted for this name
	ReceivedFP    string // fingerprint from the incoming bundle
}

// KeyConflictError.Error returns the error message.
func (e *KeyConflictError) Error() string  { return ErrKeyConflict.Error() }
// KeyConflictError.Unwrap returns the underlying wrapped error.
func (e *KeyConflictError) Unwrap() error   { return ErrKeyConflict }

// DisplayMessage returns the SSH-style hard warning for the operator.
func (e *KeyConflictError) DisplayMessage() string {
	return fmt.Sprintf(
		"PUBLISHER KEY CHANGED — someone may be impersonating %q\n"+
			"\n"+
			"  Expected fingerprint: %s\n"+
			"  Received fingerprint:  %s\n"+
			"\n"+
			"Recovery: run 'agentpaas trust remove' for this publisher, then reinstall.\n"+
			"No inline override is available.",
		e.PublisherName,
		trust.DisplayFingerprint(e.ExpectedFP),
		trust.DisplayFingerprint(e.ReceivedFP),
	)
}

// TrustRefusedError wraps ErrTrustRefused with operator-facing context.
type TrustRefusedError struct {
	Reason      string // explanation for non-TTY missing-flag case
	Fingerprint string // display-form fingerprint (non-TTY missing-flag)
}

// TrustRefusedError.Error returns the error message.
func (e *TrustRefusedError) Error() string  { return ErrTrustRefused.Error() }
// TrustRefusedError.Unwrap returns the underlying wrapped error.
func (e *TrustRefusedError) Unwrap() error   { return ErrTrustRefused }

// DisplayMessage returns instructions for the operator.
func (e *TrustRefusedError) DisplayMessage() string {
	if e.Fingerprint != "" {
		return fmt.Sprintf(
			"Non-interactive mode requires explicit fingerprint confirmation.\n"+
				"Run 'agentpaas inspect' first to view the publisher fingerprint, then\n"+
				"re-run with --confirm-fingerprint <full-fingerprint>.\n\n"+
				"Publisher fingerprint: %s",
			e.Fingerprint,
		)
	}
	return e.Reason
}

// ConfirmMismatchError wraps ErrConfirmMismatch with the provided vs expected values.
type ConfirmMismatchError struct {
	Provided string // display-form fingerprint the user provided
	Expected string // display-form fingerprint from the bundle
}

// ConfirmMismatchError.Error returns the error message.
func (e *ConfirmMismatchError) Error() string  { return ErrConfirmMismatch.Error() }
// ConfirmMismatchError.Unwrap returns the underlying wrapped error.
func (e *ConfirmMismatchError) Unwrap() error   { return ErrConfirmMismatch }

// DisplayMessage returns the mismatch details for the operator.
func (e *ConfirmMismatchError) DisplayMessage() string {
	return fmt.Sprintf(
		"--confirm-fingerprint does not match the publisher fingerprint.\n\n"+
			"Provided:  %s\n"+
			"Expected:  %s",
		e.Provided, e.Expected,
	)
}
// ---------------------------------------------------------------------------

const (
	// eventPublisherConfirmMismatch is the audit event emitted when
	// --confirm-fingerprint does not match. Not yet in audit package constants.
	eventPublisherConfirmMismatch = "publisher_confirm_mismatch"
)

// ---------------------------------------------------------------------------
// Options and result types
// ---------------------------------------------------------------------------

// TrustResolveOpts carries all inputs needed for trust resolution.
type TrustResolveOpts struct {
	// PublisherName is the human-readable slug from the bundle lock's publisher block.
	PublisherName string

	// PublisherFingerprint is the hex-encoded SHA-256 of the publisher's public key.
	PublisherFingerprint string

	// PublisherPublicKeyPEM is the PEM-encoded ECDSA P-256 public key.
	PublisherPublicKeyPEM string

	// Store is a pre-loaded trust store. ResolveTrust only mutates the store
	// (via Pin + Save) on the TOFU approval path. On every non-approval path
	// the store is left unchanged.
	Store *trust.Store

	// IsTTY indicates whether the caller is attached to an interactive terminal.
	IsTTY bool

	// ConfirmedFingerprint is the value of the --confirm-fingerprint CLI flag
	// (non-TTY mode). Empty string means the flag was not provided.
	ConfirmedFingerprint string

	// Prompt is a callback for TTY interactive input. It receives a prompt
	// message and must return the user's typed line. Tests inject this to
	// simulate user input without reading os.Stdin directly.
	Prompt func(prompt string) (string, error)

	// EmitAudit is a best-effort audit event emitter. It receives an event
	// type string and a string→string payload. Never called with secret material.
	EmitAudit func(eventType string, payload map[string]string)
}

// TrustResult holds the outcome of trust resolution.
type TrustResult struct {
	// Publisher is the resolved publisher record (from the store or newly pinned).
	Publisher *trust.Publisher

	// WasPinned is true when a new publisher was added to the trust store (TOFU path).
	WasPinned bool

	// DisplayLines contains the exact user-facing strings to print to the
	// operator, one per line. These are computed by ResolveTrust so the CLI
	// layer does not need to derive display messages.
	DisplayLines []string
}

// ---------------------------------------------------------------------------
// ResolveTrust
// ---------------------------------------------------------------------------

// ResolveTrust resolves a verified publisher against the trust store and
// handles the TOFU consent flow.
//
// Resolution paths:
//
//  1. Pinned + same key: returns the existing publisher with no mutation.
//
//  2. Unknown (TOFU): displays the full fingerprint and requires explicit
//     operator confirmation. TTY mode prompts for the last 8 hex characters
//     (up to 3 attempts). Non-TTY mode requires --confirm-fingerprint with
//     the full normalized fingerprint. On approval, the publisher is pinned
//     with source=tofu and the store is saved.
//
//  3. Alias/name match, different fingerprint: returns ErrKeyConflict with
//     an SSH-style hard warning. No store mutation occurs.
//
//  4. Non-TTY without --confirm-fingerprint: returns an error instructing the
//     operator to run inspect first or pass the flag.
//
//  5. --confirm-fingerprint with a non-matching value: returns ErrConfirmMismatch
//     (exit code 2) with no store mutation.
func ResolveTrust(opts TrustResolveOpts) (*TrustResult, error) {
	// Normalize the incoming fingerprint for lookups and comparisons.
	fp := trust.NormalizeFingerprint(opts.PublisherFingerprint)
	displayFP := trust.DisplayFingerprint(fp)

	// ── Path 1: Pinned + same key ───────────────────────────────────────
	if existing, ok := opts.Store.Get(fp); ok {
		alias := existing.Alias
		if alias == "" {
			alias = "(no alias)"
		}
		var lines []string
		if existing.FirstSeen != "" {
			lines = []string{
				fmt.Sprintf("publisher: %s (pinned %s)", alias, existing.FirstSeen),
			}
		} else {
			lines = []string{
				fmt.Sprintf("publisher: %s (pinned)", alias),
			}
		}
		return &TrustResult{
			Publisher:    existing,
			WasPinned:    false,
			DisplayLines: lines,
		}, nil
	}

	// ── Path 3: Alias/name match, different fingerprint ─────────────────
	if conflict := opts.Store.CheckKeyConflict(opts.PublisherName, fp); conflict != nil {
		emitAudit(opts.EmitAudit, audit.EventTypePublisherKeyConflict, map[string]string{
			"fingerprint":       fp,
			"publisher_name":    opts.PublisherName,
			"conflicting_alias": conflict.Alias,
		})
		return nil, &KeyConflictError{
			PublisherName: opts.PublisherName,
			ExpectedFP:    conflict.Fingerprint,
			ReceivedFP:    fp,
		}
	}

	// ── Path 2: Unknown (TOFU) ──────────────────────────────────────────

	// TTY mode: prompt for last 8 hex chars.
	if opts.IsTTY {
		return resolveTOFUInteractive(fp, displayFP, opts)
	}

	// Non-TTY mode: require --confirm-fingerprint flag.
	return resolveTOFUNonInteractive(fp, displayFP, opts)
}

// ---------------------------------------------------------------------------
// TOFU sub-paths
// ---------------------------------------------------------------------------

// tofuPromptLimit is the maximum number of TTY prompts before giving up.
const tofuPromptLimit = 3

// resolveTOFUInteractive handles the TTY TOFU flow: display fingerprint,
// prompt for last 8 hex chars (up to 3 attempts), and pin on success.
func resolveTOFUInteractive(fp, displayFP string, opts TrustResolveOpts) (*TrustResult, error) {
	last8 := last8Hex(fp)

	// Build the instruction message (sent once before the first prompt).
	instruction := fmt.Sprintf(
		"Publisher fingerprint: %s\n\n"+
			"Verify this fingerprint with the sender over another channel before continuing.\n"+
			"Type the LAST 8 characters of the fingerprint to confirm: ",
		displayFP,
	)

	for attempt := 0; attempt < tofuPromptLimit; attempt++ {
		response, err := opts.Prompt(instruction)
		if err != nil {
			return nil, &TrustRefusedError{Reason: err.Error()}
		}
		response = strings.ToLower(strings.Join(strings.Fields(response), ""))

		if response == last8 {
			return pinPublisher(fp, displayFP, opts)
		}

		// Wrong answer. Update instruction for retry.
		instruction = fmt.Sprintf(
			"The typed value does not match. Verify the fingerprint carefully.\n"+
				"Type the LAST 8 characters of the fingerprint to confirm: ",
		)
	}

	return nil, ErrTrustRefused
}

// resolveTOFUNonInteractive handles the non-TTY TOFU flow:
// requires --confirm-fingerprint matching the full normalized fingerprint.
func resolveTOFUNonInteractive(fp, displayFP string, opts TrustResolveOpts) (*TrustResult, error) {
	if opts.ConfirmedFingerprint == "" {
		return nil, &TrustRefusedError{
			Fingerprint: displayFP,
		}
	}

	confirmed := trust.NormalizeFingerprint(opts.ConfirmedFingerprint)
	if confirmed != fp {
		emitAudit(opts.EmitAudit, eventPublisherConfirmMismatch, map[string]string{
			"fingerprint":     fp,
			"confirmed_value": opts.ConfirmedFingerprint,
		})
		return nil, &ConfirmMismatchError{
			Provided: trust.DisplayFingerprint(confirmed),
			Expected: displayFP,
		}
	}

	return pinPublisher(fp, displayFP, opts)
}

// pinPublisher adds the publisher to the trust store with source=tofu, saves,
// and emits the publisher_trusted audit event.
func pinPublisher(fp, displayFP string, opts TrustResolveOpts) (*TrustResult, error) {
	pub := trust.Publisher{
		Fingerprint:  fp,
		PublicKeyPEM: opts.PublisherPublicKeyPEM,
		Alias:        opts.PublisherName,
	}

	if err := opts.Store.Pin(pub, trust.SourceTOFU); err != nil {
		return nil, fmt.Errorf("pin publisher to trust store: %w", err)
	}

	if err := opts.Store.Save(); err != nil {
		return nil, fmt.Errorf("save trust store: %w", err)
	}

	emitAudit(opts.EmitAudit, audit.EventTypePublisherTrusted, map[string]string{
		"fingerprint":    fp,
		"publisher_name": opts.PublisherName,
		"source":         string(trust.SourceTOFU),
	})

	// Re-read the pinned publisher to get the populated FirstSeen/LastUsed.
	pinned, ok := opts.Store.Get(fp)
	if !ok {
		pinned = &pub
	}

	return &TrustResult{
		Publisher: pinned,
		WasPinned: true,
		DisplayLines: []string{
			fmt.Sprintf("publisher: %s (trusted now, fingerprint %s)", opts.PublisherName, displayFP),
		},
	}, nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// last8Hex returns the last 8 lowercase hex characters of a normalized fingerprint.
// The fingerprint is expected to be at least 8 characters (64 hex chars).
func last8Hex(fp string) string {
	if len(fp) < 8 {
		return fp
	}
	return fp[len(fp)-8:]
}

// emitAudit is a no-op-safe wrapper around the EmitAudit callback.
func emitAudit(emit func(string, map[string]string), eventType string, payload map[string]string) {
	if emit == nil {
		return
	}
	emit(eventType, payload)
}