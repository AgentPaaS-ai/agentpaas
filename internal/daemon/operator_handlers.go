package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	controlv1 "github.com/parvezsyed/agentpaas/api/control/v1"
	"github.com/parvezsyed/agentpaas/internal/audit"
	"github.com/parvezsyed/agentpaas/internal/logging"
	"github.com/parvezsyed/agentpaas/internal/operator"
	"github.com/parvezsyed/agentpaas/internal/pack"
	"github.com/parvezsyed/agentpaas/internal/policy"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// ValidateAgentProject validates an agent project directory and returns a
// structured readiness response with the Block 11 operator schema fields.
func (s *controlServer) ValidateAgentProject(ctx context.Context, req *controlv1.ValidateAgentProjectRequest) (*controlv1.ValidateAgentProjectResponse, error) {
	projectPath := req.GetProjectPath()
	if projectPath == "" {
		return nil, status.Error(codes.InvalidArgument, "project_path is required")
	}

	absProjectPath, err := filepath.Abs(projectPath)
	if err != nil {
		return validationFailure(
			projectPath,
			"",
			operator.ErrDependencyConflict,
			fmt.Sprintf("resolve project path: %v", err),
		), nil
	}
	if isSystemPath(absProjectPath) {
		return validationFailure(
			projectPath,
			"",
			operator.ErrDependencyConflict,
			"project path must not be a system directory",
		), nil
	}

	det, err := pack.DetectProject(projectPath)
	if err != nil {
		return validationFailure(
			projectPath,
			"",
			operator.ErrDependencyConflict,
			err.Error(),
		), nil
	}
	runtime := string(det.Runtime)
	if !det.HasAgentYAML {
		return validationFailure(
			projectPath,
			runtime,
			operator.ErrDependencyConflict,
			"agent.yaml not found; run 'agent init --from-code --noninteractive'",
		), nil
	}

	agentConfig, err := pack.LoadAgentYAML(projectPath)
	if err != nil {
		return validationFailure(
			projectPath,
			runtime,
			operator.ErrDependencyConflict,
			err.Error(),
		), nil
	}
	if agentConfig != nil && invalidAgentEntry(agentConfig.Entry) {
		return validationFailure(
			projectPath,
			runtime,
			operator.ErrDependencyConflict,
			"agent entry must be a relative path within the project directory; absolute paths and traversal are not allowed",
		), nil
	}

	policyPath := filepath.Join(projectPath, "policy.yaml")
	info, err := os.Lstat(policyPath)
	if errors.Is(err, fs.ErrNotExist) {
		return validationFailure(
			projectPath,
			runtime,
			operator.ErrPolicyValidationFailed,
			"policy.yaml not found; run 'agent init --noninteractive' to create default-deny policy",
		), nil
	}
	if err != nil {
		return validationFailure(
			projectPath,
			runtime,
			operator.ErrPolicyValidationFailed,
			fmt.Sprintf("inspect policy.yaml: %v", err),
		), nil
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return validationFailure(
			projectPath,
			runtime,
			operator.ErrPolicyValidationFailed,
			"policy.yaml must not be a symlink",
		), nil
	}

	policyFile, err := os.Open(policyPath)
	if err != nil {
		return validationFailure(
			projectPath,
			runtime,
			operator.ErrPolicyValidationFailed,
			fmt.Sprintf("open policy.yaml: %v", err),
		), nil
	}
	defer func() { _ = policyFile.Close() }()

	parsedPolicy, err := policy.ParsePolicy(policyFile)
	if err != nil {
		return validationFailure(
			projectPath,
			runtime,
			operator.ErrPolicyValidationFailed,
			err.Error()+"; see docs/policy-reference.md for valid fields and examples",
		), nil
	}

	var policyErrors []string
	for _, validationErr := range policy.ValidatePolicy(parsedPolicy) {
		if validationErr.Severity == "error" {
			policyErrors = append(policyErrors, validationErr.Error())
		}
	}
	if len(policyErrors) > 0 {
		return validationFailure(
			projectPath,
			runtime,
			operator.ErrPolicyValidationFailed,
			strings.Join(policyErrors, "; ")+"; see docs/policy-reference.md for valid fields and examples",
		), nil
	}

	return &controlv1.ValidateAgentProjectResponse{
		Validations: []*controlv1.ProjectValidation{{
			Check:   "project_validation",
			Passed:  true,
			Details: fmt.Sprintf("runtime: %s", runtime),
		}},
		Valid:         true,
		Summary:       fmt.Sprintf("project ready=true, runtime=%s", runtime),
		SchemaVersion: operator.SchemaVersion,
		Ready:         true,
		ProjectDir:    projectPath,
		Runtime:       runtime,
		Issues:        []*controlv1.OperatorIssue{},
	}, nil
}

