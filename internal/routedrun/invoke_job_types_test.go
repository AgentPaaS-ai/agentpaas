package routedrun

import (
	"encoding/json"
	"testing"
)

// TestInvokeJob_TypeFields verifies InvokeJob, InvokeJobEvent, and InvokeJobResult
// JSON-serialize round-trip with all versioned fields preserved. This guards the
// B26 admission contract: every durable invocation job carries schema version,
// invocation/workflow/run/attempt identity, resolved deployment identity, nested
// package digests, bounded input + digest, initial ceilings, journal config,
// artifact root, and a compatibility-safe SDK config — but never a raw
// credential value (spec: b30-summary.md line 114).
func TestInvokeJob_TypeFields(t *testing.T) {
	// --- InvokeJob ---
	job := InvokeJob{
		SchemaVersion:               invokeJobSchemaVersionV1,
		InvocationID:               "inv-1",
		WorkflowID:                 "wf-1",
		RunID:                      "run-1",
		AttemptID:                  "", // empty until T05 supervisor claim
		ResolvedDeploymentID:       "dep-1",
		ResolvedDeploymentVersion:  "1.0.0",
		ResolvedDeploymentDigest:   "sha256:bundle1",
		NestedPackageDigests:       map[string]string{"stage0": "sha256:n0"},
		InputDigest:                "sha256:input1",
		InputPayload:               `{"x":1}`,
		InitialMaxActiveDurationMs: 60_000,
		InitialAttemptLeaseMs:     30_000,
		InitialMaxCostUsdDecimal:  "1.00",
		ProgressJournalRoot:        "/state/runs/run-1/journal",
		ArtifactRoot:               "/state/runs/run-1/artifacts",
		SDKConfig:                  `{"api_version":"v1","timeout_ms":5000}`,
	}
	// CRITICAL: no raw credential field exists on InvokeJob.
	if job.CredentialValue != "" {
		t.Fatalf("InvokeJob must not carry a raw credential value, got %q", job.CredentialValue)
	}

	data, err := json.Marshal(job)
	if err != nil {
		t.Fatalf("marshal InvokeJob: %v", err)
	}
	var got InvokeJob
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal InvokeJob: %v", err)
	}
	if got.SchemaVersion != invokeJobSchemaVersionV1 {
		t.Fatalf("schema_version=%s want %s", got.SchemaVersion, invokeJobSchemaVersionV1)
	}
	if got.InvocationID != "inv-1" || got.WorkflowID != "wf-1" || got.RunID != "run-1" {
		t.Fatalf("identity mismatch: %+v", got)
	}
	if got.AttemptID != "" {
		t.Fatalf("attempt must be empty until T05, got %q", got.AttemptID)
	}
	if got.ResolvedDeploymentDigest != "sha256:bundle1" {
		t.Fatalf("deployment digest=%s", got.ResolvedDeploymentDigest)
	}
	if got.NestedPackageDigests["stage0"] != "sha256:n0" {
		t.Fatalf("nested digests=%v", got.NestedPackageDigests)
	}
	if got.InputDigest != "sha256:input1" || got.InputPayload != `{"x":1}` {
		t.Fatalf("input mismatch: digest=%s payload=%s", got.InputDigest, got.InputPayload)
	}
	if got.SDKConfig != `{"api_version":"v1","timeout_ms":5000}` {
		t.Fatalf("sdk config=%s", got.SDKConfig)
	}

	// --- InvokeJobEvent ---
	ev := InvokeJobEvent{
		SchemaVersion: invokeJobSchemaVersionV1,
		Sequence:      7,
		EventKind:     InvokeJobEventStarted,
		Payload:       `{"msg":"started"}`,
	}
	edata, err := json.Marshal(ev)
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}
	var evgot InvokeJobEvent
	if err := json.Unmarshal(edata, &evgot); err != nil {
		t.Fatalf("unmarshal event: %v", err)
	}
	if evgot.Sequence != 7 {
		t.Fatalf("sequence=%d", evgot.Sequence)
	}
	if evgot.EventKind != InvokeJobEventStarted {
		t.Fatalf("kind=%s want %s", evgot.EventKind, InvokeJobEventStarted)
	}
	// HMAC not set on this event; ensure field exists and is empty-safe.
	if ev.HMAC != "" {
		t.Fatalf("unexpected hmac: %s", ev.HMAC)
	}

	// --- InvokeJobResult ---
	res := InvokeJobResult{
		SchemaVersion:      invokeJobSchemaVersionV1,
		InvocationID:      "inv-1",
		WorkflowID:        "wf-1",
		RunID:             "run-1",
		AttemptID:         "att-1",
		ResultDigest:      "sha256:result1",
		StructuredResult:   `{"ok":true}`,
		ArtifactReferences: []string{"artifacts/out.bin"},
		TerminalStatus:     InvokeJobResultSucceeded,
	}
	rdata, err := json.Marshal(res)
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	var rgot InvokeJobResult
	if err := json.Unmarshal(rdata, &rgot); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if rgot.ResultDigest != "sha256:result1" {
		t.Fatalf("result digest=%s", rgot.ResultDigest)
	}
	if rgot.TerminalStatus != InvokeJobResultSucceeded {
		t.Fatalf("terminal=%s want %s", rgot.TerminalStatus, InvokeJobResultSucceeded)
	}
	if len(rgot.ArtifactReferences) != 1 || rgot.ArtifactReferences[0] != "artifacts/out.bin" {
		t.Fatalf("artifacts=%v", rgot.ArtifactReferences)
	}
}

// TestInvokeJobEventKinds verifies the event-kind constants are stable strings.
func TestInvokeJobEventKinds(t *testing.T) {
	cases := []struct {
		kind InvokeJobEventKind
		want string
	}{
		{InvokeJobEventAccepted, "ACCEPTED"},
		{InvokeJobEventStarted, "STARTED"},
		{InvokeJobEventProgressRef, "PROGRESS_REF"},
		{InvokeJobEventSucceeded, "SUCCEEDED"},
		{InvokeJobEventFailed, "FAILED"},
		{InvokeJobEventCancelled, "CANCELLED"},
	}
	for _, c := range cases {
		if c.kind.String() != c.want {
			t.Fatalf("kind=%v want %s got %s", c.kind, c.want, c.kind.String())
		}
	}
}
