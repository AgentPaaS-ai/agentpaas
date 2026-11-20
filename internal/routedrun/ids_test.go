package routedrun

import (
	"testing"
)

func TestIDTypes_String(t *testing.T) {
	tests := []struct {
		name string
		id   interface{ String() string }
		want string
	}{
		{"DeploymentID", DeploymentID("dep-abc123"), "dep-abc123"},
		{"InvocationID", InvocationID("inv-xyz789"), "inv-xyz789"},
		{"ControlRequestID", ControlRequestID("ctrl-001"), "ctrl-001"},
		{"LimitAmendmentID", LimitAmendmentID("amend-001"), "amend-001"},
		{"WorkflowID", WorkflowID("wf-001"), "wf-001"},
		{"NodeID", NodeID("node-001"), "node-001"},
		{"ServiceID", ServiceID("svc-001"), "svc-001"},
		{"HandoffID", HandoffID("ho-001"), "ho-001"},
		{"ChildBatchID", ChildBatchID("cb-001"), "cb-001"},
		{"ChildResultID", ChildResultID("cr-001"), "cr-001"},
		{"ArtifactID", ArtifactID("art-001"), "art-001"},
		{"RunID", RunID("run-001"), "run-001"},
		{"AttemptID", AttemptID("at-001"), "at-001"},
		{"LeaseID", LeaseID("ls-001"), "ls-001"},
		{"CheckpointID", CheckpointID("cp-001"), "cp-001"},
		{"ModelCallID", ModelCallID("mc-001"), "mc-001"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.id.String(); got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestIDTypes_MarshalText(t *testing.T) {
	id := RunID("run-test-marshal")
	got, err := id.MarshalText()
	if err != nil {
		t.Fatalf("MarshalText() error = %v", err)
	}
	if string(got) != "run-test-marshal" {
		t.Errorf("MarshalText() = %q, want %q", string(got), "run-test-marshal")
	}
}

func TestIDTypes_UnmarshalText(t *testing.T) {
	var id RunID
	if err := id.UnmarshalText([]byte("run-abc")); err != nil {
		t.Fatalf("UnmarshalText() error = %v", err)
	}
	if string(id) != "run-abc" {
		t.Errorf("After UnmarshalText, id = %q, want %q", string(id), "run-abc")
	}
}

func TestIDTypes_JSONRoundTrip(t *testing.T) {
	type wrapper struct {
		RunID RunID `json:"run_id"`
	}
	original := wrapper{RunID: "run-json-test"}
	data, err := MarshalCanonical(original)
	if err != nil {
		t.Fatalf("MarshalCanonical error = %v", err)
	}
	var decoded wrapper
	if err := UnmarshalStrict(data, &decoded); err != nil {
		t.Fatalf("UnmarshalStrict error = %v", err)
	}
	if decoded.RunID != original.RunID {
		t.Errorf("round-trip mismatch: %q != %q", decoded.RunID, original.RunID)
	}
}

func TestIDTypes_EmptyInvalid(t *testing.T) {
	// Empty ID is syntactically valid as a typed string but should be
	// treated as a logical zero value; callers must validate.
	var id RunID
	if id.String() != "" {
		t.Errorf("zero value RunID should be empty, got %q", id.String())
	}
}

func TestAttemptID_Validate(t *testing.T) {
	tests := []struct {
		id    AttemptID
		valid bool
	}{
		{"at-001", true},
		{"", false},
		{"at-", false},
		{"at-abcdef123456", true},
	}
	for _, tt := range tests {
		t.Run(string(tt.id), func(t *testing.T) {
			got := AttemptIDValidate(tt.id)
			if got != tt.valid {
				t.Errorf("AttemptIDValidate(%q) = %v, want %v", tt.id, got, tt.valid)
			}
		})
	}
}