func isSystemPath(path string) bool {
	cleanPath := filepath.Clean(path)
	for _, root := range []string{"/etc", "/usr", "/bin", "/sys", "/proc", "/dev", "/root"} {
		rel, err := filepath.Rel(root, cleanPath)
		if err == nil && (rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))) {
			return true
		}
	}
	return cleanPath == "/var" || cleanPath == "/home"
}

func invalidAgentEntry(entry string) bool {
	entry = strings.TrimSpace(entry)
	if entry == "" {
		return false
	}
	if filepath.IsAbs(entry) || strings.ContainsRune(entry, 0) {
		return true
	}
	for _, component := range strings.FieldsFunc(entry, func(r rune) bool {
		return r == '/' || r == '\\'
	}) {
		if component == ".." {
			return true
		}
	}
	return false
}

func validationFailure(
	projectPath string,
	runtime string,
	category operator.ErrorCategory,
	message string,
) *controlv1.ValidateAgentProjectResponse {
	return &controlv1.ValidateAgentProjectResponse{
		Validations: []*controlv1.ProjectValidation{{
			Check:   "project_validation",
			Passed:  false,
			Details: message,
		}},
		Valid:         false,
		Summary:       fmt.Sprintf("project ready=false, runtime=%s", runtime),
		SchemaVersion: operator.SchemaVersion,
		Ready:         false,
		ProjectDir:    projectPath,
		Runtime:       runtime,
		Issues: []*controlv1.OperatorIssue{{
			Category:   string(category),
			Message:    message,
			NextAction: string(operator.ActionFixCode),
		}},
	}
}

// SummarizeRun generates a structured summary from the run's audit events.
func (s *controlServer) SummarizeRun(ctx context.Context, req *controlv1.SummarizeRunRequest) (*controlv1.SummarizeRunResponse, error) {
	runID := req.GetRunId()
	if runID == "" {
		return nil, status.Error(codes.InvalidArgument, "run_id is required")
	}

	records, err := s.auditRecordsForRun(runID)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "query audit records: %v", err)
	}
	resp := &controlv1.SummarizeRunResponse{
		SchemaVersion: operator.SchemaVersion,
		Status:        "unknown",
	}
	if len(records) == 0 {
		resp.Summary = fmt.Sprintf("no events found for run %s", runID)
		return resp, nil
	}

	var startedAt, finishedAt time.Time
	for _, record := range records {
		switch record.EventType {
		case "run_start":
			if startedAt.IsZero() {
				startedAt = parseAuditTime(record.Timestamp)
			}
			if resp.Status == "unknown" {
				resp.Status = "running"
			}
		case "run_complete":
			resp.Status = "completed"
			finishedAt = parseAuditTime(record.Timestamp)
			resp.ExitCode = auditInt32(record.Payload, "exit_code")
		case "run_failed":
			resp.Status = "failed"
			finishedAt = parseAuditTime(record.Timestamp)
			resp.ExitCode = auditInt32(record.Payload, "exit_code")
			category, _ := diagnosisForRecord(record)
			resp.ErrorCategory = string(category)
		}
		if strings.Contains(record.EventType, "invoke") {
			resp.Invocations++
		}
		if record.EventType == "policy_denied" {
			resp.PolicyDenials++
		}
	}

	resp.StartedAt = toTimestampPB(startedAt)
	resp.FinishedAt = toTimestampPB(finishedAt)
	duration := "unknown duration"
	if !startedAt.IsZero() && !finishedAt.IsZero() {
		resp.DurationMs = finishedAt.Sub(startedAt).Milliseconds()
		duration = (time.Duration(resp.DurationMs) * time.Millisecond).String()
	}
	resp.Summary = fmt.Sprintf(
		"Run %s %s after %s, %d invocations, %d policy denials",
		runID,
		resp.Status,
		duration,
		resp.Invocations,
		resp.PolicyDenials,
	)
	resp.EvidenceRefs = []*controlv1.EvidenceRef{auditEvidence(records[0], "")}
	return resp, nil
}

