"""Hermes tool schemas for the AgentPaaS operator contract."""

TOOL_NAMES = [
    "agentpaas_init_project",
    "agentpaas_reconcile_project",
    "agentpaas_validate_project",
    "agentpaas_doctor",
    "agentpaas_pack",
    "agentpaas_run",
    "agentpaas_stop",
    "agentpaas_logs",
    "agentpaas_status",
    "agentpaas_list_runs",
    "agentpaas_get_run_timeline",
    "agentpaas_policy_show",
    "agentpaas_explain_policy_denial",
    "agentpaas_recommend_policy_patch",
    "agentpaas_audit_query",
    "agentpaas_export_audit",
    "agentpaas_summarize_run",
    "agentpaas_explain_failure",
    "agentpaas_next_action",
    "agentpaas_secret_add",
    "agentpaas_secret_list",
    "agentpaas_secret_remove",
    "agentpaas_secret_rotate",
    "agentpaas_secret_test",
    "agentpaas_llm_configure",
    "agentpaas_policy_init",
    "agentpaas_trigger_invoke",
    "agentpaas_cron_add",
    "agentpaas_cron_list",
    "agentpaas_cron_remove",
]

AGENTPAAS_INIT_PROJECT = {
    "name": "agentpaas_init_project",
    "description": "Initialize a new agent project with scaffold and default-deny policy.",
    "parameters": {
        "type": "object",
        "properties": {
            "project_dir": {
                "type": "string",
                "description": "Project directory to initialize (default: current directory).",
            },
            "runtime": {
                "type": "string",
                "description": "Agent runtime: python, langgraph, or crewai.",
                "enum": ["python", "langgraph", "crewai"],
            },
        },
        "required": ["project_dir"],
        "additionalProperties": False,
    },
}

AGENTPAAS_RECONCILE_PROJECT = {
    "name": "agentpaas_reconcile_project",
    "description": "Reconcile agent.yaml and policy from existing agent source code.",
    "parameters": {
        "type": "object",
        "properties": {
            "project_dir": {
                "type": "string",
                "description": "Project directory to reconcile (default: current directory).",
            },
        },
        "required": ["project_dir"],
        "additionalProperties": False,
    },
}

AGENTPAAS_VALIDATE_PROJECT = {
    "name": "agentpaas_validate_project",
    "description": "Validate an agent project directory for pack/run readiness.",
    "parameters": {
        "type": "object",
        "properties": {
            "project_dir": {
                "type": "string",
                "description": "Project directory to validate.",
            },
        },
        "required": ["project_dir"],
        "additionalProperties": False,
    },
}

AGENTPAAS_DOCTOR = {
    "name": "agentpaas_doctor",
    "description": "Run AgentPaaS system diagnostics (daemon, Docker, configuration).",
    "parameters": {
        "type": "object",
        "properties": {},
        "additionalProperties": False,
    },
}

AGENTPAAS_PACK = {
    "name": "agentpaas_pack",
    "description": "Build a signed agent image from a project directory.",
    "parameters": {
        "type": "object",
        "properties": {
            "project_dir": {
                "type": "string",
                "description": "Project directory to pack (default: current directory).",
            },
        },
        "required": ["project_dir"],
        "additionalProperties": False,
    },
}

AGENTPAAS_RUN = {
    "name": "agentpaas_run",
    "description": "Start a new governed agent run from an image digest or project path.",
    "parameters": {
        "type": "object",
        "properties": {
            "image_or_project": {
                "type": "string",
                "description": "Image digest or project path to run.",
            },
        },
        "required": ["image_or_project"],
        "additionalProperties": False,
    },
}

AGENTPAAS_STOP = {
    "name": "agentpaas_stop",
    "description": "Terminate a running agent by run ID.",
    "parameters": {
        "type": "object",
        "properties": {
            "run_id": {
                "type": "string",
                "description": "Run identifier to stop.",
            },
        },
        "required": ["run_id"],
        "additionalProperties": False,
    },
}

AGENTPAAS_LOGS = {
    "name": "agentpaas_logs",
    "description": "Query or tail logs for an agent run.",
    "parameters": {
        "type": "object",
        "properties": {
            "run_id": {
                "type": "string",
                "description": "Run identifier to fetch logs for.",
            },
            "tail": {
                "type": "integer",
                "description": "Number of historical log entries to return.",
            },
        },
        "required": ["run_id"],
        "additionalProperties": False,
    },
}

AGENTPAAS_STATUS = {
    "name": "agentpaas_status",
    "description": "Show daemon status or a specific run's status.",
    "parameters": {
        "type": "object",
        "properties": {
            "run_id": {
                "type": "string",
                "description": "Optional run identifier; omit for daemon status.",
            },
        },
        "additionalProperties": False,
    },
}

