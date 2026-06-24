"""B11 operator contract fixtures for contract-parity testing.

These mirror internal/operator/schema.go and categories.go.
If the Go structs change, these fixtures must be updated in the same PR.
"""

SCHEMA_VERSION = "1.0.0"

# Error categories (from categories.go AllErrorCategories)
ERROR_CATEGORIES = frozenset({
    "dependency_conflict", "docker_unavailable", "policy_denied",
    "missing_secret_binding", "budget_exceeded", "trigger_auth_failed",
    "harness_health_failed", "agent_runtime_exception",
    "policy_validation_failed", "network_sandbox_failed",
    "secret_scan_failed", "package_verification_failed",
    "dashboard_unavailable",
})

# Next actions (from categories.go AllNextActions)
NEXT_ACTIONS = frozenset({
    "fix_code", "install_dependency", "start_docker", "set_secret",
    "review_policy_patch", "review_handoff", "increase_budget",
    "rerun", "export_audit", "ask_user",
})

# Risk levels
RISK_LEVELS = frozenset({"low", "medium", "high"})

# Evidence ref types (from schema.go EvidenceRef.Type doc)
EVIDENCE_REF_TYPES = frozenset({
    "audit_seq", "run_id", "policy_rule", "span", "log",
    "redacted_excerpt", "verification",
})

# Trusted control fields (operator-authoritative, never from untrusted evidence)
TRUSTED_CONTROL_FIELDS = frozenset({
    "schema_version", "status", "error_category", "next_action",
    "requires_confirmation", "confirmation_id", "risk_level",
    "rationale", "blocking_rule_id", "policy_digest",
    "affected_destinations", "credential_ids", "run_id", "project_dir",
    "ready", "runtime", "exit_code", "started_at", "finished_at",
    "duration_ms", "invocations", "policy_denials", "root_cause",
    "denied_action", "proposed_patch", "summary", "events",
    "audit_seq", "event_type", "timestamp", "detail",
})

# Untrusted evidence fields (may contain agent/log/source content)
UNTRUSTED_EVIDENCE_FIELDS = frozenset({
    "redacted_excerpts", "evidence_refs", "raw_output_truncated",
    "content",  # within RedactedExcerpt
})

# Per-response required field contracts.
# Each entry maps a CLI subcommand to the fields its JSON response MUST contain
# (when the response is a success, not an error envelope).
RESPONSE_CONTRACTS = {
    "validate": {
        "required": ["schema_version", "ready", "project_dir"],
        "optional": ["runtime", "issues"],
        # if issues present, each issue must have: category, message, next_action
    },
    "summarize": {
        "required": ["schema_version", "run_id", "status", "summary"],
        "optional": ["exit_code", "started_at", "finished_at", "duration_ms",
                      "invocations", "policy_denials", "error_category", "evidence_refs"],
    },
    "explain-failure": {
        "required": ["schema_version", "run_id", "error_category", "root_cause", "next_action"],
        "optional": ["redacted_excerpts", "evidence_refs"],
    },
    "explain-denial": {
        "required": ["schema_version", "denied_action", "blocking_rule_id",
                      "rationale", "next_action"],
        "optional": ["run_id", "policy_digest", "evidence_refs"],
    },
    "recommend-patch": {
        "required": ["schema_version", "proposed_patch", "risk_level",
                      "rationale", "next_action", "confirmation"],
        "optional": ["affected_destinations", "credential_ids", "evidence_refs"],
        # confirmation must be a dict with requires_confirmation=True
    },
    "timeline": {
        "required": ["schema_version", "run_id", "events"],
        # each event: timestamp, event_type, detail
    },
    "next-action": {
        "required": ["schema_version", "next_action", "rationale"],
        "optional": ["run_id", "evidence_refs", "confirmation"],
    },
}

# Map each plugin tool to its CLI subcommand and response contract
TOOL_TO_CONTRACT = {
    "agentpaas_validate_project": ("validate", "validate"),
    "agentpaas_summarize_run": ("summarize", "summarize"),
    "agentpaas_explain_failure": ("explain-failure", "explain-failure"),
    "agentpaas_explain_policy_denial": ("explain-denial", "explain-denial"),
    "agentpaas_recommend_policy_patch": ("recommend-patch", "recommend-patch"),
    "agentpaas_get_run_timeline": ("timeline", "timeline"),
    "agentpaas_next_action": ("next-action", "next-action"),
}