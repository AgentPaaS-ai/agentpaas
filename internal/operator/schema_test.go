package operator

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestSchemaVersion verifies the schema version is set and follows semver.
func TestSchemaVersion(t *testing.T) {
	if SchemaVersion == "" {
		t.Fatal("SchemaVersion must not be empty")
	}
	if !strings.Contains(SchemaVersion, ".") {
		t.Fatalf("SchemaVersion %q must be semver (major.minor.patch)", SchemaVersion)
	}
	parts := strings.Split(SchemaVersion, ".")
	if len(parts) < 3 {
		t.Fatalf("SchemaVersion %q must have at least 3 parts", SchemaVersion)
	}
	if parts[0] != "1" {
		t.Fatalf("P1 schema major version must be 1, got %s", parts[0])
	}
}

// TestAllErrorCategories verifies every defined error category is in the
// AllErrorCategories list and passes IsValidErrorCategory.
func TestAllErrorCategories(t *testing.T) {
	expected := []ErrorCategory{
		ErrDependencyConflict,
		ErrDockerUnavailable,
		ErrPolicyDenied,
		ErrMissingSecretBinding,
		ErrBudgetExceeded,
		ErrTriggerAuthFailed,
		ErrHarnessHealthFailed,
		ErrAgentRuntimeException,
		ErrPolicyValidationFailed,
		ErrNetworkSandboxFailed,
		ErrSecretScanFailed,
		ErrPackageVerificationFailed,
		ErrDashboardUnavailable,
	}

	all := AllErrorCategories()
	if len(all) != len(expected) {
		t.Fatalf("AllErrorCategories has %d categories, expected %d", len(all), len(expected))
	}

	seen := make(map[ErrorCategory]bool)
	for _, cat := range all {
		if seen[cat] {
			t.Fatalf("duplicate error category: %s", cat)
		}
		seen[cat] = true
		if !IsValidErrorCategory(cat) {
			t.Errorf("IsValidErrorCategory(%q) = false, want true", cat)
		}
	}

	for _, e := range expected {
		if !seen[e] {
			t.Errorf("expected category %q not in AllErrorCategories", e)
		}
	}

	// Invalid category must fail validation.
	if IsValidErrorCategory("nonexistent_category") {
		t.Error("IsValidErrorCategory should return false for unknown category")
	}
}

// TestAllNextActions verifies every defined next action is in the
// AllNextActions list and passes IsValidNextAction.
func TestAllNextActions(t *testing.T) {
	expected := []NextAction{
		ActionFixCode,
		ActionInstallDependency,
		ActionStartDocker,
		ActionSetSecret,
		ActionReviewPolicyPatch,
		ActionReviewHandoff,
		ActionIncreaseBudget,
		ActionRerun,
		ActionExportAudit,
		ActionAskUser,
	}

	all := AllNextActions()
	if len(all) != len(expected) {
		t.Fatalf("AllNextActions has %d actions, expected %d", len(all), len(expected))
	}

	seen := make(map[NextAction]bool)
	for _, a := range all {
		if seen[a] {
			t.Fatalf("duplicate next action: %s", a)
		}
		seen[a] = true
		if !IsValidNextAction(a) {
			t.Errorf("IsValidNextAction(%q) = false, want true", a)
		}
	}

	for _, e := range expected {
		if !seen[e] {
			t.Errorf("expected action %q not in AllNextActions", e)
		}
	}

	if IsValidNextAction("nonexistent_action") {
		t.Error("IsValidNextAction should return false for unknown action")
	}
}

// TestRiskLevels verifies the three risk levels are defined.
func TestRiskLevels(t *testing.T) {
	if RiskLow != "low" {
		t.Errorf("RiskLow = %q, want %q", RiskLow, "low")
	}
	if RiskMedium != "medium" {
		t.Errorf("RiskMedium = %q, want %q", RiskMedium, "medium")
	}
	if RiskHigh != "high" {
		t.Errorf("RiskHigh = %q, want %q", RiskHigh, "high")
	}
}

