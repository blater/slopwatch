from __future__ import annotations

import inspect
import unittest
from unittest.mock import patch

import slopslap_mcp


class McpSurfaceTests(unittest.TestCase):
  def test_only_language_neutral_analysis_functions_are_exposed(self) -> None:
    self.assertTrue(callable(slopslap_mcp.rank_files))
    self.assertTrue(callable(slopslap_mcp.score_files))
    self.assertTrue(callable(slopslap_mcp.get_config))
    self.assertFalse(hasattr(slopslap_mcp, "rank_java_files"))
    self.assertFalse(hasattr(slopslap_mcp, "score_java_files"))

  def test_ranking_has_no_default_result_limit(self) -> None:
    self.assertEqual(inspect.signature(slopslap_mcp.rank_files)
                     .parameters["limit"].default, 0)

  def test_get_config_uses_shared_read_only_document(self) -> None:
    expected = {"configuration": {}, "analyzers": [], "components": []}
    with patch("slopslap_mcp.configuration_document", return_value=expected):
      self.assertIs(slopslap_mcp.get_config(), expected)


if __name__ == "__main__":
  unittest.main()
