package runtime

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestModelCallEnvelopeValidate_RejectsEmptyMessages(t *testing.T) {
	e := ModelCallEnvelope{
		Messages: nil,
	}
	if err := e.Validate(); err == nil {
		t.Fatal("Validate must reject empty messages")
	}
}

func TestModelCallEnvelopeValidate_RejectsInvalidRole(t *testing.T) {
	e := ModelCallEnvelope{
		Messages: []Message{
			{Role: "wizard", Content: "hi"},
		},
	}
	if err := e.Validate(); err == nil {
		t.Fatal("Validate must reject an invalid role")
	}
}

func TestModelCallEnvelopeValidate_AcceptsAllValidRoles(t *testing.T) {
	roles := []string{RoleSystem, RoleDeveloper, RoleUser, RoleAssistant, RoleTool}
	for _, r := range roles {
		e := ModelCallEnvelope{
			Messages: []Message{{Role: r, Content: "x"}},
		}
		if err := e.Validate(); err != nil {
			t.Fatalf("role %q must be accepted: %v", r, err)
		}
	}
}

func TestModelCallEnvelopeValidate_RejectsRawCoTInReasoningSummary(t *testing.T) {
	// Security spec: reasoning summary is provider-approved, never raw CoT.
	// Reject if ReasoningSummary contains obvious CoT markers.
	cases := []string{
		"<thought>let me think about this</thought>",
		"<thinking>step 1: ...</thinking>",
		"Step 1: I need to reason through this.\nStep 2: Therefore...",
		"chain of thought: first I consider...",
	}
	for _, cot := range cases {
		e := ModelCallEnvelope{
			Messages:         []Message{{Role: RoleUser, Content: "q"}},
			ReasoningSummary: &cot,
		}
		if err := e.Validate(); err == nil {
			t.Fatalf("Validate must reject raw CoT in ReasoningSummary: %q", cot)
		}
	}
}

func TestModelCallEnvelopeValidate_AcceptsCleanReasoningSummary(t *testing.T) {
	summary := "The model evaluated three candidate themes and selected the highest-scoring one."
	e := ModelCallEnvelope{
		Messages:         []Message{{Role: RoleUser, Content: "q"}},
		ReasoningSummary: &summary,
	}
	if err := e.Validate(); err != nil {
		t.Fatalf("clean provider-approved summary must be accepted: %v", err)
	}
}

func TestModelCallEnvelopeValidate_AcceptsReasoningControls(t *testing.T) {
	e := ModelCallEnvelope{
		Messages:           []Message{{Role: RoleUser, Content: "q"}},
		ReasoningEffort:    ReasoningEffortMedium,
		MaxReasoningTokens: 1024,
	}
	if err := e.Validate(); err != nil {
		t.Fatalf("medium reasoning effort must be accepted: %v", err)
	}
}

func TestModelCallEnvelopeValidate_RejectsInvalidReasoningEffort(t *testing.T) {
	e := ModelCallEnvelope{
		Messages:        []Message{{Role: RoleUser, Content: "q"}},
		ReasoningEffort: ReasoningEffort("bogus"),
	}
	if err := e.Validate(); err == nil {
		t.Fatal("Validate must reject an invalid reasoning effort")
	}
}

func TestModelCallEnvelopeValidate_AcceptsStructuredOutputSchema(t *testing.T) {
	schema := map[string]any{"type": "object", "properties": map[string]any{"x": map[string]any{"type": "string"}}}
	e := ModelCallEnvelope{
		Messages:           []Message{{Role: RoleUser, Content: "q"}},
		StructuredOutput:   &StructuredOutputSpec{JSONSchema: schema},
	}
	if err := e.Validate(); err != nil {
		t.Fatalf("structured output schema must be accepted: %v", err)
	}
}

func TestModelCallEnvelopeValidate_AcceptsToolCallParts(t *testing.T) {
	e := ModelCallEnvelope{
		Messages: []Message{{Role: RoleUser, Content: "q"}},
		ToolCalls: []ToolCallPart{
			{ID: "call_1", Name: "search", Arguments: `{"q":"x"}`},
		},
	}
	if err := e.Validate(); err != nil {
		t.Fatalf("tool-call parts must be accepted: %v", err)
	}
}

func TestModelCallEnvelopeValidate_AcceptsProviderExtensions(t *testing.T) {
	e := ModelCallEnvelope{
		Messages:           []Message{{Role: RoleUser, Content: "q"}},
		ProviderExtensions: map[string]any{"x-vendor-foo": "bar"},
	}
	if err := e.Validate(); err != nil {
		t.Fatalf("namespaced provider extensions must be accepted: %v", err)
	}
}

func TestModelCallEnvelopeValidate_RejectsUnnamespacedProviderExtension(t *testing.T) {
	// Spec: provider extensions are namespaced, policy-reviewed. Reject keys
	// that are not namespaced (no hyphen-prefix like x-vendor-).
	e := ModelCallEnvelope{
		Messages:           []Message{{Role: RoleUser, Content: "q"}},
		ProviderExtensions: map[string]any{"barekey": "bar"},
	}
	if err := e.Validate(); err == nil {
		t.Fatal("Validate must reject a non-namespaced provider extension key")
	}
}

func TestModelCallEnvelopeValidate_AcceptsDeadline(t *testing.T) {
	dl := time.Now().Add(30 * time.Second)
	e := ModelCallEnvelope{
		Messages:  []Message{{Role: RoleUser, Content: "q"}},
		Deadline:  &dl,
		CancelCtx: context.Background(),
	}
	if err := e.Validate(); err != nil {
		t.Fatalf("deadline + cancel ctx must be accepted: %v", err)
	}
}

func TestModelCallEnvelopeValidate_RejectsExpiredDeadline(t *testing.T) {
	dl := time.Now().Add(-1 * time.Second)
	e := ModelCallEnvelope{
		Messages: []Message{{Role: RoleUser, Content: "q"}},
		Deadline: &dl,
	}
	if err := e.Validate(); err == nil {
		t.Fatal("Validate must reject an already-expired deadline")
	}
}

func TestModelCallEnvelopeValidate_AcceptsUsage(t *testing.T) {
	u := Usage{PromptTokens: 10, CompletionTokens: 5, ReasoningTokens: 2, TotalTokens: 17}
	e := ModelCallEnvelope{
		Messages: []Message{{Role: RoleUser, Content: "q"}},
		Usage:    &u,
	}
	if err := e.Validate(); err != nil {
		t.Fatalf("usage must be accepted: %v", err)
	}
}

func TestModelCallEnvelopeValidate_ErrorMentionsMessages(t *testing.T) {
	e := ModelCallEnvelope{}
	err := e.Validate()
	if err == nil {
		t.Fatal("expected error for empty messages")
	}
	if !strings.Contains(err.Error(), "messages") {
		t.Fatalf("error must mention messages; got %v", err)
	}
}
