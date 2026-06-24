"""Contract-parity tests for B13-T02 Hermes plugin operator wrappers.

Verifies that plugin tool handlers mirror the Block 11 Go operator contract.
Run with: python3 -m unittest discover -s integrations/hermes-plugin/tests -v -k Parity
"""

import importlib.util
import json
import re
import sys
import types
import unittest
import warnings
from pathlib import Path
from unittest import mock

PLUGIN_ROOT = Path(__file__).resolve().parents[1]
REPO_ROOT = PLUGIN_ROOT.parents[1]

# Error envelope fields allowed on any response.
_ERROR_ENVELOPE_FIELDS = frozenset({
    "error", "error_category", "exit_code", "raw_output_truncated",
})

# Tool-level error categories emitted by the plugin (not operator contract).
_TOOL_ERROR_CATEGORIES = frozenset({
    "cli_error", "cli_non_json_output", "tool_invocation_failed",
})


def _load_contracts_module(pkg_name="agentpaas_hermes_plugin"):
    """Load contracts.py into the plugin package (and sys.modules)."""
    full_name = f"{pkg_name}.contracts"
    cached_mod = sys.modules.get(full_name)
    if cached_mod is not None:
        return cached_mod

    spec = importlib.util.spec_from_file_location(
        full_name,
        PLUGIN_ROOT / "contracts.py",
    )
    module = importlib.util.module_from_spec(spec)
    module.__package__ = pkg_name
    sys.modules[full_name] = module
    spec.loader.exec_module(module)
    return module


def _load_plugin_package():
    """Load the plugin as a package despite the hyphenated directory name."""
    pkg_name = "agentpaas_hermes_plugin"
    cached = sys.modules.get(pkg_name)
    if cached is not None and hasattr(cached, "register"):
        if not hasattr(cached, "contracts"):
            setattr(cached, "contracts", _load_contracts_module(pkg_name))
        return cached

    pkg = types.ModuleType(pkg_name)
    pkg.__path__ = [str(PLUGIN_ROOT)]
    pkg.__package__ = pkg_name
    sys.modules[pkg_name] = pkg

    for mod_name in ("schemas", "tools", "contracts"):
        full_name = f"{pkg_name}.{mod_name}"
        if mod_name == "contracts":
            module = _load_contracts_module(pkg_name)
        else:
            spec = importlib.util.spec_from_file_location(
                full_name,
                PLUGIN_ROOT / f"{mod_name}.py",
            )
            module = importlib.util.module_from_spec(spec)
            module.__package__ = pkg_name
            sys.modules[full_name] = module
            spec.loader.exec_module(module)
        setattr(pkg, mod_name, module)

    init_spec = importlib.util.spec_from_file_location(
        pkg_name,
        PLUGIN_ROOT / "__init__.py",
        submodule_search_locations=[str(PLUGIN_ROOT)],
    )
    init_mod = importlib.util.module_from_spec(init_spec)
    init_mod.__package__ = pkg_name
    sys.modules[pkg_name] = init_mod
    init_spec.loader.exec_module(init_mod)
    if not hasattr(init_mod, "contracts"):
        setattr(init_mod, "contracts", sys.modules[f"{pkg_name}.contracts"])
    return init_mod


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


def _minimal_field_value(field):
    """Return a minimal valid value for a contracted response field."""
    values = {
        "ready": True,
        "project_dir": "/tmp/project",
        "run_id": "run_test",
        "status": "completed",
        "summary": "Run completed successfully.",
        "error_category": "agent_runtime_exception",
        "root_cause": "Unhandled exception in agent code.",
        "next_action": "fix_code",
        "denied_action": "egress to https://example.test",
        "blocking_rule_id": "egress[2]",
        "rationale": "Policy blocks unknown destinations.",
        "proposed_patch": "--- policy.yaml\n+++ policy.yaml\n",
        "risk_level": "medium",
        "events": [
            {
                "timestamp": "2026-01-01T00:00:00Z",
                "event_type": "run_start",
                "detail": "Run started.",
            }
        ],
        "confirmation": {
            "requires_confirmation": True,
            "confirmation_id": "conf_123",
        },
        "issues": [
            {
                "category": "dependency_conflict",
                "message": "Missing dependency.",
                "next_action": "install_dependency",
            }
        ],
        "runtime": "python",
    }
    return values[field]


