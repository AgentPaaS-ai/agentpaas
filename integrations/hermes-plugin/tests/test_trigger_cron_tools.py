"""Tests for trigger invoke and cron management plugin tools."""

import json
import sys
import types
import unittest
from pathlib import Path
from unittest import mock

PLUGIN_ROOT = Path(__file__).resolve().parents[1]


def _load_plugin_package():
    """Load the plugin as a package despite the hyphenated directory name."""
    pkg_name = "agentpaas_hermes_plugin"
    cached = sys.modules.get(pkg_name)
    if cached is not None and hasattr(cached, "register"):
        return cached

    import importlib.util

    pkg = types.ModuleType(pkg_name)
    pkg.__path__ = [str(PLUGIN_ROOT)]
    pkg.__package__ = pkg_name
    sys.modules[pkg_name] = pkg

    for mod_name in ("schemas", "tools"):
        full_name = f"{pkg_name}.{mod_name}"
        spec = importlib.util.spec_from_file_location(
            full_name,
            PLUGIN_ROOT / f"{mod_name}.py",
        )
        module = importlib.util.module_from_spec(spec)
        module.__package__ = pkg_name
        sys.modules[full_name] = module
        setattr(pkg, mod_name, module)
        spec.loader.exec_module(module)

    init_spec = importlib.util.spec_from_file_location(
        pkg_name,
        PLUGIN_ROOT / "__init__.py",
        submodule_search_locations=[str(PLUGIN_ROOT)],
    )
    init_mod = importlib.util.module_from_spec(init_spec)
    init_mod.__package__ = pkg_name
    sys.modules[pkg_name] = init_mod
    init_spec.loader.exec_module(init_mod)
    return init_mod


class TestTriggerInvokeTool(unittest.TestCase):
    def setUp(self):
        self.plugin = _load_plugin_package()

    def test_trigger_invoke_basic(self):
        """Mock _run_cli, verify call args."""
        with mock.patch.object(
            self.plugin.tools, "_run_cli",
            return_value={"run_id": "run-123", "status": "RUN_STATUS_RUNNING"}
        ) as m:
            result = json.loads(
                self.plugin.tools.agentpaas_trigger_invoke({"agent_name": "weather-agent"})
            )
            self.assertEqual(result["run_id"], "run-123")
            m.assert_called_once_with(["trigger", "invoke", "weather-agent"])

    def test_trigger_invoke_requires_agent_name(self):
        result = json.loads(self.plugin.tools.agentpaas_trigger_invoke({}))
        self.assertIn("error", result)
        self.assertEqual(result["error_category"], "tool_invocation_failed")
        self.assertIn("agent_name is required", result["error"])

    def test_trigger_invoke_with_payload(self):
        """Mock _run_cli, verify --payload flag is included."""
        with mock.patch.object(
            self.plugin.tools, "_run_cli",
            return_value={"run_id": "run-456", "status": "RUN_STATUS_RUNNING"}
        ) as m:
            result = json.loads(
                self.plugin.tools.agentpaas_trigger_invoke(
                    {"agent_name": "agent", "payload": "/tmp/payload.json"}
                )
            )
            m.assert_called_once_with([
                "trigger", "invoke", "agent",
                "--payload", "/tmp/payload.json",
            ])

    def test_trigger_invoke_with_content_type(self):
        """Mock _run_cli, verify --content-type flag is included."""
        with mock.patch.object(
            self.plugin.tools, "_run_cli",
            return_value={"run_id": "run-789"}
        ) as m:
            result = json.loads(
                self.plugin.tools.agentpaas_trigger_invoke(
                    {"agent_name": "agent", "content_type": "text/plain"}
                )
            )
            m.assert_called_once_with([
                "trigger", "invoke", "agent",
                "--content-type", "text/plain",
            ])


class TestCronAddTool(unittest.TestCase):
    def setUp(self):
        self.plugin = _load_plugin_package()

    def test_cron_add_basic(self):
        """Mock _run_cli, verify call args include --expr."""
        with mock.patch.object(
            self.plugin.tools, "_run_cli",
            return_value={"schedule_id": "abc123", "expr": "*/5 * * * *", "agent_name": "weather"}
        ) as m:
            result = json.loads(
                self.plugin.tools.agentpaas_cron_add(
                    {"agent_name": "weather", "expr": "*/5 * * * *"}
                )
            )
            m.assert_called_once_with([
                "cron", "add", "weather", "--expr", "*/5 * * * *",
            ])

    def test_cron_add_requires_agent_name(self):
        result = json.loads(self.plugin.tools.agentpaas_cron_add({"expr": "*/5 * * * *"}))
        self.assertIn("error", result)
        self.assertIn("agent_name is required", result["error"])

    def test_cron_add_requires_expr(self):
        result = json.loads(self.plugin.tools.agentpaas_cron_add({"agent_name": "weather"}))
        self.assertIn("error", result)
        self.assertIn("expr is required", result["error"])

    def test_cron_add_with_version_and_timezone(self):
        """Mock _run_cli, verify --version and --timezone flags."""
        with mock.patch.object(
            self.plugin.tools, "_run_cli",
            return_value={"schedule_id": "xyz", "expr": "0 0 * * *"}
        ) as m:
            result = json.loads(
                self.plugin.tools.agentpaas_cron_add({
                    "agent_name": "nightly",
                    "expr": "0 0 * * *",
                    "version": "v2",
                    "timezone": "America/New_York",
                })
            )
            m.assert_called_once_with([
                "cron", "add", "nightly", "--expr", "0 0 * * *",
                "--version", "v2", "--timezone", "America/New_York",
            ])


class TestCronListTool(unittest.TestCase):
    def setUp(self):
        self.plugin = _load_plugin_package()

    def test_cron_list_basic(self):
        """Mock _run_cli, verify call args."""
        with mock.patch.object(
            self.plugin.tools, "_run_cli",
            return_value={"schedules": []}
        ) as m:
            result = json.loads(self.plugin.tools.agentpaas_cron_list({}))
            self.assertEqual(result, {"schedules": []})
            m.assert_called_once_with(["cron", "list"])


class TestCronRemoveTool(unittest.TestCase):
    def setUp(self):
        self.plugin = _load_plugin_package()

    def test_cron_remove_basic(self):
        """Mock _run_cli, verify call args include schedule_id."""
        with mock.patch.object(
            self.plugin.tools, "_run_cli",
            return_value={"removed": True}
        ) as m:
            result = json.loads(self.plugin.tools.agentpaas_cron_remove({"schedule_id": "abc123"}))
            self.assertEqual(result, {"removed": True})
            m.assert_called_once_with(["cron", "remove", "abc123"])

    def test_cron_remove_requires_schedule_id(self):
        result = json.loads(self.plugin.tools.agentpaas_cron_remove({}))
        self.assertIn("error", result)
        self.assertIn("schedule_id is required", result["error"])


if __name__ == "__main__":
    unittest.main()