// ExplainFailure diagnoses a failed run from its audit failure context.
func (s *controlServer) ExplainFailure(ctx context.Context, req *controlv1.ExplainFailureRequest) (*controlv1.ExplainFailureResponse, error) {
	runID := req.GetRunId()
	if runID == "" {
		return nil, status.Error(codes.InvalidArgument, "run_id is required")
	}

	records, err := s.auditRecordsForRun(runID)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "query audit records: %v", err)
	}
	failure, found := latestFailureRecord(records)
	if !found {
		return &controlv1.ExplainFailureResponse{
			RootCause:     fmt.Sprintf("no failure events found for run %s", runID),
			SchemaVersion: operator.SchemaVersion,
			ErrorCategory: string(operator.ErrAgentRuntimeException),
			NextAction:    string(operator.ActionAskUser),
		}, nil
	}

	category, nextAction := diagnosisForRecord(failure)
	rootCause := firstAuditString(failure.Payload, "reason", "detail", "redacted_detail")
	excerpts := make([]*controlv1.RedactedExcerpt, 0, 2)
	for _, field := range []string{"stderr_ref", "stdout_ref"} {
		if content := auditString(failure.Payload, field); content != "" {
			excerpts = append(excerpts, &controlv1.RedactedExcerpt{
				Source:  strings.TrimSuffix(field, "_ref"),
				Content: logging.Redact(content),
			})
		}
	}
	return &controlv1.ExplainFailureResponse{
		RootCause:        logging.Redact(rootCause),
		SchemaVersion:    operator.SchemaVersion,
		ErrorCategory:    string(category),
		RedactedExcerpts: excerpts,
		EvidenceRefs:     []*controlv1.EvidenceRef{auditEvidence(failure, "failure event")},
		NextAction:       string(nextAction),
	}, nil
}

// ExplainPolicyDenial identifies the blocking policy rule for a denied action.
func (s *controlServer) ExplainPolicyDenial(ctx context.Context, req *controlv1.ExplainPolicyDenialRequest) (*controlv1.ExplainPolicyDenialResponse, error) {
	records, err := s.auditRecords()
	if err != nil {
		return nil, status.Errorf(codes.Internal, "query audit records: %v", err)
	}
	var denial audit.AuditRecord
	found := false
	for i := len(records) - 1; i >= 0; i-- {
		record := records[i]
		if record.EventType != "policy_denied" {
			continue
		}
		if req.GetRunId() != "" && auditString(record.Payload, "run_id") != req.GetRunId() {
			continue
		}
		if req.GetDeniedDestination() != "" &&
			firstAuditString(record.Payload, "destination", "action") != req.GetDeniedDestination() {
			continue
		}
		denial = record
		found = true
		break
	}

	if found {
		deniedAction := firstAuditString(denial.Payload, "action", "destination")
		ruleID := auditString(denial.Payload, "rule_id")
		if ruleID == "" {
			ruleID = "default_deny"
		} else if !isValidPolicyRuleID(ruleID) {
			slog.WarnContext(
				ctx,
				"rejected invalid policy rule ID from audit event",
				"rule_id", logging.Redact(ruleID),
				"audit_seq", denial.Seq,
			)
			ruleID = "default_deny"
		}
		rationale := auditString(denial.Payload, "reason")
		if rationale == "" {
			rationale = "destination not in allowed egress list"
		}
		return &controlv1.ExplainPolicyDenialResponse{
			DeniedDestination: deniedAction,
			MatchingRule:      ruleID,
			Explanation:       logging.Redact(rationale),
			SchemaVersion:     operator.SchemaVersion,
			RunId:             req.GetRunId(),
			DeniedAction:      logging.Redact(deniedAction),
			BlockingRuleId:    ruleID,
			PolicyDigest:      logging.Redact(auditString(denial.Payload, "policy_digest")),
			Rationale:         logging.Redact(rationale),
			EvidenceRefs:      []*controlv1.EvidenceRef{auditEvidence(denial, "")},
			NextAction:        string(operator.ActionReviewPolicyPatch),
		}, nil
	}

	destination := logging.Redact(req.GetDeniedDestination())
	return &controlv1.ExplainPolicyDenialResponse{
		DeniedDestination: destination,
		MatchingRule:      "default_deny",
		Explanation:       "destination not in allowed egress list",
		SchemaVersion:     operator.SchemaVersion,
		RunId:             req.GetRunId(),
		DeniedAction:      destination,
		BlockingRuleId:    "default_deny",
		Rationale:         "destination not in allowed egress list",
		NextAction:        string(operator.ActionReviewPolicyPatch),
	}, nil
}