_COMPLEX_RESPONSE_FIELDS = frozenset({"issues", "events", "confirmation"})


def _build_minimal_response(subcommand, contracts):
    """Build a minimal valid CLI response for a contracted subcommand."""
    contract = contracts.RESPONSE_CONTRACTS[subcommand]
    resp = {"schema_version": contracts.SCHEMA_VERSION}
    for field in contract["required"]:
        if field == "schema_version":
            continue
        resp[field] = _minimal_field_value(field)
    return resp


def _allowed_response_fields(contract):
    """Fields a success response may contain per the contract."""
    return (
        set(contract["required"])
        | set(contract.get("optional", []))
        | _ERROR_ENVELOPE_FIELDS
    )


def _invoke_handler(plugin, tool_name, cli_response):
    """Run a tool handler with a mocked CLI response; return parsed dict."""
    handler = getattr(plugin.tools, tool_name)
    with mock.patch.object(plugin.tools, "_run_cli", return_value=cli_response):
        result = handler(_sample_args(tool_name))
    return json.loads(result)


def _flag_unknown_evidence_ref_types(refs, evidence_ref_types):
    """Warn when evidence refs use types outside the operator contract."""
    for ref in refs:
        if ref.get("type") not in evidence_ref_types:
            warnings.warn(
                f"contract violation: unknown evidence ref type {ref['type']!r}",
                UserWarning,
            )


class OperatorCoverageTests(unittest.TestCase):
    """Verify every B11 response struct has a corresponding tool wrapper."""

    @classmethod
    def setUpClass(cls):
        cls.plugin = _load_plugin_package()
        cls.contracts = cls.plugin.contracts

    def test_contracted_tools_have_schemas_and_handlers(self):
        for tool_name in self.contracts.TOOL_TO_CONTRACT:
            with self.subTest(tool=tool_name):
                self.assertIn(tool_name, self.plugin.schemas.TOOL_NAMES)
                handler = getattr(self.plugin.tools, tool_name)
                self.assertTrue(callable(handler))

    def test_all_seven_contracted_subcommands_present(self):
        contracted = {entry[1] for entry in self.contracts.TOOL_TO_CONTRACT.values()}
        expected = {
            "validate", "summarize", "explain-failure", "explain-denial",
            "recommend-patch", "timeline", "next-action",
        }
        self.assertEqual(contracted, expected)
        self.assertEqual(len(self.contracts.TOOL_TO_CONTRACT), 7)

    def test_all_tool_names_have_handlers(self):
        for tool_name in self.plugin.schemas.TOOL_NAMES:
            with self.subTest(tool=tool_name):
                handler = getattr(self.plugin.tools, tool_name, None)
                self.assertIsNotNone(handler)
                self.assertTrue(callable(handler))

    def test_no_orphan_handlers(self):
        tool_names = set(self.plugin.schemas.TOOL_NAMES)
        for attr in dir(self.plugin.tools):
            if not attr.startswith("agentpaas_"):
                continue
            with self.subTest(handler=attr):
                self.assertIn(attr, tool_names)


