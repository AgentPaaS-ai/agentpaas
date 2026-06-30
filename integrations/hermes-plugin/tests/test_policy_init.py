"""Tests for the agentpaas_policy_init plugin tool."""

import json
import os
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


class PolicyInitTests(unittest.TestCase):
    def test_policy_init_deny_all(self):
        """template=deny-all, mock _run_cli, assert cmd has --template deny-all."""
        plugin = _load_plugin_package()
        with mock.patch.object(plugin.tools, "_run_cli", return_value={"ok": True}) as mock_cli:
            result = json.loads(
                plugin.tools.agentpaas_policy_init({
                    "project_dir": ".",
                    "template": "deny-all",
                })
            )
            self.assertTrue(result.get("ok"))
            mock_cli.assert_called_once()
            cmd = mock_cli.call_args[0][0]
            self.assertIn("--template", cmd)
            self.assertIn("deny-all", cmd)

    def test_policy_init_allow_llm(self):
        """template=allow-llm, assert cmd has --template allow-llm."""
        plugin = _load_plugin_package()
        with mock.patch.object(plugin.tools, "_run_cli", return_value={"ok": True}) as mock_cli:
            result = json.loads(
                plugin.tools.agentpaas_policy_init({
                    "project_dir": ".",
                    "template": "allow-llm",
                })
            )
            self.assertTrue(result.get("ok"))
            cmd = mock_cli.call_args[0][0]
            self.assertIn("--template", cmd)
            self.assertIn("allow-llm", cmd)

    def test_policy_init_force(self):
        """force=True, assert --force in cmd."""
        plugin = _load_plugin_package()
        with mock.patch.object(plugin.tools, "_run_cli", return_value={"ok": True}) as mock_cli:
            result = json.loads(
                plugin.tools.agentpaas_policy_init({
                    "project_dir": ".",
                    "template": "deny-all",
                    "force": True,
                })
            )
            self.assertTrue(result.get("ok"))
            cmd = mock_cli.call_args[0][0]
            self.assertIn("--force", cmd)

    def test_policy_init_no_force(self):
        """force=False (default), assert --force NOT in cmd."""
        plugin = _load_plugin_package()
        with mock.patch.object(plugin.tools, "_run_cli", return_value={"ok": True}) as mock_cli:
            result = json.loads(
                plugin.tools.agentpaas_policy_init({
                    "project_dir": ".",
                    "template": "deny-all",
                })
            )
            self.assertTrue(result.get("ok"))
            cmd = mock_cli.call_args[0][0]
            self.assertNotIn("--force", cmd)

    def test_policy_init_default_project(self):
        """project_dir='.' uses resolved path."""
        plugin = _load_plugin_package()
        with mock.patch.object(plugin.tools, "_run_cli", return_value={"ok": True}) as mock_cli:
            with mock.patch.object(
                plugin.tools, "_validate_project_path",
                return_value=(True, os.path.abspath("."), None),
            ):
                result = json.loads(
                    plugin.tools.agentpaas_policy_init({"project_dir": "."})
                )
                self.assertTrue(result.get("ok"))
                cmd = mock_cli.call_args[0][0]
                # resolved path is the second positional arg after "policy init"
                resolved_arg = cmd[2]
                self.assertEqual(resolved_arg, os.path.abspath("."))

    def test_policy_init_invalid_path(self):
        """project_dir='/nonexistent' returns error."""
        plugin = _load_plugin_package()
        result = json.loads(
            plugin.tools.agentpaas_policy_init({"project_dir": "/nonexistent"})
        )
        self.assertIn("error", result)

    def test_policy_init_invalid_template(self):
        """template='bogus', mock _run_cli returns error (CLI rejects unknown template)."""
        plugin = _load_plugin_package()
        cli_error = {
            "error": "invalid template 'bogus': must be deny-all, allow-http, allow-llm, or allow-mcp",
            "error_category": "cli_error",
        }
        with mock.patch.object(plugin.tools, "_run_cli", return_value=cli_error) as mock_cli:
            result = json.loads(
                plugin.tools.agentpaas_policy_init({
                    "project_dir": ".",
                    "template": "bogus",
                })
            )
            self.assertIn("error", result)
            self.assertEqual(result["error_category"], "cli_error")
            self.assertIn("invalid template", result["error"])


if __name__ == "__main__":
    unittest.main()
