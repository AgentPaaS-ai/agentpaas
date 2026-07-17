"""B11 operator contract fixtures for contract-parity testing.

These mirror internal/operator/schema.go and categories.go.
If the Go structs change, these fixtures must be updated in the same PR.
"""

SCHEMA_VERSION = "1.1.0"

# Error categories (from categories.go AllErrorCategories)
ERROR_CATEGORIES = frozenset({
    "dependency_conflict", "docker_unavailable", "policy_denied",
    "missing_secret_binding", "budget_exceeded", "trigger_auth_failed",
    "harness_health_failed", "agent_runtime_exception",
    "policy_validation_failed", "network_sandbox_failed",
    "secret_scan_failed", "package_verification_failed",
    "dashboard_unavailable",
    # B26-T04 typed control/admission categories (additive)
    "deployment_inactive", "idempotency_conflict", "concurrency_unavailable",
    "limit_amendment_denied", "unsafe_pause_boundary", "run_terminal",
    "feature_not_enabled", "missing_scope",
})

# Next actions (from categories.go AllNextActions)
NEXT_ACTIONS = frozenset({
    "fix_code", "install_dependency", "start_docker", "set_secret",
    "review_policy_patch", "review_handoff", "increase_budget",
    "rerun", "export_audit", "ask_user",
    # B26-T04 additive recovery recommendations
    "more_time", "capability_up", "larger_context", "split_task", "stop",
})

# Risk levels
RISK_LEVELS = frozenset({"low", "medium", "high"})

# Authority scopes (invoke vs administrative control separation)
AUTHORITY_SCOPES = frozenset({
    "default", "runs:control", "runs:amend_limits",
})

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
    "category", "message", "type", "ref",
    # B26 additive trusted control fields
    "latest_reason", "latest_action", "attempt_id", "workflow_id",
    "invocation_id", "attempt_report",
})

# Untrusted evidence fields (may contain agent/log/source content)
UNTRUSTED_EVIDENCE_FIELDS = frozenset({
    "redacted_excerpts", "evidence_refs", "raw_output_truncated",
    "content",  # within RedactedExcerpt
    "source", "start_line", "end_line",
})

# Per-response required field contracts.
# Each entry maps a CLI subcommand to the fields its JSON response MUST contain
# (when the response is a success, not an error envelope).
RESPONSE_CONTRACTS = {
    "validate": {
        "required": ["schema_version", "ready", "project_dir"],
        "optional": ["runtime", "issues"],
        # each item in issues follows NESTED_CONTRACTS["validation_issue"]
        "nested": {"issues": "validation_issue"},
    },
    "summarize": {
        "required": ["schema_version", "run_id", "status", "summary"],
        "optional": ["exit_code", "started_at", "finished_at", "duration_ms",
                      "invocations", "policy_denials", "error_category",
                      "evidence_refs", "invoke_response", "attempt_report"],
        "nested": {"attempt_report": "attempt_report"},
    },
    "explain-failure": {
        "required": ["schema_version", "run_id", "error_category", "root_cause", "next_action"],
        "optional": ["redacted_excerpts", "evidence_refs", "latest_reason", "latest_action"],
        "nested": {"redacted_excerpts": "redacted_excerpt"},
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
        # confirmation follows NESTED_CONTRACTS["confirmation_requirement"]
        "nested": {"confirmation": "confirmation_requirement"},
    },
    "timeline": {
        "required": ["schema_version", "run_id", "events"],
        # each event in events follows NESTED_CONTRACTS["timeline_event"]
        "nested": {"events": "timeline_event"},
    },
    "next-action": {
        "required": ["schema_version", "next_action", "rationale"],
        "optional": ["run_id", "evidence_refs", "confirmation", "latest_reason"],
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

# Nested struct field contracts (fields within complex response fields)
NESTED_CONTRACTS = {
    "validation_issue": {
        "required": ["category", "message", "next_action"],
        "optional": ["evidence_refs"],
    },
    "timeline_event": {
        "required": ["timestamp", "event_type", "detail"],
        "optional": ["audit_seq", "evidence_refs"],
    },
    "redacted_excerpt": {
        "required": ["source", "content"],
        "optional": ["start_line", "end_line"],
    },
    "evidence_ref": {
        "required": ["type", "ref"],
        "optional": ["detail"],
    },
    "confirmation_requirement": {
        "required": ["requires_confirmation"],
        "optional": ["confirmation_id", "risk_level", "rationale",
                      "affected_destinations", "credential_ids", "evidence_refs"],
    },
    "attempt_report": {
        "required": ["schema_version", "run_id", "attempt_id", "status"],
        "optional": [
            "reason", "failure_scope", "recovery_disposition", "resume_capability",
            "progress", "checkpoint", "artifacts", "time", "llm_budget",
            "route_decisions", "recommended_actions", "evidence_refs", "created_at",
        ],
        "nested": {
            "progress": "progress_summary",
            "checkpoint": "checkpoint_summary",
            "time": "time_budget_summary",
            "llm_budget": "llm_budget_summary",
        },
    },
    "progress_summary": {
        "required": [],
        "optional": [
            "schema_version", "model_calls_completed", "tool_calls_completed",
            "actions_since_checkpoint", "actions_without_progress",
        ],
    },
    "checkpoint_summary": {
        "required": [],
        "optional": [
            "schema_version", "checkpoint_id", "attempt_id", "run_id",
            "action_count", "total_model_calls", "created_at",
        ],
    },
    "time_budget_summary": {
        "required": [],
        "optional": [
            "schema_version", "attempt_duration_ms", "run_active_time_ms",
            "workflow_active_time_ms", "remaining_ms",
        ],
    },
    "llm_budget_summary": {
        "required": [],
        "optional": [
            "schema_version", "total_tokens", "input_tokens", "output_tokens",
            "total_cost_decimal", "remaining_cost_decimal", "model_calls",
        ],
    },
}
