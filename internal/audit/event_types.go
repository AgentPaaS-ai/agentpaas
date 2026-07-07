package audit

const EventTypeSecretInjected = "secret_injected"
const EventTypeSecretLeased = "secret_leased"
const EventTypeSecretRead = "secret_read"
const EventTypeMCPToolCall = "mcp_tool_call"
const EventTypeMCPToolDenied = "mcp_tool_denied"
const EventTypeImmutableViolation = "immutable_violation"

// Trust store event type constants.
const EventTypePublisherTrusted    = "publisher_trusted"
const EventTypePublisherRemoved    = "publisher_removed"
const EventTypePublisherKeyConflict = "publisher_key_conflict"

// Publisher identity event type constants.
const EventTypePublisherIdentityCreated = "publisher_identity_created"
const EventTypePublisherIdentityExported = "publisher_identity_exported"
const EventTypePublisherIdentityImported = "publisher_identity_imported"
const EventTypePublisherIdentityRotated = "publisher_identity_rotated"

// Install consent event type constants (B23).
const EventTypeInstallPolicyApproved   = "install_policy_approved"
const EventTypeInstallDowngradeAllowed = "install_downgrade_allowed"
const EventTypeInstallCredentialMapped = "install_credential_mapped"
const EventTypeInstallRemoved          = "install_removed"
const EventTypeInstallAliasChanged     = "install_alias_changed"

const EventTypeAgentForked = "agent_forked"

// AuditAppender is implemented by audit sinks that accept audit records.
type AuditAppender interface {
	Append(record AuditRecord) error
}
