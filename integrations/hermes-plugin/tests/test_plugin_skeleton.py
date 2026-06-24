"""Manifest and tool-discovery tests for the AgentPaaS Hermes plugin."""

import importlib.util
import json
import os
import shutil
import subprocess
import sys
import types
import unittest
from pathlib import Path
from unittest import mock

try:
    import yaml
except ImportError:
    yaml = None

PLUGIN_ROOT = Path(__file__).resolve().parents[1]
REPO_ROOT = PLUGIN_ROOT.parents[1]
MANIFEST_PATH = PLUGIN_ROOT / "plugin.yaml"
REPO_BIN = REPO_ROOT / "bin" / "agentpaas"


def _load_plugin_package():
    """Load the plugin as a package despite the hyphenated directory name."""
    pkg_name = "agentpaas_hermes_plugin"
    cached = sys.modules.get(pkg_name)
    if cached is not None and hasattr(cached, "register"):
        return cached

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


def _parse_manifest():
    text = MANIFEST_PATH.read_text(encoding="utf-8")
    if yaml is not None:
        return yaml.safe_load(text)

    data = {
        "name": "agentpaas",
        "version": "0.1.0",
        "provides_tools": [],
        "requires_env": [],
    }
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
            data["provides_tools"].append(stripped[2:].strip())
            continue
        if ":" in stripped and section != "requires_env":
            key, val = stripped.split(":", 1)
            data[key.strip()] = val.strip().strip('"')
    return data


class PluginManifestTests(unittest.TestCase):
    def test_plugin_yaml_exists_and_parses(self):
        self.assertTrue(MANIFEST_PATH.is_file())
        manifest = _parse_manifest()
        self.assertEqual(manifest["name"], "agentpaas")
        self.assertTrue(manifest.get("version"))

    def test_provides_tools_have_schemas(self):
        plugin = _load_plugin_package()
        manifest = _parse_manifest()
        for tool_name in manifest["provides_tools"]:
            with self.subTest(tool=tool_name):
                self.assertIn(tool_name, plugin.schemas.TOOL_NAMES)
                schema = getattr(plugin.schemas, tool_name.upper())
                self.assertEqual(schema["name"], tool_name)

    def test_provides_tools_have_handlers(self):
        plugin = _load_plugin_package()
        manifest = _parse_manifest()
        for tool_name in manifest["provides_tools"]:
            with self.subTest(tool=tool_name):
                handler = getattr(plugin.tools, tool_name)
                self.assertTrue(callable(handler))


class PluginRegistrationTests(unittest.TestCase):
    def test_register_function_exists(self):
        plugin = _load_plugin_package()
        self.assertTrue(callable(plugin.register))

    def test_register_wires_all_tools(self):
        plugin = _load_plugin_package()
        manifest = _parse_manifest()

        class FakeCtx:
            def __init__(self):
                self.tools = []

            def register_tool(self, name, toolset, schema, handler):
                self.tools.append(
                    {"name": name, "toolset": toolset, "schema": schema, "handler": handler}
                )

        ctx = FakeCtx()
        plugin.register(ctx)
        registered_names = [entry["name"] for entry in ctx.tools]
        self.assertEqual(registered_names, manifest["provides_tools"])
        for entry in ctx.tools:
            self.assertEqual(entry["toolset"], "agentpaas")
            self.assertEqual(entry["schema"]["name"], entry["name"])
            self.assertIs(entry["handler"], getattr(plugin.tools, entry["name"]))


class HandlerBehaviorTests(unittest.TestCase):
    def test_handlers_return_json_strings(self):
        plugin = _load_plugin_package()
        mock_result = {"ok": True, "schema_version": "v1"}

        with mock.patch.object(plugin.tools, "_run_cli", return_value=mock_result):
            for tool_name in plugin.schemas.TOOL_NAMES:
                with self.subTest(tool=tool_name):
                    handler = getattr(plugin.tools, tool_name)
                    args = _sample_args(tool_name)
                    result = handler(args)
                    self.assertIsInstance(result, str)
                    parsed = json.loads(result)
                    self.assertIsInstance(parsed, dict)

    def test_handlers_never_raise(self):
        plugin = _load_plugin_package()

        with mock.patch.object(
            plugin.tools, "_run_cli", side_effect=RuntimeError("boom")
        ):
            for tool_name in plugin.schemas.TOOL_NAMES:
                with self.subTest(tool=tool_name):
                    handler = getattr(plugin.tools, tool_name)
                    args = _sample_args(tool_name)
                    result = handler(args)
                    parsed = json.loads(result)
                    self.assertIn("error", parsed)
                    self.assertEqual(parsed["error_category"], "tool_invocation_failed")


class BinaryResolverTests(unittest.TestCase):
    def test_resolve_agentpaas_binary_finds_repo_dev_layout(self):
        plugin = _load_plugin_package()
        self.assertTrue(REPO_BIN.is_file(), f"missing repo binary: {REPO_BIN}")

        integrations_bin = PLUGIN_ROOT.parent / "bin" / "agentpaas"
        integrations_bin.parent.mkdir(parents=True, exist_ok=True)
        shutil.copy2(REPO_BIN, integrations_bin)
        os.chmod(integrations_bin, 0o755)

        env = os.environ.copy()
        env.pop("AGENTPAAS_CLI", None)
        path_without_agentpaas = os.pathsep.join(
            p for p in env.get("PATH", "").split(os.pathsep) if "agentpaas" not in p.lower()
        )

        with mock.patch.dict(os.environ, {"PATH": path_without_agentpaas}, clear=False):
            with mock.patch("shutil.which", return_value=None):
                resolved = plugin.tools._resolve_agentpaas_binary()

        self.assertEqual(os.path.abspath(resolved), os.path.abspath(integrations_bin))


def _sample_args(tool_name):
    samples = {
        "agentpaas_init_project": {"project_dir": ".", "runtime": "python"},
        "agentpaas_reconcile_project": {"project_dir": "."},
        "agentpaas_validate_project": {"project_dir": "."},
        "agentpaas_doctor": {},
        "agentpaas_pack": {"project_dir": "."},
        "agentpaas_run": {"image_or_project": "demo"},
        "agentpaas_stop": {"run_id": "run_test"},
        "agentpaas_logs": {"run_id": "run_test", "tail": 10},
        "agentpaas_status": {"run_id": "run_test"},
        "agentpaas_get_run_timeline": {"run_id": "run_test"},
        "agentpaas_policy_show": {"project_dir": "."},
        "agentpaas_explain_policy_denial": {
            "run_id": "run_test",
            "destination": "https://example.test",
        },
        "agentpaas_recommend_policy_patch": {"destination": "https://example.test"},
        "agentpaas_audit_query": {"run_id": "run_test", "category": "egress"},
        "agentpaas_export_audit": {"output_path": "/tmp/audit.json"},
        "agentpaas_summarize_run": {"run_id": "run_test"},
        "agentpaas_explain_failure": {"run_id": "run_test"},
        "agentpaas_next_action": {"run_id": "run_test"},
    }
    return samples.get(tool_name, {})


if __name__ == "__main__":
    unittest.main()