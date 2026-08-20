from __future__ import annotations

import unittest
from unittest.mock import patch

import slopscout_mcp


class McpSurfaceTests(unittest.TestCase):
  def test_only_language_neutral_analysis_functions_are_exposed(self) -> None:
    self.assertTrue(callable(slopscout_mcp.rank_files))
    self.assertTrue(callable(slopscout_mcp.score_files))
    self.assertTrue(callable(slopscout_mcp.get_config))
    self.assertFalse(hasattr(slopscout_mcp, "rank_java_files"))
    self.assertFalse(hasattr(slopscout_mcp, "score_java_files"))

  def test_get_config_uses_shared_read_only_document(self) -> None:
    expected = {"configuration": {}, "analyzers": [], "components": []}
    with patch("slopscout_mcp.configuration_document", return_value=expected):
      self.assertIs(slopscout_mcp.get_config(), expected)


if __name__ == "__main__":
  unittest.main()
