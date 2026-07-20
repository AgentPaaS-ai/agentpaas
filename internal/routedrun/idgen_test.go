package routedrun

import (
	"strings"
	"testing"
)

func TestGenerateID_PrefixesAndEntropy(t *testing.T) {
	checks := []struct {
		name   string
		gen    func() (string, error)
		prefix string
	}{
		{"deployment", func() (string, error) { id, err := NewDeploymentID(); return string(id), err }, PrefixDeployment},
		{"invocation", func() (string, error) { id, err := NewInvocationID(); return string(id), err }, PrefixInvocation},
		{"workflow", func() (string, error) { id, err := NewWorkflowID(); return string(id), err }, PrefixWorkflow},
		{"run", func() (string, error) { id, err := NewRunID(); return string(id), err }, PrefixRun},
		{"attempt", func() (string, error) { id, err := NewAttemptID(); return string(id), err }, PrefixAttempt},
		{"lease", func() (string, error) { id, err := NewLeaseID(); return string(id), err }, PrefixLease},
		{"node", func() (string, error) { id, err := NewNodeID(); return string(id), err }, PrefixNode},
		{"handoff", func() (string, error) { id, err := NewHandoffID(); return string(id), err }, PrefixHandoff},
		{"control", func() (string, error) { id, err := NewControlRequestID(); return string(id), err }, PrefixControl},
		{"amendment", func() (string, error) { id, err := NewLimitAmendmentID(); return string(id), err }, PrefixAmendment},
		{"service", func() (string, error) { id, err := NewServiceID(); return string(id), err }, PrefixService},
		{"child_batch", func() (string, error) { id, err := NewChildBatchID(); return string(id), err }, PrefixChildBatch},
		{"child_result", func() (string, error) { id, err := NewChildResultID(); return string(id), err }, PrefixChildResult},
		{"artifact", func() (string, error) { id, err := NewArtifactID(); return string(id), err }, PrefixArtifact},
		{"checkpoint", func() (string, error) { id, err := NewCheckpointID(); return string(id), err }, PrefixCheckpoint},
		{"model_call", func() (string, error) { id, err := NewModelCallID(); return string(id), err }, PrefixModelCall},
	}
	seen := map[string]bool{}
	for _, c := range checks {
		t.Run(c.name, func(t *testing.T) {
			id, err := c.gen()
			if err != nil {
				t.Fatalf("gen: %v", err)
			}
			if !strings.HasPrefix(id, c.prefix) {
				t.Fatalf("id %q missing prefix %q", id, c.prefix)
			}
			rest := strings.TrimPrefix(id, c.prefix)
			if rest == "" {
				t.Fatal("empty entropy")
			}
			if strings.Contains(rest, "-") {
				t.Fatalf("random part must not contain hyphens: %q", rest)
			}
			if seen[id] {
				t.Fatalf("duplicate id %q", id)
			}
			seen[id] = true
			// Second call must differ.
			id2, err := c.gen()
			if err != nil {
				t.Fatal(err)
			}
			if id2 == id {
				t.Fatal("IDs not unique")
			}
		})
	}
}

func TestAttemptIDValidate_Generated(t *testing.T) {
	id, err := NewAttemptID()
	if err != nil {
		t.Fatal(err)
	}
	if !AttemptIDValidate(id) {
		t.Fatalf("generated attempt id %q failed AttemptIDValidate", id)
	}
}

func TestValidateIDPrefix(t *testing.T) {
	if !ValidateIDPrefix("dep-abc", PrefixDeployment) {
		t.Fatal("expected valid")
	}
	if ValidateIDPrefix("dep-", PrefixDeployment) {
		t.Fatal("prefix-only should be invalid")
	}
	if ValidateIDPrefix("", PrefixDeployment) {
		t.Fatal("empty invalid")
	}
}
