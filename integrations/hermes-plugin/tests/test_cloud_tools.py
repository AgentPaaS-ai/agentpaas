"""Tests for the Hermes cloud CLI tool surface."""

import json
from pathlib import Path
from types import SimpleNamespace
from unittest import mock

from test_plugin_skeleton import _load_plugin_package


CLOUD_TOOLS = (
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
)


def test_cloud_tools_are_registered_and_callable():
    plugin = _load_plugin_package()
    for name in CLOUD_TOOLS:
        assert name in plugin.schemas.TOOL_NAMES
        assert callable(getattr(plugin.tools, name))


def test_cloud_invoke_defaults_to_wait_without_token_parameter():
    plugin = _load_plugin_package()
    with mock.patch.object(plugin.tools, "_run_cli", return_value={"run_id": "run-1"}) as run:
        result = json.loads(plugin.tools.agentpaas_cloud_invoke({"deployment_id": "dep-1"}))

    assert result == {"run_id": "run-1"}
    run.assert_called_once_with(["cloud", "invoke", "dep-1", "--wait"])
    properties = plugin.schemas.AGENTPAAS_CLOUD_INVOKE["parameters"]["properties"]
    assert "token" not in properties
    assert "api_token" not in properties


def test_cloud_handlers_forward_cli_arguments():
    plugin = _load_plugin_package()
    cases = [
        ("agentpaas_cloud_whoami", {}, ["cloud", "whoami"]),
        ("agentpaas_cloud_registry", {}, ["cloud", "registry"]),
        (
            "agentpaas_cloud_push",
            {"lock": "/tmp/agent.lock", "image": "agentpaas/demo:latest"},
            ["cloud", "push", "--lock", "/tmp/agent.lock", "--image", "agentpaas/demo:latest"],
        ),
        (
            "agentpaas_cloud_deploy",
            {"digest": "latest", "instance_type": "standard-2"},
            ["cloud", "deploy", "latest", "--instance-type", "standard-2"],
        ),
        ("agentpaas_cloud_deployments", {}, ["cloud", "deployments"]),
        (
            "agentpaas_cloud_invoke",
            {"deployment_id": "dep-1", "body": '{"question":"hi"}', "wait": False},
            ["cloud", "invoke", "dep-1", "--body", '{"question":"hi"}'],
        ),
        ("agentpaas_cloud_result", {"run_id": "run-1"}, ["cloud", "result", "run-1"]),
        ("agentpaas_cloud_logs", {"run_id": "run-1"}, ["cloud", "logs", "run-1"]),
        ("agentpaas_cloud_usage", {}, ["cloud", "usage"]),
        ("agentpaas_cloud_images", {}, ["cloud", "images"]),
        ("agentpaas_cloud_events", {"run_id": "run-1"}, ["cloud", "events", "run-1"]),
        (
            "agentpaas_cloud_audit",
            {"since": "2026-08-01", "until": "2026-08-04", "limit": 10},
            ["cloud", "audit", "--since", "2026-08-01", "--until", "2026-08-04", "--limit", "10"],
        ),
        (
            "agentpaas_cloud_audit_export",
            {"run_id": "run-1"},
            ["cloud", "audit", "export", "run-1"],
        ),
        ("agentpaas_cloud_metrics", {}, ["cloud", "metrics"]),
        ("agentpaas_cloud_secrets_list", {}, ["cloud", "secrets", "list"]),
        (
            "agentpaas_cloud_secrets_push",
            {"names": ["openai-key", "anthropic-key"]},
            ["cloud", "secrets", "push", "openai-key", "anthropic-key"],
        ),
        (
            "agentpaas_cloud_cron_list",
            {},
            ["cloud", "cron", "list"],
        ),
        (
            "agentpaas_cloud_cron_set",
            {"deployment": "dep_x", "expr": "every_5m"},
            ["cloud", "cron", "set", "dep_x", "--expr", "every_5m"],
        ),
        (
            "agentpaas_cloud_cron_disable",
            {"deployment": "dep_x"},
            ["cloud", "cron", "disable", "dep_x"],
        ),
        (
            "agentpaas_cloud_cron_enable",
            {"deployment": "dep_x"},
            ["cloud", "cron", "enable", "dep_x"],
        ),
    ]
    # cloud_login intentionally does not call CLI (user_cli_login coaching)


    for tool_name, args, expected_command in cases:
        with mock.patch.object(plugin.tools, "_run_cli", return_value={"ok": True}) as run:
            result = json.loads(getattr(plugin.tools, tool_name)(args))
        assert result == {"ok": True}
        run.assert_called_once_with(expected_command)


