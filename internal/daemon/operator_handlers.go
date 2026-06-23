package daemon

import (
	"context"
	"fmt"
	"time"

	controlv1 "github.com/parvezsyed/agentpaas/api/control/v1"
	"github.com/parvezsyed/agentpaas/internal/operator"
	"github.com/parvezsyed/agentpaas/internal/pack"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// ValidateAgentProject validates an agent project directory and returns a
// structured readiness response with the Block 11 operator schema fields.
func (s *stubControlServer) ValidateAgentProject(ctx context.Context, req *controlv1.ValidateAgentProjectRequest) (*controlv1.ValidateAgentProjectResponse, error) {
	projectPath := req.GetProjectPath()
	if projectPath == "" {
		return nil, status.Error(codes.InvalidArgument, "project_path is required")
	}

	// Use the pack package to detect the project runtime.
	det, err := pack.DetectProject(projectPath)
	runtime := ""
	ready := true
	var issues []*controlv1.OperatorIssue
	var validations []*controlv1.ProjectValidation

	if err != nil {
		ready = false
		issues = append(issues, &controlv1.OperatorIssue{
			Category:   string(operator.ErrDependencyConflict),
			Message:     fmt.Sprintf("project detection failed: %v", err),
			NextAction: string(operator.ActionFixCode),
		})
		validations = append(validations, &controlv1.ProjectValidation{
			Check:   "project_detection",
			Passed:  false,
			Details: fmt.Sprintf("detection error: %v", err),
		})
	} else {
		if det != nil {
			runtime = string(det.Runtime)
		}
		validations = append(validations, &controlv1.ProjectValidation{
			Check:   "project_detection",
			Passed:  true,
			Details: fmt.Sprintf("runtime: %s", runtime),
		})
	}

	return &controlv1.ValidateAgentProjectResponse{
		Validations:    validations,
		Valid:          ready,
		Summary:        fmt.Sprintf("project ready=%v, runtime=%s", ready, runtime),
		SchemaVersion:  operator.SchemaVersion,
		Ready:          ready,
		ProjectDir:     projectPath,
		Runtime:        runtime,
		Issues:         issues,
	}, nil
}

// SummarizeRun generates a structured summary for a completed run. P1
// implementation: the run store is not yet wired, so we return a
// not-found error for unknown run ids. When the harness run store is
// available, this method will pull the run record and build the summary.
func (s *stubControlServer) SummarizeRun(ctx context.Context, req *controlv1.SummarizeRunRequest) (*controlv1.SummarizeRunResponse, error) {
	runID := req.GetRunId()
	if runID == "" {
		return nil, status.Error(codes.InvalidArgument, "run_id is required")
	}
	// P1: return a minimal summary. Full run-store integration is in B11-T04.
	return &controlv1.SummarizeRunResponse{
		Summary:       fmt.Sprintf("run %s summary: no run store available in P1 stub", runID),
		SchemaVersion: operator.SchemaVersion,
		Status:        "unknown",
	}, nil
}

// ExplainFailure diagnoses a failed run. P1 implementation: returns a
// not-found for unknown run ids. Full failure-context integration is in
// B11-T04.
func (s *stubControlServer) ExplainFailure(ctx context.Context, req *controlv1.ExplainFailureRequest) (*controlv1.ExplainFailureResponse, error) {
	runID := req.GetRunId()
	if runID == "" {
		return nil, status.Error(codes.InvalidArgument, "run_id is required")
	}
	return &controlv1.ExplainFailureResponse{
		RootCause:     fmt.Sprintf("no failure context available for run %s in P1 stub", runID),
		SchemaVersion: operator.SchemaVersion,
		ErrorCategory: string(operator.ErrAgentRuntimeException),
		NextAction:    string(operator.ActionAskUser),
	}, nil
}

// ExplainPolicyDenial identifies the blocking policy rule for a denied action.
// P1 implementation: the policy compiler integration is in B11-T04.
func (s *stubControlServer) ExplainPolicyDenial(ctx context.Context, req *controlv1.ExplainPolicyDenialRequest) (*controlv1.ExplainPolicyDenialResponse, error) {
	return &controlv1.ExplainPolicyDenialResponse{
		SchemaVersion:  operator.SchemaVersion,
		RunId:          req.GetRunId(),
		DeniedAction:   fmt.Sprintf("egress to %s", req.GetDeniedDestination()),
		BlockingRuleId: "default_deny",
		Rationale:      "destination not in allowed egress list",
		NextAction:     string(operator.ActionReviewPolicyPatch),
	}, nil
}

// RecommendPolicyPatch proposes a policy patch. P1 implementation: returns
// a proposal with confirmation required. Full policy-compiler integration
// is in B11-T05.
func (s *stubControlServer) RecommendPolicyPatch(ctx context.Context, req *controlv1.RecommendPolicyPatchRequest) (*controlv1.RecommendPolicyPatchResponse, error) {
	return &controlv1.RecommendPolicyPatchResponse{
		PatchYaml:      "# proposed patch based on desired behavior",
		Explanation:    "adds egress rule for requested destination",
		RiskAssessment: string(operator.RiskMedium),
		SchemaVersion:  operator.SchemaVersion,
		ProposedPatch:  "# proposed patch based on desired behavior",
		RiskLevel:      string(operator.RiskMedium),
		Rationale:      req.GetDesiredBehavior(),
		Confirmation: &controlv1.ConfirmationRequirement{
			RequiresConfirmation: true,
			RiskLevel:            string(operator.RiskMedium),
			Rationale:            "adds new egress destination — requires confirmation",
		},
		NextAction: string(operator.ActionReviewPolicyPatch),
	}, nil
}

// GetRunTimeline returns a chronological event list for a run. P1
// implementation: the audit store integration is in B11-T04.
func (s *stubControlServer) GetRunTimeline(ctx context.Context, req *controlv1.GetRunTimelineRequest) (*controlv1.GetRunTimelineResponse, error) {
	runID := req.GetRunId()
	if runID == "" {
		return nil, status.Error(codes.InvalidArgument, "run_id is required")
	}
	return &controlv1.GetRunTimelineResponse{
		SchemaVersion: operator.SchemaVersion,
		RunId:         runID,
		Events:        []*controlv1.TimelineEvent{},
	}, nil
}

// NextAction recommends the next operator action. P1 implementation: returns
// ask_user when no run context is provided. Full integration is in B11-T04.
func (s *stubControlServer) NextAction(ctx context.Context, req *controlv1.NextActionRequest) (*controlv1.NextActionResponse, error) {
	return &controlv1.NextActionResponse{
		Action:        "ask_user",
		Reasoning:     "no run context available in P1 stub",
		SchemaVersion: operator.SchemaVersion,
		NextAction:    string(operator.ActionAskUser),
		Rationale:     "unable to determine next action without run context",
	}, nil
}

// toTimestampPB converts a time.Time to a protobuf Timestamp, returning nil
// for zero times. Kept for future use by B11-T04 run-store integration.
func toTimestampPB(t time.Time) *timestamppb.Timestamp {
	if t.IsZero() {
		return nil
	}
	return timestamppb.New(t)
}

var _ = toTimestampPB // referenced by B11-T04 handlers