func isValidPolicyRuleID(ruleID string) bool {
	if ruleID == "" || ruleID == "default_deny" {
		return true
	}
	if !strings.HasPrefix(ruleID, "egress[") || !strings.HasSuffix(ruleID, "]") {
		return false
	}

	index := ruleID[len("egress[") : len(ruleID)-1]
	if index == "" {
		return false
	}
	for _, digit := range index {
		if digit < '0' || digit > '9' {
			return false
		}
	}
	return true
}

var (
	safePolicyToken    = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
	safeDomain         = regexp.MustCompile(`^(?:\*\.)?[A-Za-z0-9](?:[A-Za-z0-9.-]*[A-Za-z0-9])?$`)
	confirmationStores sync.Map
)

// RecommendPolicyPatch proposes, but never applies, a policy change.
func (s *controlServer) RecommendPolicyPatch(ctx context.Context, req *controlv1.RecommendPolicyPatchRequest) (*controlv1.RecommendPolicyPatchResponse, error) {
	desired := strings.TrimSpace(req.GetDesiredBehavior())
	fields := strings.Fields(desired)

	var proposal PendingConfirmation
	switch {
	case len(fields) >= 4 &&
		strings.EqualFold(fields[0], "allow") &&
		strings.EqualFold(fields[1], "egress") &&
		strings.EqualFold(fields[2], "to"):
		domain := strings.ToLower(strings.TrimSuffix(fields[3], "."))
		if strings.Contains(domain, "*") {
			proposal = PendingConfirmation{
				ChangeType: "policy_patch",
				RiskLevel:  string(operator.RiskHigh),
				Rationale:  "wildcard egress destinations are rejected; this proposal is retained for review only",
			}
			return s.policyPatchResponse(proposal)
		}
		if len(fields) != 4 {
			return unableToParsePolicyPatch(), nil
		}
		if !safeDomain.MatchString(domain) {
			return unableToParsePolicyPatch(), nil
		}
		risk := operator.RiskMedium
		if domain == "github.com" || domain == "api.openai.com" {
			risk = operator.RiskLow
		}
		allowWildcard := ""
		if strings.HasPrefix(domain, "*.") {
			risk = operator.RiskHigh
			allowWildcard = "\n    allow_wildcard: true"
		}
		proposal = PendingConfirmation{
			ChangeType:    "policy_patch",
			RiskLevel:     string(risk),
			Rationale:     fmt.Sprintf("allow HTTPS egress to %s", domain),
			AffectedDests: []string{domain},
			ProposedPatch: fmt.Sprintf(
				"egress:\n  - domain: %s\n    ports: [443]\n    methods: [GET, POST]%s\n",
				domain,
				allowWildcard,
			),
		}
		evidence, err := s.policyDenialEvidence(domain)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "query policy denial evidence: %v", err)
		}
		proposal.EvidenceRefs = evidence
	case isCredentialBinding(fields):
		directLease := strings.EqualFold(fields[1], "direct_lease") ||
			(len(fields) == 7 && strings.EqualFold(fields[5], "as") && strings.EqualFold(fields[6], "direct_lease"))
		idIndex := 2
		forIndex := 3
		if directLease && strings.EqualFold(fields[1], "direct_lease") {
			idIndex = 3
			forIndex = 4
		}
		credentialID := fields[idIndex]
		service := fields[forIndex+1]
		if !safePolicyToken.MatchString(credentialID) || !safePolicyToken.MatchString(service) {
			return unableToParsePolicyPatch(), nil
		}
		credentialType := "brokered"
		changeType := "credential_binding"
		risk := operator.RiskMedium
		if directLease {
			credentialType = "direct_lease"
			changeType = "direct_lease"
			risk = operator.RiskHigh
		}
		proposal = PendingConfirmation{
			ChangeType:    changeType,
			RiskLevel:     string(risk),
			Rationale:     fmt.Sprintf("bind credential %s for %s", credentialID, service),
			CredentialIDs: []string{credentialID},
			ProposedPatch: fmt.Sprintf(
				"credentials:\n  - id: %s\n    type: %s\n    service: %s\n",
				credentialID,
				credentialType,
				service,
			),
		}
	default:
		return unableToParsePolicyPatch(), nil
	}

	return s.policyPatchResponse(proposal)
}

