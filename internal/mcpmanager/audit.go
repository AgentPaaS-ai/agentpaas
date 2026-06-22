package mcpmanager

import (
	"time"

	"github.com/parvezsyed/agentpaas/internal/audit"
)

// AuditToolCall records an MCP tool call audit event.
// decision: "allowed" or "denied"
// policyRuleID: the policy rule that authorized the call (or "undeclared" if denied)
// credentialID: the credential used (empty if none)
func AuditToolCall(appender audit.AuditAppender, serverID, tool, agentID, runID, decision, policyRuleID, credentialID string, inputHash, outputHash string, timingMS int64) {
	if appender == nil {
		return
	}
	_ = appender.Append(audit.AuditRecord{
		Timestamp:      time.Now().UTC().Format(time.RFC3339Nano),
		EventType:      audit.EventTypeMCPToolCall,
		DeploymentMode: "local",
		Actor:          agentID,
		Payload: map[string]interface{}{
			"server_id":      serverID,
			"tool":           tool,
			"agent_id":       agentID,
			"run_id":         runID,
			"decision":       decision,
			"policy_rule_id": policyRuleID,
			"credential_id":  credentialID,
			"input_hash":     inputHash,
			"output_hash":    outputHash,
			"timing_ms":      timingMS,
			"host_affecting": IsHostAffecting(tool),
		},
	})
}

// AuditToolDenied records an MCP tool denial audit event with full metadata.
func AuditToolDenied(appender audit.AuditAppender, serverID, tool, agentID, runID, reason, policyRuleID string) {
	if appender == nil {
		return
	}
	_ = appender.Append(audit.AuditRecord{
		Timestamp:      time.Now().UTC().Format(time.RFC3339Nano),
		EventType:      audit.EventTypeMCPToolDenied,
		DeploymentMode: "local",
		Actor:          agentID,
		Payload: map[string]interface{}{
			"server_id":      serverID,
			"tool":           tool,
			"agent_id":       agentID,
			"run_id":         runID,
			"decision":       "denied",
			"reason":         reason,
			"policy_rule_id": policyRuleID,
			"host_affecting": IsHostAffecting(tool),
		},
	})
}
