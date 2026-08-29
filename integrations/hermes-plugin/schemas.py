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
    "agentpaas_identity_show",
    "agentpaas_export",
    "agentpaas_bundle_inspect",
    "agentpaas_install",
    "agentpaas_installed_list",
    "agentpaas_provenance_show",
    "agentpaas_trust_list",
    "agentpaas_fork",
    "agentpaas_cloud_whoami",
    "agentpaas_cloud_registry",
    "agentpaas_cloud_push",
    "agentpaas_cloud_deploy",
    "agentpaas_cloud_deployments",
    "agentpaas_cloud_undeploy",
    "agentpaas_cloud_invoke",
    "agentpaas_cloud_result",
    "agentpaas_cloud_logs",
    "agentpaas_cloud_usage",
    "agentpaas_cloud_images",
    "agentpaas_cloud_events",
    "agentpaas_cloud_audit",
    "agentpaas_cloud_audit_export",
    "agentpaas_cloud_metrics",
    "agentpaas_cloud_secrets_list",
    "agentpaas_cloud_secrets_push",
    "agentpaas_cloud_login",
    "agentpaas_cloud_cron_list",
    "agentpaas_cloud_cron_set",
    "agentpaas_cloud_cron_disable",
    "agentpaas_cloud_cron_enable",
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
                "description": "Provider to validate against (openrouter, openai, anthropic, xai, nous).",
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
                "description": "LLM provider: openrouter, openai, anthropic, xai, or nous.",
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
                "description": "Payload to send to the agent. Can be either inline JSON (e.g. '{\"city\": \"Folsom\"}') or a path to a payload file. For simple key-value inputs, pass inline JSON directly — no need to create a file.",
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
            "payload": {
                "type": "string",
                "description": "Optional invocation payload as inline JSON (e.g. 'city': 'Folsom').",
            },
            "content_type": {
                "type": "string",
                "description": "Payload content type (default: application/json).",
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


AGENTPAAS_IDENTITY_SHOW = {
    "name": "agentpaas_identity_show",
    "description": "Show the current publisher identity (fingerprint, name). Read-only. If no identity exists, returns guidance to run `agentpaas identity init` in the terminal (identity creation is terminal-gated).",
    "parameters": {
        "type": "object",
        "properties": {},
        "additionalProperties": False,
    },
}


AGENTPAAS_EXPORT = {
    "name": "agentpaas_export",
    "description": "Export an agent project as a signed bundle for sharing. Returns bundle path, digest, publisher fingerprint, and a canned instruction to read the fingerprint to the receiver over another channel (phone, Signal, etc.) so they can verify the bundle.",
    "parameters": {
        "type": "object",
        "properties": {
            "project_dir": {
                "type": "string",
                "description": "Project directory to export.",
            },
            "with_image": {
                "type": "boolean",
                "description": "Include the Docker image in the bundle.",
            },
            "output": {
                "type": "string",
                "description": "Output path for the bundle file.",
            },
        },
        "required": ["project_dir"],
        "additionalProperties": False,
    },
}


AGENTPAAS_BUNDLE_INSPECT = {
    "name": "agentpaas_bundle_inspect",
    "description": "Inspect a signed agent bundle and return its structured report (policy, lints, provenance, credentials needed).",
    "parameters": {
        "type": "object",
        "properties": {
            "bundle_path": {
                "type": "string",
                "description": "Path to the bundle file to inspect.",
            },
        },
        "required": ["bundle_path"],
        "additionalProperties": False,
    },
}


AGENTPAAS_INSTALL = {
    "name": "agentpaas_install",
    "description": "Install a signed agent bundle. NEVER includes approval parameters (no confirm_fingerprint, no accept_policy) — consent happens in terminal. The tool will fail at trust/consent steps; instructs user to run `agentpaas install <file>` in their terminal and follow prompts.",
    "parameters": {
        "type": "object",
        "properties": {
            "bundle_path": {
                "type": "string",
                "description": "Path to the bundle file to install.",
            },
            "alias": {
                "type": "string",
                "description": "Optional alias to assign to the installed agent.",
            },
            "map_credential": {
                "type": "array",
                "items": {"type": "string"},
                "description": "Optional list of credential mappings (e.g. ['local_key=remote_key']).",
            },
        },
        "required": ["bundle_path"],
        "additionalProperties": False,
    },
}


AGENTPAAS_INSTALLED_LIST = {
    "name": "agentpaas_installed_list",
    "description": "List all installed agents with their refs, aliases, and fingerprints.",
    "parameters": {
        "type": "object",
        "properties": {},
        "additionalProperties": False,
    },
}


AGENTPAAS_PROVENANCE_SHOW = {
    "name": "agentpaas_provenance_show",
    "description": "Show the provenance chain for an installed agent.",
    "parameters": {
        "type": "object",
        "properties": {
            "installed_ref": {
                "type": "string",
                "description": "Reference (digest or alias) of the installed agent.",
            },
        },
        "required": ["installed_ref"],
        "additionalProperties": False,
    },
}


AGENTPAAS_TRUST_LIST = {
    "name": "agentpaas_trust_list",
    "description": "List trusted publishers.",
    "parameters": {
        "type": "object",
        "properties": {},
        "additionalProperties": False,
    },
}


AGENTPAAS_FORK = {
    "name": "agentpaas_fork",
    "description": "Create a local project copy from an installed agent (safe, no trust decision).",
    "parameters": {
        "type": "object",
        "properties": {
            "installed_ref": {
                "type": "string",
                "description": "Reference (digest or alias) of the installed agent.",
            },
            "target_dir": {
                "type": "string",
                "description": "Target directory for the forked project.",
            },
        },
        "required": ["installed_ref", "target_dir"],
        "additionalProperties": False,
    },
}


AGENTPAAS_CLOUD_WHOAMI = {
    "name": "agentpaas_cloud_whoami",
    "description": "Show the authenticated AgentPaaS Cloud account.",
    "parameters": {"type": "object", "properties": {}, "additionalProperties": False},
}


AGENTPAAS_CLOUD_REGISTRY = {
    "name": "agentpaas_cloud_registry",
    "description": "List tenant cloud assets and the platform MCP catalog; secret values are never returned.",
    "parameters": {"type": "object", "properties": {}, "additionalProperties": False},
}


AGENTPAAS_CLOUD_PUSH = {
    "name": "agentpaas_cloud_push",
    "description": "Admit a signed packed image to AgentPaaS Cloud. Confirmation gates remain in the skill.",
    "parameters": {
        "type": "object",
        "properties": {
            "lock": {"type": "string", "description": "Path to the signed agent.lock file."},
            "digest": {"type": "string", "description": "Optional image digest override."},
            "platform": {"type": "string", "description": "Cloud target platform."},
            "registry_ref": {"type": "string", "description": "Optional existing registry reference."},
            "skip_registry": {"type": "boolean", "description": "Skip image upload and admit only."},
            "image": {"type": "string", "description": "Optional local image reference."},
        },
        "required": ["lock"],
        "additionalProperties": False,
    },
}


AGENTPAAS_CLOUD_DEPLOY = {
    "name": "agentpaas_cloud_deploy",
    "description": "Deploy an admitted image digest in AgentPaaS Cloud.",
    "parameters": {
        "type": "object",
        "properties": {
            "digest": {"type": "string", "description": "Image digest; defaults to latest admitted image."},
            "instance_type": {"type": "string", "description": "Cloud container preset."},
        },
        "required": ["digest"],
        "additionalProperties": False,
    },
}


AGENTPAAS_CLOUD_DEPLOYMENTS = {
    "name": "agentpaas_cloud_deployments",
    "description": "List AgentPaaS Cloud deployments.",
    "parameters": {"type": "object", "properties": {}, "additionalProperties": False},
}


AGENTPAAS_CLOUD_UNDEPLOY = {
    "name": "agentpaas_cloud_undeploy",
    "description": "Remove an AgentPaaS Cloud deployment.",
    "parameters": {
        "type": "object",
        "properties": {"deployment_id": {"type": "string", "description": "Deployment identifier."}},
        "required": ["deployment_id"],
        "additionalProperties": False,
    },
}


AGENTPAAS_CLOUD_INVOKE = {
    "name": "agentpaas_cloud_invoke",
    "description": "Invoke a stored-token AgentPaaS Cloud deployment and wait for its result by default.",
    "parameters": {
        "type": "object",
        "properties": {
            "deployment_id": {"type": "string", "description": "Deployment identifier."},
            "body": {"type": "string", "description": "Optional JSON request body."},
            "wait": {"type": "boolean", "default": True, "description": "Wait for a terminal result."},
            "wait_timeout": {"type": "string", "description": "Optional Go duration, for example 10m or 30s."},
        },
        "required": ["deployment_id"],
        "additionalProperties": False,
    },
}


AGENTPAAS_CLOUD_RESULT = {
    "name": "agentpaas_cloud_result",
    "description": "Fetch the final result object for a Cloud run.",
    "parameters": {
        "type": "object",
        "properties": {"run_id": {"type": "string", "description": "Run identifier."}},
        "required": ["run_id"],
        "additionalProperties": False,
    },
}


AGENTPAAS_CLOUD_LOGS = {
    "name": "agentpaas_cloud_logs",
    "description": "Fetch logs for a Cloud run.",
    "parameters": {
        "type": "object",
        "properties": {"run_id": {"type": "string", "description": "Run identifier."}},
        "required": ["run_id"],
        "additionalProperties": False,
    },
}


AGENTPAAS_CLOUD_USAGE = {
    "name": "agentpaas_cloud_usage",
    "description": "Show AgentPaaS Cloud usage and limits.",
    "parameters": {"type": "object", "properties": {}, "additionalProperties": False},
}


AGENTPAAS_CLOUD_IMAGES = {
    "name": "agentpaas_cloud_images",
    "description": "List admitted AgentPaaS Cloud images.",
    "parameters": {"type": "object", "properties": {}, "additionalProperties": False},
}


AGENTPAAS_CLOUD_EVENTS = {
    "name": "agentpaas_cloud_events",
    "description": "Show lifecycle and audit events for a Cloud run.",
    "parameters": {
        "type": "object",
        "properties": {
            "run_id": {"type": "string", "description": "Cloud run identifier."},
        },
        "required": ["run_id"],
        "additionalProperties": False,
    },
}


AGENTPAAS_CLOUD_AUDIT = {
    "name": "agentpaas_cloud_audit",
    "description": "Query tenant-scoped Cloud audit events by time range and limit.",
    "parameters": {
        "type": "object",
        "properties": {
            "since": {"type": "string", "description": "Earliest event timestamp to include."},
            "until": {"type": "string", "description": "Latest event timestamp to include."},
            "limit": {"type": "integer", "minimum": 1, "description": "Maximum number of events."},
        },
        "additionalProperties": False,
    },
}


AGENTPAAS_CLOUD_AUDIT_EXPORT = {
    "name": "agentpaas_cloud_audit_export",
    "description": "Fetch the audit export for a Cloud run.",
    "parameters": {
        "type": "object",
        "properties": {
            "run_id": {"type": "string", "description": "Cloud run identifier."},
        },
        "required": ["run_id"],
        "additionalProperties": False,
    },
}


AGENTPAAS_CLOUD_METRICS = {
    "name": "agentpaas_cloud_metrics",
    "description": "Show aggregate Cloud run and audit metrics.",
    "parameters": {"type": "object", "properties": {}, "additionalProperties": False},
}


AGENTPAAS_CLOUD_SECRETS_LIST = {
    "name": "agentpaas_cloud_secrets_list",
    "description": "List Cloud secret labels only; values are never returned.",
    "parameters": {"type": "object", "properties": {}, "additionalProperties": False},
}


AGENTPAAS_CLOUD_SECRETS_PUSH = {
    "name": "agentpaas_cloud_secrets_push",
    "description": "Push locally stored secrets by label; never accepts a secret value.",
    "parameters": {
        "type": "object",
        "properties": {
            "names": {
                "type": "array",
                "items": {"type": "string"},
                "minItems": 1,
                "description": "Secret labels whose values are read from the local store.",
            },
        },
        "required": ["names"],
        "additionalProperties": False,
    },
}


AGENTPAAS_CLOUD_LOGIN = {
    "name": "agentpaas_cloud_login",
    "description": "Start AgentPaaS Cloud login, print the URL, and wait for the callback. The CLI does not open a browser by default; open the URL in the same browser as the claim link.",
    "parameters": {"type": "object", "properties": {}, "additionalProperties": False},
}


AGENTPAAS_CLOUD_CRON_LIST = {
    "name": "agentpaas_cloud_cron_list",
    "description": "List cloud cron schedules for the logged-in tenant.",
    "parameters": {"type": "object", "properties": {}, "additionalProperties": False},
}

AGENTPAAS_CLOUD_CRON_SET = {
    "name": "agentpaas_cloud_cron_set",
    "description": "Set or change a cloud deployment cron schedule (enables it). expr: every_5m|every_15m|every_1h.",
    "parameters": {
        "type": "object",
        "properties": {
            "deployment": {"type": "string", "description": "Deployment id or agent name."},
            "expr": {"type": "string", "description": "every_5m, every_15m, or every_1h"},
        },
        "required": ["deployment", "expr"],
        "additionalProperties": False,
    },
}

AGENTPAAS_CLOUD_CRON_DISABLE = {
    "name": "agentpaas_cloud_cron_disable",
    "description": "Disable cloud cron on a deployment without removing the stored schedule.",
    "parameters": {
        "type": "object",
        "properties": {
            "deployment": {"type": "string", "description": "Deployment id or agent name."},
        },
        "required": ["deployment"],
        "additionalProperties": False,
    },
}

AGENTPAAS_CLOUD_CRON_ENABLE = {
    "name": "agentpaas_cloud_cron_enable",
    "description": "Re-enable a previously configured cloud cron schedule.",
    "parameters": {
        "type": "object",
        "properties": {
            "deployment": {"type": "string", "description": "Deployment id or agent name."},
        },
        "required": ["deployment"],
        "additionalProperties": False,
    },
}