func isCredentialBinding(fields []string) bool {
	if len(fields) == 5 {
		return strings.EqualFold(fields[0], "bind") &&
			strings.EqualFold(fields[1], "credential") &&
			strings.EqualFold(fields[3], "for")
	}
	if len(fields) == 6 {
		return strings.EqualFold(fields[0], "bind") &&
			strings.EqualFold(fields[1], "direct_lease") &&
			strings.EqualFold(fields[2], "credential") &&
			strings.EqualFold(fields[4], "for")
	}
	return len(fields) == 7 &&
		strings.EqualFold(fields[0], "bind") &&
		strings.EqualFold(fields[1], "credential") &&
		strings.EqualFold(fields[3], "for") &&
		strings.EqualFold(fields[5], "as") &&
		strings.EqualFold(fields[6], "direct_lease")
}

func unableToParsePolicyPatch() *controlv1.RecommendPolicyPatchResponse {
	return &controlv1.RecommendPolicyPatchResponse{
		SchemaVersion: operator.SchemaVersion,
		Rationale:     "unable to parse desired behavior",
		NextAction:    string(operator.ActionAskUser),
	}
}

func (s *controlServer) policyPatchResponse(
	proposal PendingConfirmation,
) (*controlv1.RecommendPolicyPatchResponse, error) {
	id, err := s.proposeTrustBoundaryChange(proposal)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "create confirmation: %v", err)
	}
	evidence := make([]*controlv1.EvidenceRef, 0, len(proposal.EvidenceRefs))
	for _, ref := range proposal.EvidenceRefs {
		evidence = append(evidence, &controlv1.EvidenceRef{
			Type:   ref.Type,
			Ref:    ref.Ref,
			Detail: ref.Detail,
		})
	}
	confirmation := &controlv1.ConfirmationRequirement{
		RequiresConfirmation: true,
		ConfirmationId:       id,
		RiskLevel:            proposal.RiskLevel,
		Rationale:            proposal.Rationale,
		AffectedDestinations: append([]string(nil), proposal.AffectedDests...),
		CredentialIds:        append([]string(nil), proposal.CredentialIDs...),
		EvidenceRefs:         evidence,
	}
	return &controlv1.RecommendPolicyPatchResponse{
		PatchYaml:            proposal.ProposedPatch,
		Explanation:          proposal.Rationale,
		RiskAssessment:       proposal.RiskLevel,
		SchemaVersion:        operator.SchemaVersion,
		ProposedPatch:        proposal.ProposedPatch,
		RiskLevel:            proposal.RiskLevel,
		Rationale:            proposal.Rationale,
		AffectedDestinations: append([]string(nil), proposal.AffectedDests...),
		CredentialIds:        append([]string(nil), proposal.CredentialIDs...),
		EvidenceRefs:         evidence,
		Confirmation:         confirmation,
		NextAction:           string(operator.ActionReviewPolicyPatch),
	}, nil
}