AGENTPAAS_GET_RUN_TIMELINE = {
    "name": "agentpaas_get_run_timeline",
    "description": "Show chronological timeline of events for a run.",
    "parameters": {
        "type": "object",
        "properties": {
            "run_id": {
                "type": "string",
                "description": "Run identifier.",
            },
        },
        "required": ["run_id"],
        "additionalProperties": False,
    },
}

AGENTPAAS_POLICY_SHOW = {
    "name": "agentpaas_policy_show",
    "description": "Show the active policy for a project directory or run.",
    "parameters": {
        "type": "object",
        "properties": {
            "project_dir": {
                "type": "string",
                "description": "Project directory whose policy to show.",
            },
            "run_id": {
                "type": "string",
                "description": "Run identifier whose policy to show.",
            },
        },
        "required": ["project_dir"],
        "additionalProperties": False,
    },
}

AGENTPAAS_EXPLAIN_POLICY_DENIAL = {
    "name": "agentpaas_explain_policy_denial",
    "description": "Explain why a destination was denied by policy for a run.",
    "parameters": {
        "type": "object",
        "properties": {
            "run_id": {
                "type": "string",
                "description": "Run identifier associated with the denial.",
            },
            "destination": {
                "type": "string",
                "description": "Denied network destination or action.",
            },
        },
        "required": ["run_id", "destination"],
        "additionalProperties": False,
    },
}

AGENTPAAS_RECOMMEND_POLICY_PATCH = {
    "name": "agentpaas_recommend_policy_patch",
    "description": "Suggest a policy patch for a desired behavior or denied destination.",
    "parameters": {
        "type": "object",
        "properties": {
            "run_id": {
                "type": "string",
                "description": "Run identifier for context.",
            },
            "destination": {
                "type": "string",
                "description": "Denied destination or desired behavior to allow.",
            },
        },
        "required": ["run_id"],
        "additionalProperties": False,
    },
}

AGENTPAAS_AUDIT_QUERY = {
    "name": "agentpaas_audit_query",
    "description": "Query audit log entries, optionally filtered by run or category.",
    "parameters": {
        "type": "object",
        "properties": {
            "run_id": {
                "type": "string",
                "description": "Filter entries to a specific run.",
            },
            "category": {
                "type": "string",
                "description": "Filter entries by event category.",
            },
        },
        "additionalProperties": False,
    },
}

AGENTPAAS_EXPORT_AUDIT = {
    "name": "agentpaas_export_audit",
    "description": "Export audit log entries to a file.",
    "parameters": {
        "type": "object",
        "properties": {
            "output_path": {
                "type": "string",
                "description": "Filesystem path for the exported audit data.",
            },
        },
        "required": ["output_path"],
        "additionalProperties": False,
    },
}

AGENTPAAS_SUMMARIZE_RUN = {
    "name": "agentpaas_summarize_run",
    "description": "Generate a structured summary of a completed or failed run.",
    "parameters": {
        "type": "object",
        "properties": {
            "run_id": {
                "type": "string",
                "description": "Run identifier to summarize.",
            },
        },
        "required": ["run_id"],
        "additionalProperties": False,
    },
}

AGENTPAAS_EXPLAIN_FAILURE = {
    "name": "agentpaas_explain_failure",
    "description": "Analyze a failed run and return root cause with redacted evidence.",
    "parameters": {
        "type": "object",
        "properties": {
            "run_id": {
                "type": "string",
                "description": "Run identifier to diagnose.",
            },
        },
        "required": ["run_id"],
        "additionalProperties": False,
    },
}

AGENTPAAS_NEXT_ACTION = {
    "name": "agentpaas_next_action",
    "description": "Recommend the next operator action based on run context.",
    "parameters": {
        "type": "object",
        "properties": {
            "run_id": {
                "type": "string",
                "description": "Run identifier for context.",
            },
        },
        "required": ["run_id"],
        "additionalProperties": False,
    },
}

AGENTPAAS_SECRET_ADD = {
    "name": "agentpaas_secret_add",
    "description": "Store a credential in macOS Keychain. Value passed via 'value' arg.",
    "parameters": {
        "type": "object",
        "properties": {
            "name": {
                "type": "string",
                "description": "Credential name/label.",
            },
            "value": {
                "type": "string",
                "description": "Credential value (sent through stdin, never logged to argv).",
            },
        },
        "required": ["name", "value"],
        "additionalProperties": False,
    },
}

AGENTPAAS_SECRET_LIST = {
    "name": "agentpaas_secret_list",
    "description": "List stored credentials by label (never by value).",
    "parameters": {
        "type": "object",
        "properties": {},
        "additionalProperties": False,
    },
}

AGENTPAAS_SECRET_REMOVE = {
    "name": "agentpaas_secret_remove",
    "description": "Remove a stored credential.",
    "parameters": {
        "type": "object",
        "properties": {
            "name": {
                "type": "string",
                "description": "Credential name to remove.",
            },
        },
        "required": ["name"],
        "additionalProperties": False,
    },
}

