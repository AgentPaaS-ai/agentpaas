package harness

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/audit"
)

const (
	FailureCategoryTaskFailed     = "task_failed"
	FailureCategoryToolFailed     = "tool_failed"
	FailureCategorySaaSFailed     = "saas_failed"
	FailureCategoryMCPFailed      = "mcp_failed"
	FailureCategoryCodeFailed     = "code_failed"
	FailureCategoryBudgetExceeded = "budget_exceeded"
	FailureCategoryImportFailed   = "import_failed"
	FailureCategoryInvokeTimeout  = "invoke_timeout"
	FailureCategoryWorkerKilled   = "worker_killed"
	FailureCategoryMCPDenied      = "mcp_denied"

	// FailureCategoryResourceLimit (B30-T04) records that the worker was
	// terminated by an explicit resource-limit policy: CPU quota exhausted
	// (SIGXCPU), PID limit exhausted (RLIMIT_NPROC / cgroup pids), or OOM
	// (cgroup memory). Reported separately from accumulated workflow
	// active time — the limit is signed policy, not an accidental ceiling.
	FailureCategoryResourceLimit = "resource_limit_exhausted"

	AvailabilityAvailable   = "available"
	AvailabilityUnavailable = "unavailable"
	AvailabilityRateLimited = "rate_limited"
	AvailabilityForbidden   = "forbidden"
)

var (
	bearerTokenPattern     = regexp.MustCompile(`(?i)bearer(?:\s+|\\s\+)[^\s"'<>]+`)
	secretFieldPattern     = regexp.MustCompile(`(?i)["']?(api[_-]?key|token|password|secret)["']?\s*[=:]\s*["']?[^\s"',}]+["']?`)
	privateKeyPattern      = regexp.MustCompile(`(?is)-----BEGIN .*PRIVATE KEY-----.*?-----END [^-]*PRIVATE KEY-----`)
	urlQueryPattern        = regexp.MustCompile(`https?://[^\s"'<>]*\?[^\s"'<>]+`)
	secretFieldNamePattern = regexp.MustCompile(`(?i)(api[_-]?key|token|password|secret)`)
)

type FailureContext struct {
	RunID             string            `json:"run_id"`
	InvokeID          string            `json:"invoke_id"`
	Category          string            `json:"category"`
	Reason            string            `json:"reason"`
	PolicyDigest      string            `json:"policy_digest"`
	PolicyDecisionIDs []string          `json:"policy_decision_ids"`
	UpstreamEvidence  *UpstreamEvidence `json:"upstream_evidence,omitempty"`
	StderrRef         string            `json:"stderr_ref,omitempty"`
	StdoutRef         string            `json:"stdout_ref,omitempty"`
	RedactedDetail    string            `json:"redacted_detail"`
}

type UpstreamEvidence struct {
	StatusCode   int               `json:"status_code,omitempty"`
	Headers      map[string]string `json:"headers,omitempty"`
	TimingMS     int64             `json:"timing_ms,omitempty"`
	Availability string            `json:"availability"`
	Method       string            `json:"method,omitempty"`
	URL          string            `json:"url,omitempty"`
	BodyHash     string            `json:"body_hash,omitempty"`
	BodyRedacted string            `json:"body,omitempty"`
	Credential   string            `json:"credential,omitempty"`
}

type invokeMetadata struct {
	runID        string
	invokeID     string
	policyDigest string
	decisionIDs  []string
	stdoutRef    string
	stderrRef    string
	audit        AuditAppender
}

func newInvokeMetadata(payload map[string]any, cfg Config) invokeMetadata {
	runID := runIDFromPayload(payload)
	if runID == "" {
		runID = newFailureID("run")
		payload["run_id"] = runID
	}
	invokeID := invokeIDFromPayload(payload)
	if invokeID == "" {
		invokeID = newFailureID("invoke")
		payload["invoke_id"] = invokeID
	}
	return invokeMetadata{
		runID:        runID,
		invokeID:     invokeID,
		policyDigest: policyDigestFromPayload(payload),
		decisionIDs:  nil,
		stdoutRef:    cfg.StdoutPath,
		stderrRef:    cfg.StderrPath,
		audit:        cfg.Audit,
	}
}

func newImportFailureContext(cfg Config, reason, detail string) *FailureContext {
	return &FailureContext{
		RunID:             newFailureID("run"),
		InvokeID:          newFailureID("invoke"),
		Category:          FailureCategoryImportFailed,
		Reason:            defaultString(reason, "import_failed"),
		PolicyDigest:      placeholderPolicyDigest(),
		PolicyDecisionIDs: []string{},
		StdoutRef:         cfg.StdoutPath,
		StderrRef:         cfg.StderrPath,
		RedactedDetail:    redactFailureDetail(detail),
	}
}

func buildFailureContext(errResp *ErrorResponse, meta invokeMetadata, evidence *UpstreamEvidence) *FailureContext {
	if errResp == nil {
		return nil
	}
	category := failureCategory(errResp.Reason, errResp.Status, errResp.Detail, evidence)
	return &FailureContext{
		RunID:             meta.runID,
		InvokeID:          meta.invokeID,
		Category:          category,
		Reason:            defaultString(errResp.Reason, "invoke_failed"),
		PolicyDigest:      defaultString(meta.policyDigest, placeholderPolicyDigest()),
		PolicyDecisionIDs: append([]string(nil), meta.decisionIDs...),
		UpstreamEvidence:  evidence,
		StdoutRef:         meta.stdoutRef,
		StderrRef:         meta.stderrRef,
		RedactedDetail:    redactFailureDetail(errResp.Detail),
	}
}