func (s *controlServer) policyDenialEvidence(destination string) ([]operator.EvidenceRef, error) {
	if s.auditIndex == nil {
		return nil, nil
	}
	records, err := s.auditIndex.QueryByEventType("policy_denied", 0)
	if err != nil {
		return nil, err
	}
	start := 0
	if len(records) > 20 {
		start = len(records) - 20
	}
	evidence := make([]operator.EvidenceRef, 0)
	for i := len(records) - 1; i >= start; i-- {
		record, queryErr := s.auditIndex.QueryBySeq(records[i].Seq)
		if queryErr != nil {
			return nil, queryErr
		}
		if firstAuditString(record.Payload, "destination", "action") == destination {
			evidence = append(evidence, operator.EvidenceRef{
				Type: "audit_seq",
				Ref:  strconv.FormatInt(record.Seq, 10),
			})
		}
	}
	return evidence, nil
}

func (s *controlServer) confirmationStore() *ConfirmationStore {
	store, _ := confirmationStores.LoadOrStore(s, NewConfirmationStore())
	return store.(*ConfirmationStore)
}

func attachConfirmationStore(server *controlServer, store *ConfirmationStore) {
	confirmationStores.Store(server, store)
}

func detachConfirmationStore(server *controlServer) {
	confirmationStores.Delete(server)
}

func (s *controlServer) proposeTrustBoundaryChange(change PendingConfirmation) (string, error) {
	return s.confirmationStore().Create(change)
}

// ConfirmChange records a human decision without applying the proposed change.
func (s *controlServer) ConfirmChange(id string, approved bool) error {
	if approved {
		return s.confirmationStore().Approve(id)
	}
	return s.confirmationStore().Decline(id)
}

// ListPendingConfirmations returns all unexpired proposals awaiting review.
func (s *controlServer) ListPendingConfirmations() []PendingConfirmation {
	pending := s.confirmationStore().ListPending()
	sort.Slice(pending, func(i, j int) bool {
		return pending[i].ID < pending[j].ID
	})
	return pending
}

// GetRunTimeline returns a chronological event list for a run.
func (s *controlServer) GetRunTimeline(ctx context.Context, req *controlv1.GetRunTimelineRequest) (*controlv1.GetRunTimelineResponse, error) {
	runID := req.GetRunId()
	if runID == "" {
		return nil, status.Error(codes.InvalidArgument, "run_id is required")
	}
	records, err := s.auditRecordsForRun(runID)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "query audit records: %v", err)
	}
	events := make([]*controlv1.TimelineEvent, 0, len(records))
	for _, record := range records {
		detail, detailErr := timelineDetail(record.Payload)
		if detailErr != nil {
			return nil, status.Errorf(codes.Internal, "marshal timeline detail: %v", detailErr)
		}
		data, marshalErr := json.Marshal(map[string]interface{}{
			"audit_seq": record.Seq,
			"evidence_refs": []map[string]string{{
				"type": "audit_seq",
				"ref":  strconv.FormatInt(record.Seq, 10),
			}},
		})
		if marshalErr != nil {
			return nil, status.Errorf(codes.Internal, "marshal timeline evidence: %v", marshalErr)
		}
		events = append(events, &controlv1.TimelineEvent{
			Timestamp:   toTimestampPB(parseAuditTime(record.Timestamp)),
			Type:        record.EventType,
			Description: detail,
			Data:        data,
		})
	}
	return &controlv1.GetRunTimelineResponse{
		SchemaVersion: operator.SchemaVersion,
		RunId:         runID,
		Events:        events,
	}, nil
}

func timelineDetail(payload map[string]interface{}) (string, error) {
	if detail := firstAuditString(payload, "detail", "description"); detail != "" {
		return logging.Redact(detail), nil
	}

	plainPayload := make(map[string]interface{}, len(payload))
	for key, value := range payload {
		if strings.HasSuffix(strings.ToLower(key), "_ref") {
			plainPayload[key] = "[REDACTED]"
			continue
		}
		plainPayload[key] = value
	}
	data, err := json.Marshal(plainPayload)
	if err != nil {
		return "", err
	}
	return logging.Redact(string(data)), nil
}