def test_cloud_undeploy_missing_yes_does_not_invoke_cli():
    plugin = _load_plugin_package()
    with mock.patch.object(plugin.tools, "_run_cli") as run:
        result = json.loads(
            plugin.tools.agentpaas_cloud_undeploy({"deployment_id": "dep-1"})
        )

    assert result["error_category"] == "tool_invocation_failed"
    assert "agentpaas cloud undeploy" in result["error"]
    assert "--yes" in result["error"]
    run.assert_not_called()


def test_cloud_undeploy_yes_true_does_not_invoke_cli():
    plugin = _load_plugin_package()
    with mock.patch.object(plugin.tools, "_run_cli") as run:
        result = json.loads(
            plugin.tools.agentpaas_cloud_undeploy(
                {"deployment_id": "dep-1", "yes": True}
            )
        )

    assert result["error_category"] == "tool_invocation_failed"
    assert "agentpaas cloud undeploy dep-1 --yes" in result["error"]
    run.assert_not_called()


def test_cloud_secret_push_accepts_labels_only():
    plugin = _load_plugin_package()
    properties = plugin.schemas.AGENTPAAS_CLOUD_SECRETS_PUSH["parameters"]["properties"]
    assert "value" not in properties
    assert "secret_value" not in properties
    assert "token" not in properties
    assert "name" in properties or "names" in properties


def test_cloud_secret_push_rejects_value_parameters():
    plugin = _load_plugin_package()
    with mock.patch.object(plugin.tools, "_run_cli") as run:
        result = json.loads(
            plugin.tools.agentpaas_cloud_secrets_push(
                {"names": ["openai-key"], "value": "must-not-be-accepted"}
            )
        )

    assert result["error_category"] == "tool_invocation_failed"
    assert "value" in result["error"]
    run.assert_not_called()


def test_nonzero_cli_preserves_typed_json_error_from_stdout():
    plugin = _load_plugin_package()
    process = SimpleNamespace(
        returncode=2,
        stdout='{"error":"quota_exceeded","reason":"trial_expired","message":"quota","retry_after_sec":9}\n',
        stderr="",
    )

    result = plugin.tools._parse_cli_result(process)

    assert result["error"] == "quota_exceeded"
    assert result["reason"] == "trial_expired"
    assert result["retry_after_sec"] == 9
    assert result["exit_code"] == 2


def test_cloud_login_has_no_token_parameters():
    plugin = _load_plugin_package()
    properties = plugin.schemas.AGENTPAAS_CLOUD_LOGIN["parameters"]["properties"]
    assert properties == {}


def test_walkthrough_uses_one_invoke_per_path_and_cold_provider_picker():
    skill_text = (plugin_root := plugin_path()).joinpath("SKILL.md").read_text(encoding="utf-8")
    assert "invokes exactly once" in skill_text
    assert "do not also" in skill_text
    assert "OpenRouter" in skill_text
    assert "openrouter-key" in skill_text
    assert "Nous token-exchange" in skill_text
    assert "xAI OAuth" in skill_text
    assert "deepseek" in skill_text.lower()
    assert "Build a Workflow" in skill_text
    assert "agentpaas cloud workflow create" in skill_text
    assert "kind: \"choice\"" in skill_text or 'kind: "choice"' in skill_text


def plugin_path():
    return Path(__file__).resolve().parents[1]