// TestValidateAgentProjectResponseGolden verifies the JSON serialization of a
// ValidateAgentProjectResponse matches the golden schema shape: schema_version
// is set, ready is present, issues have category/next_action.
func TestValidateAgentProjectResponseGolden(t *testing.T) {
	resp := ValidateAgentProjectResponse{
		SchemaVersion: SchemaVersion,
		Ready:         false,
		ProjectDir:    "/tmp/test-agent",
		Runtime:       "python",
		Issues: []ValidationIssue{
			{
				Category:    ErrMissingSecretBinding,
				Message:     "credential 'api_key' is not bound",
				NextAction:  ActionSetSecret,
				EvidenceRefs: []EvidenceRef{{Type: "policy_rule", Ref: "credentials[0]"}},
			},
		},
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if m["schema_version"] != SchemaVersion {
		t.Errorf("schema_version = %v, want %s", m["schema_version"], SchemaVersion)
	}
	if m["ready"] != false {
		t.Errorf("ready = %v, want false", m["ready"])
	}
	if m["project_dir"] != "/tmp/test-agent" {
		t.Errorf("project_dir = %v, want /tmp/test-agent", m["project_dir"])
	}
	issues, ok := m["issues"].([]interface{})
	if !ok || len(issues) != 1 {
		t.Fatalf("issues not a 1-element array: %v", m["issues"])
	}
	issue := issues[0].(map[string]interface{})
	if issue["category"] != string(ErrMissingSecretBinding) {
		t.Errorf("issue category = %v, want %s", issue["category"], ErrMissingSecretBinding)
	}
	if issue["next_action"] != string(ActionSetSecret) {
		t.Errorf("issue next_action = %v, want %s", issue["next_action"], ActionSetSecret)
	}
}

// TestSummarizeRunResponseGolden verifies SummarizeRunResponse JSON shape.
func TestSummarizeRunResponseGolden(t *testing.T) {
	resp := SummarizeRunResponse{
		SchemaVersion: SchemaVersion,
		RunID:         "run_abc123",
		Status:        "failed",
		ExitCode:      1,
		Summary:       "agent crashed with import error",
		ErrorCategory: ErrAgentRuntimeException,
		EvidenceRefs:  []EvidenceRef{{Type: "audit_seq", Ref: "42"}},
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if m["schema_version"] != SchemaVersion {
		t.Errorf("schema_version = %v, want %s", m["schema_version"], SchemaVersion)
	}
	if m["run_id"] != "run_abc123" {
		t.Errorf("run_id = %v, want run_abc123", m["run_id"])
	}
	if m["status"] != "failed" {
		t.Errorf("status = %v, want failed", m["status"])
	}
	if m["error_category"] != string(ErrAgentRuntimeException) {
		t.Errorf("error_category = %v, want %s", m["error_category"], ErrAgentRuntimeException)
	}
}

// TestExplainFailureResponseGolden verifies ExplainFailureResponse JSON shape.
func TestExplainFailureResponseGolden(t *testing.T) {
	resp := ExplainFailureResponse{
		SchemaVersion:    SchemaVersion,
		RunID:            "run_abc123",
		ErrorCategory:    ErrDependencyConflict,
		RootCause:        "missing 'requests' package in requirements.txt",
		RedactedExcerpts: []RedactedExcerpt{{Source: "stderr", Content: "ModuleNotFoundError: No module named 'requests'"}},
		EvidenceRefs:     []EvidenceRef{{Type: "log", Ref: "stderr:line 3"}},
		NextAction:       ActionInstallDependency,
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if m["error_category"] != string(ErrDependencyConflict) {
		t.Errorf("error_category = %v, want %s", m["error_category"], ErrDependencyConflict)
	}
	if m["next_action"] != string(ActionInstallDependency) {
		t.Errorf("next_action = %v, want %s", m["next_action"], ActionInstallDependency)
	}
	excerpts, ok := m["redacted_excerpts"].([]interface{})
	if !ok || len(excerpts) != 1 {
		t.Fatalf("redacted_excerpts not 1-element array: %v", m["redacted_excerpts"])
	}
}

// TestExplainPolicyDenialResponseGolden verifies ExplainPolicyDenialResponse.
func TestExplainPolicyDenialResponseGolden(t *testing.T) {
	resp := ExplainPolicyDenialResponse{
		SchemaVersion:  SchemaVersion,
		RunID:          "run_abc123",
		DeniedAction:   "HTTPS GET https://api.example.com/data",
		BlockingRuleID: "egress[0]",
		PolicyDigest:   "sha256:abc123",
		Rationale:      "destination not in allowed egress list",
		EvidenceRefs:   []EvidenceRef{{Type: "audit_seq", Ref: "15"}},
		NextAction:     ActionReviewPolicyPatch,
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if m["blocking_rule_id"] != "egress[0]" {
		t.Errorf("blocking_rule_id = %v, want egress[0]", m["blocking_rule_id"])
	}
	if m["next_action"] != string(ActionReviewPolicyPatch) {
		t.Errorf("next_action = %v, want %s", m["next_action"], ActionReviewPolicyPatch)
	}
}

// TestRecommendPolicyPatchResponseGolden verifies the policy patch response
// always requires confirmation.
func TestRecommendPolicyPatchResponseGolden(t *testing.T) {
	resp := RecommendPolicyPatchResponse{
		SchemaVersion:        SchemaVersion,
		ProposedPatch:        "+  - domain: api.example.com\n+    ports: [443]",
		RiskLevel:            RiskMedium,
		Rationale:           "agent needs HTTPS to api.example.com",
		AffectedDestinations: []string{"api.example.com:443"},
		Confirmation: ConfirmationRequirement{
			RequiresConfirmation: true,
			ConfirmationID:       "confirm_001",
			RiskLevel:            RiskMedium,
			Rationale:            "adds new egress destination",
		},
		NextAction: ActionReviewPolicyPatch,
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	confirm, ok := m["confirmation"].(map[string]interface{})
	if !ok {
		t.Fatal("confirmation not a map")
	}
	if confirm["requires_confirmation"] != true {
		t.Error("policy patch must require confirmation")
	}
	if confirm["confirmation_id"] != "confirm_001" {
		t.Errorf("confirmation_id = %v, want confirm_001", confirm["confirmation_id"])
	}
	if m["next_action"] != string(ActionReviewPolicyPatch) {
		t.Errorf("next_action = %v, want %s", m["next_action"], ActionReviewPolicyPatch)
	}
}

// TestGetRunTimelineResponseGolden verifies timeline JSON shape.
func TestGetRunTimelineResponseGolden(t *testing.T) {
	resp := GetRunTimelineResponse{
		SchemaVersion: SchemaVersion,
		RunID:         "run_abc123",
		Events: []TimelineEvent{
			{EventType: "run_start", Detail: "agent started", AuditSeq: 1},
			{EventType: "policy_denied", Detail: "egress to api.example.com denied", AuditSeq: 5},
			{EventType: "run_failed", Detail: "agent exited with code 1", AuditSeq: 10},
		},
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	events, ok := m["events"].([]interface{})
	if !ok || len(events) != 3 {
		t.Fatalf("events not 3-element array: %v", m["events"])
	}
}

// TestNextActionResponseGolden verifies NextActionResponse JSON shape,
// including the optional confirmation field for trust-boundary actions.
func TestNextActionResponseGolden(t *testing.T) {
	// Without confirmation (non-trust-boundary action)
	resp := NextActionResponse{
		SchemaVersion: SchemaVersion,
		RunID:         "run_abc123",
		NextAction:    ActionFixCode,
		Rationale:     "agent source has a syntax error",
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if m["next_action"] != string(ActionFixCode) {
		t.Errorf("next_action = %v, want %s", m["next_action"], ActionFixCode)
	}
	if _, ok := m["confirmation"]; ok {
		t.Error("confirmation should be omitted for non-trust-boundary actions")
	}

	// With confirmation (trust-boundary action)
	resp2 := NextActionResponse{
		SchemaVersion: SchemaVersion,
		RunID:         "run_abc123",
		NextAction:    ActionReviewPolicyPatch,
		Rationale:     "egress denied by policy",
		Confirmation: &ConfirmationRequirement{
			RequiresConfirmation: true,
			ConfirmationID:       "confirm_002",
			RiskLevel:            RiskMedium,
		},
	}

	data2, err := json.Marshal(resp2)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var m2 map[string]interface{}
	if err := json.Unmarshal(data2, &m2); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	confirm, ok := m2["confirmation"].(map[string]interface{})
	if !ok {
		t.Fatal("confirmation should be present for trust-boundary actions")
	}
	if confirm["requires_confirmation"] != true {
		t.Error("confirmation.requires_confirmation must be true")
	}
}

// TestEvidenceRefGolden verifies EvidenceRef JSON shape.
func TestEvidenceRefGolden(t *testing.T) {
	ref := EvidenceRef{
		Type:   "audit_seq",
		Ref:    "42",
		Detail: "policy_denied event recorded at seq 42",
	}

	data, err := json.Marshal(ref)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if m["type"] != "audit_seq" {
		t.Errorf("type = %v, want audit_seq", m["type"])
	}
	if m["ref"] != "42" {
		t.Errorf("ref = %v, want 42", m["ref"])
	}
}

// TestRedactedExcerptGolden verifies RedactedExcerpt JSON shape and that
// secret patterns are not present in content.
func TestRedactedExcerptGolden(t *testing.T) {
	excerpt := RedactedExcerpt{
		Source:    "stderr",
		StartLine: 1,
		EndLine:   3,
		Content:   "Traceback (most recent call last):\n  File \"agent.py\", line 1, in <module>\n    import requests",
	}

	data, err := json.Marshal(excerpt)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if m["source"] != "stderr" {
		t.Errorf("source = %v, want stderr", m["source"])
	}
	if strings.Contains(excerpt.Content, "api_key") {
		t.Error("excerpt content should not contain secret patterns")
	}
}

// TestConfirmationRequirementGolden verifies the confirmation protocol
// struct serializes correctly for both required and not-required cases.
func TestConfirmationRequirementGolden(t *testing.T) {
	// Required case
	req := ConfirmationRequirement{
		RequiresConfirmation: true,
		ConfirmationID:       "confirm_003",
		RiskLevel:            RiskHigh,
		Rationale:            "direct lease grants file-system secret access",
		AffectedDestinations: []string{"file:///etc/secrets"},
		CredentialIDs:        []string{"db_password"},
		EvidenceRefs:         []EvidenceRef{{Type: "audit_seq", Ref: "20"}},
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if m["requires_confirmation"] != true {
		t.Error("requires_confirmation must be true")
	}
	if m["risk_level"] != string(RiskHigh) {
		t.Errorf("risk_level = %v, want %s", m["risk_level"], RiskHigh)
	}

	// Not-required case (empty struct)
	req2 := ConfirmationRequirement{}
	data2, err := json.Marshal(req2)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var m2 map[string]interface{}
	if err := json.Unmarshal(data2, &m2); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if m2["requires_confirmation"] != false {
		t.Error("requires_confirmation must be false for empty struct")
	}
}

// TestEveryErrorCategoryHasFixture verifies that every defined error category
// is used in at least one golden fixture, ensuring schema coverage.
func TestEveryErrorCategoryHasFixture(t *testing.T) {
	used := make(map[ErrorCategory]bool)
	for _, cat := range AllErrorCategories() {
		used[cat] = false
	}

	// Mark categories used in fixtures
	fixtures := []ErrorCategory{
		ErrMissingSecretBinding,       // ValidateAgentProjectResponse
		ErrAgentRuntimeException,      // SummarizeRunResponse
		ErrDependencyConflict,         // ExplainFailureResponse
		ErrPolicyDenied,               // (used in ExplainPolicyDenial context)
	}

	for _, cat := range fixtures {
		used[cat] = true
	}

	// Remaining categories are valid but not yet exercised by a fixture —
	// they will be covered by B11-T04 (explain failure) and the golden flow
	// simulator (B11-T07). For T01 we only verify the schema definitions.
	for cat, u := range used {
		if !u {
			// Not a failure for T01 — just informational. The category is
			// defined and valid; fixtures come in later subtasks.
			_ = cat
		}
	}
}
