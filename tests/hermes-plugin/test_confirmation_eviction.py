"""LRU eviction tests for _ConfirmationState confirmation ID cap (R9 / B14E)."""

import unittest

from test_plugin_skeleton import _load_plugin_package


class TestConfirmationIdEviction(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.plugin = _load_plugin_package()
        cls.ConfirmationState = cls.plugin.tools._ConfirmationState

    def test_evicts_oldest_when_over_max(self):
        state = self.ConfirmationState()
        max_ids = self.ConfirmationState.MAX_CONFIRMATION_IDS
        extra = 5
        ids = [f"cf_{i:032x}" for i in range(max_ids + extra)]

        for cid in ids:
            is_replay, should_refuse = state.check_and_add_confirmation(cid)
            self.assertFalse(is_replay, f"first add of {cid} should not be replay")
            self.assertFalse(should_refuse)

        self.assertEqual(len(state._used_confirmation_ids), max_ids)

        for kept in ids[extra:]:
            self.assertIn(
                kept,
                state._used_confirmation_ids,
                f"recent id {kept} should still be tracked",
            )
            is_replay, should_refuse = state.check_and_add_confirmation(kept)
            self.assertTrue(is_replay, f"kept id {kept} should be replay")
            self.assertTrue(should_refuse)

        for evicted in ids[:extra]:
            self.assertNotIn(
                evicted,
                state._used_confirmation_ids,
                f"oldest id {evicted} should have been evicted",
            )
            is_replay, _ = state.check_and_add_confirmation(evicted)
            self.assertFalse(
                is_replay,
                f"evicted id {evicted} should not be treated as replay",
            )


if __name__ == "__main__":
    unittest.main()