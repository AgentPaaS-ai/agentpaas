"""Tests for the 5 secret onboarding tools in the AgentPaaS Hermes plugin."""

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


class SecretToolsTests(unittest.TestCase):
    def test_secret_add_calls_cli_with_stdin(self):
        """Mock _run_cli_with_stdin, verify call args and stdin_input."""
        plugin = _load_plugin_package()
        with mock.patch.object(plugin.tools, "_run_cli_with_stdin", return_value={"ok": True}) as m:
            result = plugin.tools.agentpaas_secret_add({"name": "mykey", "value": "secret123"})
            parsed = json.loads(result)
            self.assertEqual(parsed, {"ok": True})
            m.assert_called_once_with(["secret", "add", "mykey"], "secret123")

    def test_secret_add_requires_name(self):
        """Call without name, assert error JSON."""
        plugin = _load_plugin_package()
        result = plugin.tools.agentpaas_secret_add({})
        parsed = json.loads(result)
        self.assertIn("error", parsed)
        self.assertEqual(parsed["error_category"], "tool_invocation_failed")
        self.assertIn("name is required", parsed["error"])

    def test_secret_add_requires_value(self):
        """Call without value, assert error JSON."""
        plugin = _load_plugin_package()
        result = plugin.tools.agentpaas_secret_add({"name": "mykey"})
        parsed = json.loads(result)
        self.assertIn("error", parsed)
        self.assertEqual(parsed["error_category"], "tool_invocation_failed")
        self.assertIn("value is required", parsed["error"])

    def test_secret_add_never_passes_value_in_argv(self):
        """Mock _run_cli_with_stdin, verify value is NOT in cmd_args list."""
        plugin = _load_plugin_package()
        with mock.patch.object(plugin.tools, "_run_cli_with_stdin", return_value={"ok": True}) as m:
            plugin.tools.agentpaas_secret_add({"name": "mykey", "value": "secret123"})
            m.assert_called_once()
            cmd_args = m.call_args[0][0]
            self.assertNotIn("secret123", cmd_args)
            # Value should be the second positional arg (stdin_input), not in cmd_args
            self.assertEqual(m.call_args[0][1], "secret123")

    def test_secret_list_calls_cli(self):
        """Mock _run_cli, verify call args."""
        plugin = _load_plugin_package()
        with mock.patch.object(plugin.tools, "_run_cli", return_value={"secrets": []}) as m:
            result = plugin.tools.agentpaas_secret_list({})
            parsed = json.loads(result)
            self.assertEqual(parsed, {"secrets": []})
            m.assert_called_once_with(["secret", "list"])

    def test_secret_remove_calls_cli(self):
        """Mock _run_cli, verify call args."""
        plugin = _load_plugin_package()
        with mock.patch.object(plugin.tools, "_run_cli", return_value={"removed": True}) as m:
            result = plugin.tools.agentpaas_secret_remove({"name": "mykey"})
            parsed = json.loads(result)
            self.assertEqual(parsed, {"removed": True})
            m.assert_called_once_with(["secret", "remove", "mykey"])

    def test_secret_rotate_calls_cli_with_stdin(self):
        """Mock _run_cli_with_stdin, verify call args and stdin_input."""
        plugin = _load_plugin_package()
        with mock.patch.object(plugin.tools, "_run_cli_with_stdin", return_value={"rotated": True}) as m:
            result = plugin.tools.agentpaas_secret_rotate({"name": "mykey", "value": "newval"})
            parsed = json.loads(result)
            self.assertEqual(parsed, {"rotated": True})
            m.assert_called_once_with(["secret", "rotate", "mykey"], "newval")

    def test_secret_test_calls_cli_with_provider(self):
        """Mock _run_cli, verify call args include --provider."""
        plugin = _load_plugin_package()
        with mock.patch.object(plugin.tools, "_run_cli", return_value={"valid": True}) as m:
            result = plugin.tools.agentpaas_secret_test({"name": "openai-key", "provider": "openai"})
            parsed = json.loads(result)
            self.assertEqual(parsed, {"valid": True})
            m.assert_called_once_with(["secret", "test", "openai-key", "--provider", "openai"])

    def test_secret_test_without_provider(self):
        """Call without provider, assert called with just [secret, test, mykey]."""
        plugin = _load_plugin_package()
        with mock.patch.object(plugin.tools, "_run_cli", return_value={"valid": True}) as m:
            result = plugin.tools.agentpaas_secret_test({"name": "mykey"})
            parsed = json.loads(result)
            self.assertEqual(parsed, {"valid": True})
            m.assert_called_once_with(["secret", "test", "mykey"])

    def test_secret_tools_registered_in_manifest(self):
        """Load plugin.yaml, assert all 5 tool names are in provides_tools."""
        manifest_path = PLUGIN_ROOT / "plugin.yaml"
        text = manifest_path.read_text(encoding="utf-8")

        try:
            import yaml
            manifest = yaml.safe_load(text)
        except ImportError:
            manifest = {}
            section = None
            for line in text.splitlines():
                stripped = line.strip()
                if not stripped or stripped.startswith("#"):
                    continue
                if stripped == "provides_tools:":
                    section = "provides_tools"
                    manifest["provides_tools"] = []
                    continue
                if stripped == "requires_env:":
                    section = "requires_env"
                    continue
                if stripped.startswith("- ") and section == "provides_tools":
                    manifest["provides_tools"].append(stripped[2:].strip())

        provides = manifest.get("provides_tools", [])
        for name in ("agentpaas_secret_add", "agentpaas_secret_list",
                     "agentpaas_secret_remove", "agentpaas_secret_rotate",
                     "agentpaas_secret_test"):
            self.assertIn(name, provides, f"{name} not in provides_tools")

    def test_secret_add_result_never_contains_value(self):
        """Mock _run_cli_with_stdin, verify returned JSON does NOT contain the value."""
        plugin = _load_plugin_package()
        mock_result = {"ok": True, "name": "mykey"}
        with mock.patch.object(plugin.tools, "_run_cli_with_stdin", return_value=mock_result):
            result = plugin.tools.agentpaas_secret_add({"name": "mykey", "value": "secret123"})
            self.assertNotIn("secret123", result)


if __name__ == "__main__":
    unittest.main()