AGENTPAAS_SECRET_ROTATE = {
    "name": "agentpaas_secret_rotate",
    "description": "Replace a credential with a new value (atomic). New value via 'value' arg.",
    "parameters": {
        "type": "object",
        "properties": {
            "name": {
                "type": "string",
                "description": "Credential name to rotate.",
            },
            "value": {
                "type": "string",
                "description": "New credential value (sent through stdin, never logged to argv).",
            },
        },
        "required": ["name", "value"],
        "additionalProperties": False,
    },
}

AGENTPAAS_SECRET_TEST = {
    "name": "agentpaas_secret_test",
    "description": "Validate a credential by making a trivial authenticated call to the provider.",
    "parameters": {
        "type": "object",
        "properties": {
            "name": {
                "type": "string",
                "description": "Credential name to test.",
            },
            "provider": {
                "type": "string",
                "description": "Provider to validate against (openai, anthropic, google, azure).",
            },
        },
        "required": ["name"],
        "additionalProperties": False,
    },
}

AGENTPAAS_LLM_CONFIGURE = {
    "name": "agentpaas_llm_configure",
    "description": "Write the llm: section into agent.yaml for LLM provider integration. Provider, model, and credential are user decisions.",
    "parameters": {
        "type": "object",
        "properties": {
            "project_dir": {
                "type": "string",
                "description": "Project directory containing agent.yaml.",
            },
            "provider": {
                "type": "string",
                "description": "LLM provider: openai, anthropic, or xai.",
            },
            "model": {
                "type": "string",
                "description": "Model name (e.g. gpt-4o, claude-sonnet-4, grok-beta).",
            },
            "credential": {
                "type": "string",
                "description": "Keychain secret name (label, not value). Must match a secret added via agentpaas_secret_add.",
            },
        },
        "required": ["project_dir", "provider", "model", "credential"],
        "additionalProperties": False,
    },
}

AGENTPAAS_POLICY_INIT = {
    "name": "agentpaas_policy_init",
    "description": "Scaffold a policy.yaml from a named egress template. Templates: deny-all, allow-http, allow-llm, allow-mcp.",
    "parameters": {
        "type": "object",
        "properties": {
            "project_dir": {
                "type": "string",
                "description": "Project directory to scaffold policy.yaml into (default: current directory).",
            },
            "template": {
                "type": "string",
                "description": "Egress policy template.",
                "enum": ["deny-all", "allow-http", "allow-llm", "allow-mcp"],
            },
            "force": {
                "type": "boolean",
                "description": "Overwrite an existing policy.yaml if present.",
            },
        },
        "required": ["project_dir"],
        "additionalProperties": False,
    },
}


AGENTPAAS_TRIGGER_INVOKE = {
    "name": "agentpaas_trigger_invoke",
    "description": "Invoke an agent via the trigger REST API.",
    "parameters": {
        "type": "object",
        "properties": {
            "agent_name": {
                "type": "string",
                "description": "Name of the agent to invoke.",
            },
            "payload": {
                "type": "string",
                "description": "Optional path to a payload file to send with the invocation.",
            },
            "content_type": {
                "type": "string",
                "description": "Content type of the payload (default: application/json).",
            },
        },
        "required": ["agent_name"],
        "additionalProperties": False,
    },
}


AGENTPAAS_CRON_ADD = {
    "name": "agentpaas_cron_add",
    "description": "Add a cron schedule for automatic agent invocation.",
    "parameters": {
        "type": "object",
        "properties": {
            "agent_name": {
                "type": "string",
                "description": "Name of the agent to schedule.",
            },
            "expr": {
                "type": "string",
                "description": "Cron expression (e.g. */5 * * * *).",
            },
            "version": {
                "type": "string",
                "description": "Optional agent version to invoke.",
            },
            "timezone": {
                "type": "string",
                "description": "Optional timezone for the cron schedule.",
            },
        },
        "required": ["agent_name", "expr"],
        "additionalProperties": False,
    },
}


AGENTPAAS_LIST_RUNS = {
    "name": "agentpaas_list_runs",
    "description": "List all active and recent agent runs.",
    "parameters": {
        "type": "object",
        "properties": {},
        "additionalProperties": False,
    },
}


AGENTPAAS_CRON_LIST = {
    "name": "agentpaas_cron_list",
    "description": "List all cron schedules.",
    "parameters": {
        "type": "object",
        "properties": {},
        "additionalProperties": False,
    },
}


AGENTPAAS_CRON_REMOVE = {
    "name": "agentpaas_cron_remove",
    "description": "Remove a cron schedule by ID.",
    "parameters": {
        "type": "object",
        "properties": {
            "schedule_id": {
                "type": "string",
                "description": "ID of the cron schedule to remove.",
            },
        },
        "required": ["schedule_id"],
        "additionalProperties": False,
    },
}