// NextAction recommends the next operator action from the latest relevant
// audit event in the supplied run context.
func (s *controlServer) NextAction(ctx context.Context, req *controlv1.NextActionRequest) (*controlv1.NextActionResponse, error) {
	runID := req.GetContext()
	if runID == "confirmations:list" {
		data, err := json.Marshal(s.ListPendingConfirmations())
		if err != nil {
			return nil, status.Errorf(codes.Internal, "marshal pending confirmations: %v", err)
		}
		return &controlv1.NextActionResponse{
			Action:        string(operator.ActionAskUser),
			Params:        map[string]string{"confirmations_json": string(data)},
			Reasoning:     "pending confirmations listed",
			SchemaVersion: operator.SchemaVersion,
			NextAction:    string(operator.ActionAskUser),
			Rationale:     "review pending trust-boundary changes",
		}, nil
	}
	if strings.HasPrefix(runID, "confirm-change:") {
		parts := strings.SplitN(runID, ":", 3)
		if len(parts) != 3 || (parts[1] != "approve" && parts[1] != "decline") ||
			!strings.HasPrefix(parts[2], "confirm_") {
			return nil, status.Error(codes.InvalidArgument, "invalid confirmation decision")
		}
		if err := s.ConfirmChange(parts[2], parts[1] == "approve"); err != nil {
			return nil, status.Error(codes.FailedPrecondition, err.Error())
		}
		if parts[1] == "decline" {
			return s.NextAction(ctx, &controlv1.NextActionRequest{Context: parts[2]})
		}
		return &controlv1.NextActionResponse{
			Action:        string(operator.ActionAskUser),
			Reasoning:     "approval recorded; the proposed change remains unapplied",
			SchemaVersion: operator.SchemaVersion,
			NextAction:    string(operator.ActionAskUser),
			Rationale:     "approval recorded; apply the change through the separate policy workflow",
		}, nil
	}
	if strings.HasPrefix(runID, "confirm_") {
		change, err := s.confirmationStore().Get(runID)
		if err == nil && change.Status == "declined" {
			action := operator.ActionAskUser
			rationale := "change declined; ask the user how to proceed within the current policy"
			if change.ChangeType == "policy_patch" || change.ChangeType == "credential_binding" ||
				change.ChangeType == "direct_lease" {
				action = operator.ActionFixCode
				rationale = "policy patch declined; fix the agent code to operate within current policy"
			}
			return &controlv1.NextActionResponse{
				Action:        string(action),
				Reasoning:     rationale,
				SchemaVersion: operator.SchemaVersion,
				NextAction:    string(action),
				Rationale:     rationale,
			}, nil
		}
	}
	if runID == "" {
		return &controlv1.NextActionResponse{
			Action:        string(operator.ActionAskUser),
			Reasoning:     "no run context provided",
			SchemaVersion: operator.SchemaVersion,
			NextAction:    string(operator.ActionAskUser),
			Rationale:     "no run context provided",
		}, nil
	}

	records, err := s.auditRecordsForRun(runID)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "query audit records: %v", err)
	}
	for i := len(records) - 1; i >= 0; i-- {
		record := records[i]
		switch {
		case isFailureRecord(record):
			_, action := diagnosisForRecord(record)
			rationale := logging.Redact(firstAuditString(record.Payload, "reason", "detail", "redacted_detail"))
			return nextActionResponse(runID, action, rationale, record), nil
		case record.EventType == "run_complete":
			return nextActionResponse(runID, operator.ActionRerun, "run completed successfully", record), nil
		case record.EventType == "policy_denied":
			rationale := logging.Redact(firstAuditString(record.Payload, "reason", "detail"))
			return nextActionResponse(runID, operator.ActionReviewPolicyPatch, rationale, record), nil
		}
	}

	return &controlv1.NextActionResponse{
		Action:        string(operator.ActionAskUser),
		Reasoning:     fmt.Sprintf("no actionable events found for run %s", runID),
		SchemaVersion: operator.SchemaVersion,
		RunId:         runID,
		NextAction:    string(operator.ActionAskUser),
		Rationale:     fmt.Sprintf("no actionable events found for run %s", runID),
	}, nil
}

func toTimestampPB(t time.Time) *timestamppb.Timestamp {
	if t.IsZero() {
		return nil
	}
	return timestamppb.New(t)
}

