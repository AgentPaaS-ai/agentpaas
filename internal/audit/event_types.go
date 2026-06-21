package audit

const EventTypeSecretInjected = "secret_injected"
const EventTypeSecretLeased = "secret_leased"
const EventTypeSecretRead = "secret_read"
const EventTypeMCPToolDenied = "mcp_tool_denied"

// AuditAppender is implemented by audit sinks that accept audit records.
type AuditAppender interface {
	Append(record AuditRecord) error
}
