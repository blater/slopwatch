#!/usr/bin/env python3
"""Tests for the codehealth command-line interface."""

from __future__ import annotations

import contextlib
import io
import tempfile
import unittest
from pathlib import Path
from unittest import mock

import codehealth


class ParseArgsTest(unittest.TestCase):

  def test_accepts_non_negative_max_score(self) -> None:
    args = codehealth.parse_args(["--max-score", "100"])
    self.assertEqual(args.max_score, 100.0)

  def test_rejects_invalid_max_scores(self) -> None:
    for value in ("-1", "nan", "inf"):
      with self.subTest(value=value), self.assertRaises(SystemExit):
        codehealth.parse_args(["--max-score", value])


class QualityGateTest(unittest.TestCase):

  def run_main(self, score: float, maximum: float) -> tuple[int, str]:
    metric = mock.Mock(spec=codehealth.FileMetrics)
    metric.path = "src/main/java/example/Example.java"
    metric.score = score
    with tempfile.TemporaryDirectory() as directory:
      root = Path(directory)
      source_root = root / "src" / "main" / "java"
      source_root.mkdir(parents=True)
      ruleset = root / "ruleset.xml"
      ruleset.touch()
      args = mock.Mock(
          directory=root,
          source_root=[source_root],
          ruleset=ruleset,
          java_version=None,
          limit=50,
          compact=False,
          explain=False,
          max_score=maximum,
      )
      stderr = io.StringIO()
      with (
          mock.patch.object(codehealth, "parse_args", return_value=args),
          mock.patch.object(codehealth.shutil, "which", return_value="pmd"),
          mock.patch.object(codehealth, "run_pmd", return_value={}),
          mock.patch.object(codehealth, "collect_metrics", return_value=[metric]),
          mock.patch.object(codehealth, "render_table", return_value="results"),
          contextlib.redirect_stdout(io.StringIO()),
          contextlib.redirect_stderr(stderr),
      ):
        status = codehealth.main([])
    return status, stderr.getvalue()

  def test_score_at_maximum_passes(self) -> None:
    status, stderr = self.run_main(score=100.0, maximum=100.0)
    self.assertEqual(status, 0)
    self.assertEqual(stderr, "")

  def test_score_above_maximum_fails(self) -> None:
    status, stderr = self.run_main(score=100.001, maximum=100.0)
    self.assertEqual(status, 3)
    self.assertIn("quality gate failed", stderr)
    self.assertIn("Example.java", stderr)


if __name__ == "__main__":
  unittest.main()