class ResponseFieldParityTests(unittest.TestCase):
    """Verify wrapper responses match the Go operator contract."""

    @classmethod
    def setUpClass(cls):
        cls.plugin = _load_plugin_package()
        cls.contracts = cls.plugin.contracts

    def test_handler_returns_all_required_fields(self):
        for tool_name, (_cli_cmd, contract_key) in self.contracts.TOOL_TO_CONTRACT.items():
            with self.subTest(tool=tool_name):
                cli_response = _build_minimal_response(contract_key, self.contracts)
                parsed = _invoke_handler(self.plugin, tool_name, cli_response)
                contract = self.contracts.RESPONSE_CONTRACTS[contract_key]
                for field in contract["required"]:
                    self.assertIn(
                        field, parsed,
                        f"{tool_name} dropped required field {field!r}",
                    )

    def test_handler_does_not_add_extra_fields(self):
        for tool_name, (_cli_cmd, contract_key) in self.contracts.TOOL_TO_CONTRACT.items():
            with self.subTest(tool=tool_name):
                cli_response = _build_minimal_response(contract_key, self.contracts)
                parsed = _invoke_handler(self.plugin, tool_name, cli_response)
                contract = self.contracts.RESPONSE_CONTRACTS[contract_key]
                allowed = _allowed_response_fields(contract)
                extra = set(parsed.keys()) - allowed
                self.assertEqual(
                    extra, set(),
                    f"{tool_name} added fields outside contract: {sorted(extra)}",
                )

    def test_evidence_refs_have_valid_types(self):
        for tool_name, (_cli_cmd, contract_key) in self.contracts.TOOL_TO_CONTRACT.items():
            contract = self.contracts.RESPONSE_CONTRACTS[contract_key]
            if "evidence_refs" not in contract.get("optional", []):
                continue
            with self.subTest(tool=tool_name):
                cli_response = _build_minimal_response(contract_key, self.contracts)
                cli_response["evidence_refs"] = [
                    {"type": "audit_seq", "ref": "42"},
                    {"type": "run_id", "ref": "run_test"},
                ]
                parsed = _invoke_handler(self.plugin, tool_name, cli_response)
                self.assertIn("evidence_refs", parsed)
                for ref in parsed["evidence_refs"]:
                    self.assertIn(ref["type"], self.contracts.EVIDENCE_REF_TYPES)
                    self.assertTrue(ref["ref"])

    def test_error_category_in_contract_enum(self):
        for tool_name, (_cli_cmd, contract_key) in self.contracts.TOOL_TO_CONTRACT.items():
            contract = self.contracts.RESPONSE_CONTRACTS[contract_key]
            if "error_category" not in contract.get("optional", []):
                continue
            with self.subTest(tool=tool_name):
                cli_response = _build_minimal_response(contract_key, self.contracts)
                cli_response["error_category"] = "policy_denied"
                parsed = _invoke_handler(self.plugin, tool_name, cli_response)
                self.assertIn(parsed["error_category"], self.contracts.ERROR_CATEGORIES)

    def test_next_action_in_contract_enum(self):
        for tool_name, (_cli_cmd, contract_key) in self.contracts.TOOL_TO_CONTRACT.items():
            with self.subTest(tool=tool_name):
                cli_response = _build_minimal_response(contract_key, self.contracts)
                parsed = _invoke_handler(self.plugin, tool_name, cli_response)
                if "next_action" in parsed:
                    self.assertIn(parsed["next_action"], self.contracts.NEXT_ACTIONS)


class EvidenceRefIntegrityTests(unittest.TestCase):
    """Verify wrappers never drop evidence refs or redacted excerpts."""

    @classmethod
    def setUpClass(cls):
        cls.plugin = _load_plugin_package()
        cls.contracts = cls.plugin.contracts

    def test_evidence_refs_preserved_in_handler_output(self):
        refs = [
            {"type": "audit_seq", "ref": "99", "detail": "denial event"},
            {"type": "policy_rule", "ref": "egress[2]"},
        ]
        for tool_name, (_cli_cmd, contract_key) in self.contracts.TOOL_TO_CONTRACT.items():
            contract = self.contracts.RESPONSE_CONTRACTS[contract_key]
            if "evidence_refs" not in contract.get("optional", []):
                continue
            with self.subTest(tool=tool_name):
                cli_response = _build_minimal_response(contract_key, self.contracts)
                cli_response["evidence_refs"] = refs
                parsed = _invoke_handler(self.plugin, tool_name, cli_response)
                self.assertEqual(parsed["evidence_refs"], refs)

    def test_unknown_evidence_ref_types_flagged(self):
        cli_response = _build_minimal_response("explain-failure", self.contracts)
        cli_response["evidence_refs"] = [{"type": "unknown_kind", "ref": "x"}]
        parsed = _invoke_handler(
            self.plugin, "agentpaas_explain_failure", cli_response,
        )
        self.assertIn("evidence_refs", parsed)
        self.assertEqual(parsed["evidence_refs"][0]["type"], "unknown_kind")
        with self.assertWarns(UserWarning) as caught:
            _flag_unknown_evidence_ref_types(
                parsed["evidence_refs"], self.contracts.EVIDENCE_REF_TYPES,
            )
        self.assertIn("unknown_kind", str(caught.warning))

    def test_redacted_excerpts_have_source_and_content(self):
        cli_response = _build_minimal_response("explain-failure", self.contracts)
        cli_response["redacted_excerpts"] = [
            {"source": "agent/main.py", "content": "raise ValueError('[REDACTED]')"},
        ]
        parsed = _invoke_handler(self.plugin, "agentpaas_explain_failure", cli_response)
        self.assertIn("redacted_excerpts", parsed)
        for excerpt in parsed["redacted_excerpts"]:
            self.assertIn("source", excerpt)
            self.assertIn("content", excerpt)
            self.assertTrue(excerpt["source"])
            self.assertTrue(excerpt["content"])


