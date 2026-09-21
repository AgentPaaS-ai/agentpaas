"""P2-18: cloud login coaching must be claim-first (Start free trial)."""

import json
import unittest

from test_plugin_skeleton import PLUGIN_ROOT, _load_plugin_package


class ClaimFirstCloudLoginTests(unittest.TestCase):
    def test_cloud_login_tool_coaches_claim_first_before_cli_login(self):
        plugin = _load_plugin_package()
        result = json.loads(plugin.tools.agentpaas_cloud_login({}))
        self.assertEqual(result["error"], "user_cli_login_required")
        msg = result.get("message", "")
        for want in (
            "https://agentpaas.ai",
            "Start free trial",
            "agentpaas cloud login",
            "agentpaas_cloud_whoami",
        ):
            self.assertIn(want, msg)
        trial_at = msg.lower().find("start free trial")
        login_at = msg.lower().find("agentpaas cloud login")
        self.assertGreaterEqual(trial_at, 0)
        self.assertGreaterEqual(login_at, 0)
        self.assertLess(trial_at, login_at, "claim/trial coaching must precede cloud login")
        self.assertNotIn("Cloudflare", msg)
        self.assertNotIn("cf_", msg)

    def test_skill_md_cloud_login_is_claim_primary(self):
        text = (PLUGIN_ROOT / "SKILL.md").read_text()
        section_at = text.find("### Cloud login")
        self.assertGreaterEqual(section_at, 0, "SKILL.md missing Cloud login section")
        section = text[section_at : section_at + 1500]
        for want in ("https://agentpaas.ai", "Start free trial", "agentpaas cloud login"):
            self.assertIn(want, section)
        trial_at = section.lower().find("start free trial")
        login_at = section.lower().find("agentpaas cloud login")
        self.assertLess(trial_at, login_at, "SKILL.md must coach Start free trial before cloud login")