func (s *controlServer) auditRecords() ([]audit.AuditRecord, error) {
	if s.auditIndex == nil {
		return []audit.AuditRecord{}, nil
	}
	count, err := s.auditIndex.RecordCount()
	if err != nil {
		return nil, err
	}
	records := make([]audit.AuditRecord, 0, count)
	for seq := int64(1); seq <= int64(count); seq++ {
		record, queryErr := s.auditIndex.QueryBySeq(seq)
		if queryErr != nil {
			return nil, queryErr
		}
		records = append(records, *record)
	}
	return records, nil
}

func (s *controlServer) auditRecordsForRun(runID string) ([]audit.AuditRecord, error) {
	records, err := s.auditRecords()
	if err != nil {
		return nil, err
	}
	filtered := make([]audit.AuditRecord, 0, len(records))
	for _, record := range records {
		if auditString(record.Payload, "run_id") == runID {
			filtered = append(filtered, record)
		}
	}
	return filtered, nil
}

func latestFailureRecord(records []audit.AuditRecord) (audit.AuditRecord, bool) {
	for i := len(records) - 1; i >= 0; i-- {
		if isFailureRecord(records[i]) {
			return records[i], true
		}
	}
	return audit.AuditRecord{}, false
}

func isFailureRecord(record audit.AuditRecord) bool {
	return strings.Contains(record.EventType, "run_failed") ||
		strings.Contains(record.EventType, "invoke_failed") ||
		record.EventType == "failure_context" ||
		record.EventType == "budget_exceeded" ||
		record.EventType == "policy_denied"
}

func diagnosisForRecord(record audit.AuditRecord) (operator.ErrorCategory, operator.NextAction) {
	category := firstAuditString(record.Payload, "category", "error_category")
	if category == "" {
		switch record.EventType {
		case "policy_denied":
			category = "mcp_denied"
		case "budget_exceeded":
			category = "budget_exceeded"
		}
	}
	switch category {
	case "budget_exceeded":
		return operator.ErrBudgetExceeded, operator.ActionIncreaseBudget
	case "import_failed", string(operator.ErrDependencyConflict):
		return operator.ErrDependencyConflict, operator.ActionInstallDependency
	case "mcp_denied", string(operator.ErrPolicyDenied):
		return operator.ErrPolicyDenied, operator.ActionReviewPolicyPatch
	case string(operator.ErrMissingSecretBinding):
		return operator.ErrMissingSecretBinding, operator.ActionSetSecret
	case "task_failed", "tool_failed", "code_failed", "invoke_timeout",
		"worker_killed", "saas_failed", "mcp_failed",
		string(operator.ErrAgentRuntimeException):
		return operator.ErrAgentRuntimeException, operator.ActionFixCode
	default:
		return operator.ErrAgentRuntimeException, operator.ActionFixCode
	}
}

func auditString(payload map[string]interface{}, key string) string {
	value, ok := payload[key]
	if !ok {
		return ""
	}
	text, ok := value.(string)
	if !ok {
		return ""
	}
	return text
}

func firstAuditString(payload map[string]interface{}, keys ...string) string {
	for _, key := range keys {
		if value := auditString(payload, key); value != "" {
			return value
		}
	}
	return ""
}

func auditInt32(payload map[string]interface{}, key string) int32 {
	switch value := payload[key].(type) {
	case float64:
		return int32(value)
	case int:
		return int32(value)
	case int32:
		return value
	case int64:
		return int32(value)
	default:
		return 0
	}
}

func parseAuditTime(value string) time.Time {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}
	}
	return parsed
}

func auditEvidence(record audit.AuditRecord, detail string) *controlv1.EvidenceRef {
	return &controlv1.EvidenceRef{
		Type:   "audit_seq",
		Ref:    strconv.FormatInt(record.Seq, 10),
		Detail: logging.Redact(detail),
	}
}

func nextActionResponse(
	runID string,
	action operator.NextAction,
	rationale string,
	record audit.AuditRecord,
) *controlv1.NextActionResponse {
	return &controlv1.NextActionResponse{
		Action:        string(action),
		Reasoning:     rationale,
		SchemaVersion: operator.SchemaVersion,
		RunId:         runID,
		NextAction:    string(action),
		Rationale:     rationale,
		EvidenceRefs:  []*controlv1.EvidenceRef{auditEvidence(record, "")},
	}
}