class SchemaVersionParityTests(unittest.TestCase):
    """Verify schema version parity between Python fixtures and Go source."""

    @classmethod
    def setUpClass(cls):
        cls.plugin = _load_plugin_package()
        cls.contracts = cls.plugin.contracts

    def test_schema_version_matches_go_const(self):
        go_text = (REPO_ROOT / "internal" / "operator" / "categories.go").read_text()
        match = re.search(r'SchemaVersion\s*=\s*"([^"]+)"', go_text)
        self.assertIsNotNone(match, "SchemaVersion const not found in categories.go")
        go_schema_version = match.group(1)
        self.assertEqual(self.contracts.SCHEMA_VERSION, go_schema_version)

    def test_minimal_fixtures_include_schema_version(self):
        for contract_key in self.contracts.RESPONSE_CONTRACTS:
            with self.subTest(contract=contract_key):
                resp = _build_minimal_response(contract_key, self.contracts)
                self.assertIn("schema_version", resp)
                self.assertEqual(resp["schema_version"], self.contracts.SCHEMA_VERSION)


class NestedFieldParityTests(unittest.TestCase):
    """Verify nested struct fields match NESTED_CONTRACTS."""

    @classmethod
    def setUpClass(cls):
        cls.plugin = _load_plugin_package()
        cls.contracts = cls.plugin.contracts

    def test_nested_contracts_defined_for_all_complex_fields(self):
        expected = {
            "validation_issue", "timeline_event", "redacted_excerpt",
            "evidence_ref", "confirmation_requirement",
        }
        self.assertEqual(set(self.contracts.NESTED_CONTRACTS.keys()), expected)

    def test_validation_issue_fields(self):
        cli_response = _build_minimal_response("validate", self.contracts)
        cli_response["issues"] = [
            {
                "category": "dependency_conflict",
                "message": "Package not installed.",
                "next_action": "install_dependency",
            }
        ]
        parsed = _invoke_handler(
            self.plugin, "agentpaas_validate_project", cli_response,
        )
        nested = self.contracts.NESTED_CONTRACTS["validation_issue"]
        for issue in parsed["issues"]:
            for field in nested["required"]:
                self.assertIn(field, issue)

    def test_timeline_event_fields(self):
        cli_response = _build_minimal_response("timeline", self.contracts)
        parsed = _invoke_handler(
            self.plugin, "agentpaas_get_run_timeline", cli_response,
        )
        nested = self.contracts.NESTED_CONTRACTS["timeline_event"]
        for event in parsed["events"]:
            for field in nested["required"]:
                self.assertIn(field, event)

    def test_redacted_excerpt_fields(self):
        cli_response = _build_minimal_response("explain-failure", self.contracts)
        cli_response["redacted_excerpts"] = [
            {"source": "agent/main.py", "content": "raise ValueError('[REDACTED]')"},
        ]
        parsed = _invoke_handler(
            self.plugin, "agentpaas_explain_failure", cli_response,
        )
        nested = self.contracts.NESTED_CONTRACTS["redacted_excerpt"]
        for excerpt in parsed["redacted_excerpts"]:
            for field in nested["required"]:
                self.assertIn(field, excerpt)

    def test_evidence_ref_fields(self):
        cli_response = _build_minimal_response("explain-failure", self.contracts)
        cli_response["evidence_refs"] = [
            {"type": "audit_seq", "ref": "42"},
            {"type": "policy_rule", "ref": "egress[2]", "detail": "denial"},
        ]
        parsed = _invoke_handler(
            self.plugin, "agentpaas_explain_failure", cli_response,
        )
        nested = self.contracts.NESTED_CONTRACTS["evidence_ref"]
        for ref in parsed["evidence_refs"]:
            for field in nested["required"]:
                self.assertIn(field, ref)

    def test_confirmation_requirement_fields(self):
        cli_response = _build_minimal_response("recommend-patch", self.contracts)
        parsed = _invoke_handler(
            self.plugin, "agentpaas_recommend_policy_patch", cli_response,
        )
        nested = self.contracts.NESTED_CONTRACTS["confirmation_requirement"]
        for field in nested["required"]:
            self.assertIn(field, parsed["confirmation"])