func failureCategory(reason, status, detail string, evidence *UpstreamEvidence) string {
	switch {
	case status == StatusBudgetExceeded || reason == "budget_exceeded" || strings.Contains(reason, "budget"):
		return FailureCategoryBudgetExceeded
	case reason == "import_failed":
		return FailureCategoryImportFailed
	case reason == "invoke_timeout":
		return FailureCategoryInvokeTimeout
	case reason == "worker_kill_failed":
		return FailureCategoryWorkerKilled
	case isResourceLimitTermination(reason, detail):
		return FailureCategoryResourceLimit
	case reason == "mcp_denied" || strings.Contains(detail, "mcp_denied"):
		return FailureCategoryMCPDenied
	case strings.Contains(detail, "mcp") || strings.Contains(reason, "mcp"):
		return FailureCategoryMCPFailed
	case evidence != nil:
		return FailureCategorySaaSFailed
	case reason == "invalid_result" || strings.Contains(detail, "Traceback"):
		return FailureCategoryCodeFailed
	default:
		return FailureCategoryTaskFailed
	}
}

// isResourceLimitTermination reports whether the failure reason/detail
// indicates a worker was terminated by an explicit resource-limit policy:
// CPU quota exhausted (SIGXCPU / RLIMIT_CPU), PID limit exhausted
// (RLIMIT_NPROC / cgroup pids), or OOM (cgroup memory). These are signed
// policy stops, reported separately from accumulated workflow active time
// (b30-summary.md:414).
func isResourceLimitTermination(reason, detail string) bool {
	switch reason {
	case "cpu_quota_exhausted", "pid_limit_exhausted", "oom_killed":
		return true
	}
	lower := strings.ToLower(detail)
	switch {
	case strings.Contains(lower, "cpu quota exhausted"),
		strings.Contains(lower, "pid limit exhausted"),
		strings.Contains(lower, "memory limit exceeded"),
		strings.Contains(lower, "oom"):
		return true
	}
	return false
}

func attachFailureContext(errResp *ErrorResponse, ctx *FailureContext, appender AuditAppender) *ErrorResponse {
	if errResp == nil || ctx == nil {
		return errResp
	}
	out := *errResp
	cleaned := *ctx
	cleaned.RedactedDetail = redactFailureDetail(cleaned.RedactedDetail)
	out.FailureContext = &cleaned
	if encoded, err := json.Marshal(cleaned); err == nil {
		out.Detail = string(encoded)
	}
	if appender != nil {
		if err := appender.Append(audit.AuditRecord{
			Timestamp:      time.Now().UTC().Format(time.RFC3339Nano),
			EventType:      "failure_context",
			DeploymentMode: "local",
			Actor:          "harness",
			Payload:        failureContextPayload(cleaned),
		}); err != nil {
			log.Printf("harness: audit append (%s): %v", "failure_context", err)
		}
	}
	return &out
}

func failureContextPayload(ctx FailureContext) map[string]interface{} {
	encoded, err := json.Marshal(ctx)
	if err != nil {
		return map[string]interface{}{}
	}
	var payload map[string]interface{}
	if err := json.Unmarshal(encoded, &payload); err != nil {
		return map[string]interface{}{}
	}
	return payload
}

func policyDigestFromPayload(payload map[string]any) string {
	policy, ok := payload["policy"]
	if !ok {
		return placeholderPolicyDigest()
	}
	encoded, err := json.Marshal(policy)
	if err != nil {
		return placeholderPolicyDigest()
	}
	sum := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func placeholderPolicyDigest() string {
	sum := sha256.Sum256([]byte("no-policy"))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func newFailureID(prefix string) string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
	}
	return prefix + "-" + hex.EncodeToString(b[:])
}

func redactFailureDetail(detail string) string {
	detail = privateKeyPattern.ReplaceAllString(detail, "[REDACTED:private_key]")
	detail = bearerTokenPattern.ReplaceAllString(detail, "[REDACTED:bearer_token]")
	detail = secretFieldPattern.ReplaceAllStringFunc(detail, func(match string) string {
		parts := secretFieldNamePattern.FindStringSubmatch(match)
		if len(parts) < 2 {
			return "[REDACTED:secret]"
		}
		return "[REDACTED:" + strings.ToLower(strings.ReplaceAll(parts[1], "-", "_")) + "]"
	})
	return redactURLQuery(detail)
}

func redactURLQuery(raw string) string {
	return urlQueryPattern.ReplaceAllStringFunc(raw, redactURLQueryMatch)
}

func redactURLQueryMatch(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return raw
	}
	parsed.RawQuery = ""
	parsed.ForceQuery = false
	parsed.User = nil
	return parsed.String() + "[REDACTED:query]"
}

func sanitizedURL(raw string) string {
	return redactURLQuery(raw)
}

func hashedHeaders(headers http.Header) map[string]string {
	out := make(map[string]string, len(headers))
	for key, values := range headers {
		out[key] = sha256HexString(strings.Join(values, "\x00"))
	}
	return out
}

func sha256HexString(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func redactedCredentialEvidence() string {
	return "[REDACTED:credential]"
}

func redactedBodyEvidence(body string) (string, string) {
	if body == "" {
		return "", ""
	}
	return "[REDACTED:body]", sha256HexString(body)
}
