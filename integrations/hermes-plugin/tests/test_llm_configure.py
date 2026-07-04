"""Tests for the agentpaas_llm_configure plugin tool."""

import json
import os
import sys
import tempfile
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


class LlmConfigureTests(unittest.TestCase):
    def test_llm_configure_writes_section(self):
        """Temp dir + agent.yaml (no llm), call tool, verify llm section written."""
        plugin = _load_plugin_package()
        with tempfile.TemporaryDirectory() as tmpdir:
            # Write an agent.yaml without an llm section
            agent_yaml = os.path.join(tmpdir, "agent.yaml")
            with open(agent_yaml, "w") as f:
                f.write("name: test-agent\nruntime: python\n")
            result = json.loads(
                plugin.tools.agentpaas_llm_configure({
                    "project_dir": tmpdir,
                    "provider": "openai",
                    "model": "gpt-4o",
                    "credential": "openai-key",
                })
            )
            self.assertTrue(result.get("configured"))
            self.assertEqual(result["provider"], "openai")
            self.assertEqual(result["model"], "gpt-4o")
            self.assertEqual(result["credential"], "openai-key")
            # Verify file was written
            with open(agent_yaml, "r") as f:
                content = f.read()
            self.assertIn("llm:", content)
            self.assertIn("provider: openai", content)
            self.assertIn("model: gpt-4o", content)
            self.assertIn("credential: openai-key", content)

    def test_llm_configure_requires_project_dir(self):
        """Missing project_dir returns error JSON."""
        plugin = _load_plugin_package()
        result = json.loads(plugin.tools.agentpaas_llm_configure({}))
        self.assertIn("error", result)
        self.assertEqual(result["error_category"], "tool_invocation_failed")
        self.assertIn("project_dir", result["error"])

    def test_llm_configure_requires_provider(self):
        """Missing provider returns error JSON."""
        plugin = _load_plugin_package()
        result = json.loads(
            plugin.tools.agentpaas_llm_configure({
                "project_dir": ".",
                "model": "gpt-4o",
                "credential": "openai-key",
            })
        )
        self.assertIn("error", result)
        self.assertEqual(result["error_category"], "tool_invocation_failed")
        self.assertIn("provider", result["error"].lower())

    def test_llm_configure_invalid_provider(self):
        """provider="google" returns error JSON."""
        plugin = _load_plugin_package()
        result = json.loads(
            plugin.tools.agentpaas_llm_configure({
                "project_dir": ".",
                "provider": "google",
                "model": "gpt-4o",
                "credential": "openai-key",
            })
        )
        self.assertIn("error", result)
        self.assertEqual(result["error_category"], "tool_invocation_failed")
        self.assertIn("invalid provider", result["error"])

    def test_llm_configure_requires_model(self):
        """Missing model returns error JSON."""
        plugin = _load_plugin_package()
        result = json.loads(
            plugin.tools.agentpaas_llm_configure({
                "project_dir": ".",
                "provider": "openai",
                "credential": "openai-key",
            })
        )
        self.assertIn("error", result)
        self.assertEqual(result["error_category"], "tool_invocation_failed")
        self.assertIn("model", result["error"].lower())

    def test_llm_configure_requires_credential(self):
        """Missing credential returns error JSON."""
        plugin = _load_plugin_package()
        result = json.loads(
            plugin.tools.agentpaas_llm_configure({
                "project_dir": ".",
                "provider": "openai",
                "model": "gpt-4o",
            })
        )
        self.assertIn("error", result)
        self.assertEqual(result["error_category"], "tool_invocation_failed")
        self.assertIn("credential", result["error"].lower())

    def test_llm_configure_overwrites_existing(self):
        """agent.yaml with old llm section, call tool, verify replaced."""
        plugin = _load_plugin_package()
        with tempfile.TemporaryDirectory() as tmpdir:
            agent_yaml = os.path.join(tmpdir, "agent.yaml")
            with open(agent_yaml, "w") as f:
                f.write("name: test-agent\nruntime: python\nllm:\n  provider: anthropic\n  model: claude-sonnet-4\n  credential: old-key\n")
            result = json.loads(
                plugin.tools.agentpaas_llm_configure({
                    "project_dir": tmpdir,
                    "provider": "openai",
                    "model": "gpt-4o",
                    "credential": "openai-key",
                })
            )
            self.assertTrue(result.get("configured"))
            with open(agent_yaml, "r") as f:
                content = f.read()
            self.assertIn("provider: openai", content)
            self.assertNotIn("provider: anthropic", content)
            self.assertIn("credential: openai-key", content)
            self.assertNotIn("credential: old-key", content)

    def test_llm_configure_replaces_commented_section(self):
        """agent.yaml with '# llm:' commented block, verify replaced."""
        plugin = _load_plugin_package()
        with tempfile.TemporaryDirectory() as tmpdir:
            agent_yaml = os.path.join(tmpdir, "agent.yaml")
            with open(agent_yaml, "w") as f:
                f.write("name: test-agent\nruntime: python\n# llm:\n#   provider: openai\n#   model: gpt-4o\n#   credential: openai-key\nother:\n  key: val\n")
            result = json.loads(
                plugin.tools.agentpaas_llm_configure({
                    "project_dir": tmpdir,
                    "provider": "anthropic",
                    "model": "claude-sonnet-4",
                    "credential": "claude-key",
                })
            )
            self.assertTrue(result.get("configured"))
            with open(agent_yaml, "r") as f:
                content = f.read()
            # Should have the real llm section
            self.assertIn("llm:", content)
            # The real llm: should not start with #
            lines = content.splitlines()
            llm_lines = [l for l in lines if "llm:" in l and not l.strip().startswith("#")]
            self.assertTrue(len(llm_lines) >= 1)
            self.assertIn("provider: anthropic", content)
            self.assertIn("credential: claude-key", content)
            self.assertNotIn("# llm:", content)
            self.assertNotIn("#   provider:", content)
            # The "other:" key should still be there
            self.assertIn("other:", content)

    def test_llm_configure_missing_agent_yaml(self):
        """project_dir without agent.yaml, assert error."""
        plugin = _load_plugin_package()
        with tempfile.TemporaryDirectory() as tmpdir:
            result = json.loads(
                plugin.tools.agentpaas_llm_configure({
                    "project_dir": tmpdir,
                    "provider": "openai",
                    "model": "gpt-4o",
                    "credential": "openai-key",
                })
            )
            self.assertIn("error", result)
            self.assertEqual(result["error_category"], "tool_invocation_failed")
            self.assertIn("agent.yaml", result["error"])

    def test_llm_configure_registered_in_manifest(self):
        """Verify tool name in plugin.yaml provides_tools."""
        manifest_path = PLUGIN_ROOT / "plugin.yaml"
        text = manifest_path.read_text(encoding="utf-8")

        provides = []
        section = None
        for line in text.splitlines():
            stripped = line.strip()
            if not stripped or stripped.startswith("#"):
                continue
            if stripped == "provides_tools:":
                section = "provides_tools"
                continue
            if stripped == "requires_env:":
                section = "requires_env"
                continue
            if stripped.startswith("- ") and section == "provides_tools":
                provides.append(stripped[2:].strip())

        self.assertIn("agentpaas_llm_configure", provides)

    def test_llm_configure_result_never_contains_secret(self):
        """Call with credential name 'openai-key', verify result has no secret-looking values."""
        plugin = _load_plugin_package()
        with tempfile.TemporaryDirectory() as tmpdir:
            agent_yaml = os.path.join(tmpdir, "agent.yaml")
            with open(agent_yaml, "w") as f:
                f.write("name: test-agent\nruntime: python\n")
            result_str = plugin.tools.agentpaas_llm_configure({
                "project_dir": tmpdir,
                "provider": "openai",
                "model": "gpt-4o",
                "credential": "openai-key",
            })
            result = json.loads(result_str)
            # The result should never contain the credential value in its own fields
            self.assertTrue(result.get("configured"))
            # credential is the name only
            self.assertEqual(result["credential"], "openai-key")
            # Result as raw string should not contain any secret-like patterns
            # (just the credential name, which is ok since it's a label)
            self.assertNotIn("secret", result_str.lower().replace("credential", "").replace("openai-key", ""))

    def test_llm_configure_handlers_never_raise(self):
        """Mock file write to raise, verify error JSON returned."""
        plugin = _load_plugin_package()
        with mock.patch("builtins.open", side_effect=IOError("disk full")):
            result = json.loads(
                plugin.tools.agentpaas_llm_configure({
                    "project_dir": ".",
                    "provider": "openai",
                    "model": "gpt-4o",
                    "credential": "openai-key",
                })
            )
            self.assertIn("error", result)
            self.assertEqual(result["error_category"], "tool_invocation_failed")


if __name__ == "__main__":
    unittest.main()