class TrustBoundaryClassificationTests(unittest.TestCase):
    """Verify every contract field is classified as trusted or untrusted."""

    @classmethod
    def setUpClass(cls):
        cls.plugin = _load_plugin_package()
        cls.contracts = cls.plugin.contracts
        cls.classified = (
            cls.contracts.TRUSTED_CONTROL_FIELDS
            | cls.contracts.UNTRUSTED_EVIDENCE_FIELDS
            | _COMPLEX_RESPONSE_FIELDS
        )

    def test_all_contract_fields_are_classified(self):
        for contract_key, contract in self.contracts.RESPONSE_CONTRACTS.items():
            with self.subTest(contract=contract_key):
                fields = set(contract["required"]) | set(contract.get("optional", []))
                unclassified = fields - self.classified
                self.assertEqual(
                    unclassified, set(),
                    f"unclassified fields in {contract_key}: {sorted(unclassified)}",
                )

    def test_nested_contract_fields_are_classified(self):
        trusted = self.contracts.TRUSTED_CONTROL_FIELDS
        untrusted = self.contracts.UNTRUSTED_EVIDENCE_FIELDS
        for nested_key, nested in self.contracts.NESTED_CONTRACTS.items():
            with self.subTest(nested=nested_key):
                fields = set(nested["required"]) | set(nested.get("optional", []))
                for field in fields:
                    self.assertTrue(
                        field in trusted or field in untrusted,
                        f"{nested_key}.{field} not classified",
                    )

    def test_trusted_and_untrusted_sets_are_disjoint(self):
        overlap = (
            self.contracts.TRUSTED_CONTROL_FIELDS
            & self.contracts.UNTRUSTED_EVIDENCE_FIELDS
        )
        self.assertEqual(overlap, set())

    def test_error_category_is_trusted(self):
        self.assertIn("error_category", self.contracts.TRUSTED_CONTROL_FIELDS)


class ErrorEnvelopeParityTests(unittest.TestCase):
    """Verify error envelope shape from CLI and exception paths."""

    @classmethod
    def setUpClass(cls):
        cls.plugin = _load_plugin_package()
        cls.contracts = cls.plugin.contracts

    def _assert_error_category_valid(self, parsed):
        self.assertIn("error_category", parsed)
        allowed = self.contracts.ERROR_CATEGORIES | _TOOL_ERROR_CATEGORIES
        self.assertIn(parsed["error_category"], allowed)

    def test_cli_error_envelope_includes_error_category(self):
        error_response = {
            "error": "command failed",
            "exit_code": 1,
            "error_category": "cli_error",
        }
        for tool_name in self.contracts.TOOL_TO_CONTRACT:
            with self.subTest(tool=tool_name):
                parsed = _invoke_handler(self.plugin, tool_name, error_response)
                self.assertIn("error", parsed)
                self._assert_error_category_valid(parsed)

    def test_cli_non_json_envelope_includes_error_category(self):
        error_response = {
            "error": "CLI returned non-JSON output (length 42)",
            "raw_output_truncated": "not json",
            "exit_code": 0,
            "error_category": "cli_non_json_output",
        }
        parsed = _invoke_handler(
            self.plugin, "agentpaas_validate_project", error_response,
        )
        self.assertIn("error", parsed)
        self._assert_error_category_valid(parsed)

    def test_tool_invocation_failed_includes_error_category(self):
        handler = self.plugin.tools.agentpaas_validate_project
        with mock.patch.object(
            self.plugin.tools, "_run_cli", side_effect=RuntimeError("boom"),
        ):
            parsed = json.loads(handler({"project_dir": "."}))
        self.assertIn("error", parsed)
        self.assertEqual(parsed["error_category"], "tool_invocation_failed")
        self._assert_error_category_valid(parsed)


if __name__ == "__main__":
    unittest.main()