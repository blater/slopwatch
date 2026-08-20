#!/usr/bin/env python3
"""Tests for the slopscout command-line interface."""

from __future__ import annotations

import contextlib
import io
import json
import tempfile
import unittest
from pathlib import Path
from unittest import mock

import slopscout
import slopscout_mcp


def report_with_deep_ifs(files: dict[str, int]) -> dict[str, object]:
  reports = []
  for path, count in files.items():
    reports.append({
        "filename": path,
        "violations": [
            {
                "rule": "AvoidDeeplyNestedIfStmts",
                "description": "Avoid deeply nested if statements",
            }
            for _ in range(count)
        ],
    })
  return {"files": reports}


class ParseArgsTest(unittest.TestCase):

  def test_text_is_the_default_format(self) -> None:
    args = slopscout.parse_args([])
    self.assertEqual(args.output_format, "text")

  def test_accepts_non_negative_max_score(self) -> None:
    args = slopscout.parse_args(["--max-score", "100"])
    self.assertEqual(args.max_score, 100.0)

  def test_rejects_invalid_max_scores(self) -> None:
    for value in ("-1", "nan", "inf"):
      with self.subTest(value=value), self.assertRaises(SystemExit), \
          contextlib.redirect_stderr(io.StringIO()):
        slopscout.parse_args(["--max-score", value])

  def test_explain_is_rejected_with_json(self) -> None:
    with self.assertRaises(SystemExit), \
        contextlib.redirect_stderr(io.StringIO()):
      slopscout.parse_args(["--format", "json", "--explain"])

  def test_accepts_multiple_directories_and_exact_files(self) -> None:
    args = slopscout.parse_args([
        "package/one", "package/two",
        "--file", "Changed.java", "--file", "Other.java",
    ])
    self.assertEqual(args.directories, [Path("package/one"), Path("package/two")])
    self.assertEqual(args.files, [Path("Changed.java"), Path("Other.java")])


class ActionManagementTest(unittest.TestCase):

  def setUp(self) -> None:
    self.temporary = tempfile.TemporaryDirectory()
    self.workflow = Path(self.temporary.name) / "workflows" / "slopscout.yml"

  def tearDown(self) -> None:
    self.temporary.cleanup()

  def run_action(self, *arguments: str) -> tuple[int, str, str]:
    stdout = io.StringIO()
    stderr = io.StringIO()
    with contextlib.redirect_stdout(stdout), contextlib.redirect_stderr(stderr):
      status = slopscout.main([
          "action", *arguments, "--workflow", str(self.workflow),
      ])
    return status, stdout.getvalue(), stderr.getvalue()

  def test_add_list_update_and_remove_action(self) -> None:
    status, stdout, stderr = self.run_action("add", "100")
    self.assertEqual((status, stderr), (0, ""))
    self.assertIn("max score: 100", stdout)
    self.assertIn('max-score: "100"', self.workflow.read_text(encoding="utf-8"))

    status, stdout, stderr = self.run_action("list")
    self.assertEqual((status, stderr), (0, ""))
    self.assertIn("max score: 100", stdout)

    status, stdout, stderr = self.run_action("set-max-score", "72.5")
    self.assertEqual((status, stderr), (0, ""))
    self.assertIn("72.5", stdout)
    self.assertIn('max-score: "72.5"', self.workflow.read_text(encoding="utf-8"))

    status, stdout, stderr = self.run_action("remove")
    self.assertEqual((status, stderr), (0, ""))
    self.assertIn("Removed", stdout)
    self.assertFalse(self.workflow.exists())

  def test_add_does_not_overwrite_existing_workflow(self) -> None:
    self.workflow.parent.mkdir(parents=True)
    self.workflow.write_text("name: Existing\n", encoding="utf-8")
    status, _, stderr = self.run_action("add", "100")
    self.assertEqual(status, 2)
    self.assertIn("refusing to overwrite", stderr)

  def test_remove_does_not_modify_unmanaged_workflow(self) -> None:
    self.workflow.parent.mkdir(parents=True)
    self.workflow.write_text("name: Existing\n", encoding="utf-8")
    status, _, stderr = self.run_action("remove")
    self.assertEqual(status, 2)
    self.assertIn("refusing to modify", stderr)


class ScoreContributionTest(unittest.TestCase):

  def test_contributions_are_the_single_source_of_the_total_score(self) -> None:
    metrics = slopscout.FileMetrics(
        path="Example.java",
        cognitive_methods=[slopscout.COGNITIVE_THRESHOLD],
        cyclomatic_methods=[slopscout.CYCLOMATIC_THRESHOLD],
        cyclomatic_class=slopscout.CYCLOMATIC_CLASS_THRESHOLD,
        npath_methods=[slopscout.NPATH_THRESHOLD],
        deep_ifs=1,
        coupling=slopscout.COUPLING_THRESHOLD,
        god_atfd=slopscout.GOD_ATFD_THRESHOLD,
        god_tcc=slopscout.GOD_TCC_THRESHOLD,
    )
    self.assertEqual(metrics.score_contributions, {
        "cognitive_complexity": 10.0,
        "npath_complexity": 8.0,
        "cyclomatic_complexity": 10.0,
        "deeply_nested_ifs": 6.0,
        "coupling": 10.0,
        "god_class": 46.0,
    })
    self.assertEqual(metrics.score, 90.0)


class AnalysisScopeTest(unittest.TestCase):

  def setUp(self) -> None:
    self.temporary = tempfile.TemporaryDirectory()
    self.root = Path(self.temporary.name)
    (self.root / ".git").mkdir()
    self.ruleset = self.root / "ruleset.xml"
    self.ruleset.write_text("<ruleset/>\n", encoding="utf-8")

  def tearDown(self) -> None:
    self.temporary.cleanup()

  def java_file(self, relative_path: str) -> Path:
    source_file = self.root / relative_path
    source_file.parent.mkdir(parents=True, exist_ok=True)
    source_file.write_text("class Example {}\n", encoding="utf-8")
    return source_file

  def test_package_directory_remains_narrow(self) -> None:
    (self.root / "pom.xml").write_text(
        "<project><properties><maven.compiler.release>17"
        "</maven.compiler.release></properties></project>\n",
        encoding="utf-8",
    )
    selected = self.java_file("src/main/java/one/Selected.java")
    self.java_file("src/main/java/two/Sibling.java")
    package = selected.parent
    with mock.patch.object(slopscout, "run_pmd", return_value={"files": []}) \
        as run_pmd:
      analysis = slopscout.analyze_java(
          directories=[package], ruleset=self.ruleset, pmd="pmd"
      )
    self.assertEqual([item.path for item in analysis.metrics], [
        "src/main/java/one/Selected.java"
    ])
    self.assertEqual(run_pmd.call_args.args[3], [package.resolve()])
    self.assertEqual(run_pmd.call_args.args[4], "java-17")

  def test_project_directory_expands_to_production_sources_only(self) -> None:
    production = self.java_file("src/main/java/example/Production.java")
    self.java_file("src/test/java/example/TestOnly.java")
    with mock.patch.object(slopscout, "run_pmd", return_value={"files": []}):
      analysis = slopscout.analyze_java(
          directories=[self.root], ruleset=self.ruleset, pmd="pmd"
      )
    self.assertEqual([item.path for item in analysis.metrics], [
        production.relative_to(self.root).as_posix()
    ])

  def test_modules_are_inferred_and_analyzed_separately(self) -> None:
    first = self.java_file("first/src/main/java/a/First.java")
    second = self.java_file("second/src/main/java/b/Second.java")
    (self.root / "first" / "pom.xml").write_text(
        "<project><properties><maven.compiler.release>17"
        "</maven.compiler.release></properties></project>\n",
        encoding="utf-8",
    )
    (self.root / "second" / "build.gradle.kts").write_text(
        "java { toolchain { languageVersion.set(JavaLanguageVersion.of(21)) } }\n",
        encoding="utf-8",
    )
    with mock.patch.object(slopscout, "run_pmd", return_value={"files": []}) \
        as run_pmd:
      analysis = slopscout.analyze_java(
          directories=[first.parent],
          files=[second],
          ruleset=self.ruleset,
          pmd="pmd",
      )
    self.assertEqual(len(run_pmd.call_args_list), 2)
    versions = {call.args[4] for call in run_pmd.call_args_list}
    self.assertEqual(versions, {"java-17", "java-21"})
    self.assertEqual({item.path for item in analysis.metrics}, {
        "first/src/main/java/a/First.java",
        "second/src/main/java/b/Second.java",
    })

  def test_modules_with_the_same_java_version_share_one_pmd_run(self) -> None:
    first = self.java_file("first/src/main/java/a/First.java")
    second = self.java_file("second/src/main/java/b/Second.java")
    for module in ("first", "second"):
      (self.root / module / "pom.xml").write_text(
          "<project><properties><maven.compiler.release>17"
          "</maven.compiler.release></properties></project>\n",
          encoding="utf-8",
      )
    with mock.patch.object(slopscout, "run_pmd", return_value={"files": []}) \
        as run_pmd:
      analysis = slopscout.analyze_java(
          directories=[first.parent, second.parent],
          ruleset=self.ruleset,
          pmd="pmd",
      )
    run_pmd.assert_called_once()
    self.assertEqual(set(run_pmd.call_args.args[3]), {
        first.parent.resolve(), second.parent.resolve(),
    })
    self.assertEqual(run_pmd.call_args.args[4], "java-17")
    self.assertEqual(len(analysis.modules), 2)

  def test_no_scope_defaults_to_current_directory(self) -> None:
    source = self.java_file("src/main/java/example/Default.java")
    with mock.patch.object(slopscout, "run_pmd", return_value={"files": []}):
      analysis = slopscout.analyze_java(
          ruleset=self.ruleset, pmd="pmd", base=self.root
      )
    self.assertEqual(analysis.directories, [self.root.resolve()])
    self.assertEqual(analysis.metrics[0].path, source.relative_to(self.root).as_posix())


class McpToolTest(unittest.TestCase):

  def test_score_exact_files_preserves_request_order_and_global_rank(self) -> None:
    root = Path("/workspace")
    low = slopscout.FileMetrics(path="Low.java", deep_ifs=1)
    high = slopscout.FileMetrics(path="High.java", deep_ifs=3)
    analysis = slopscout.AnalysisResult(
        workspace=root,
        directories=[],
        requested_files=[root / "Low.java", root / "High.java"],
        modules=[],
        metrics=[high, low],
    )
    with mock.patch.object(slopscout_mcp, "analyze_java", return_value=analysis):
      document = slopscout_mcp.score_java_files(["Low.java", "High.java"])
    self.assertEqual(
        [(item["path"], item["rank"]) for item in document["files"]],
        [("Low.java", 2), ("High.java", 1)],
    )
    self.assertFalse(document["truncated"])

  def test_rank_defaults_to_current_directory_and_validates_limit(self) -> None:
    analysis = slopscout.AnalysisResult(
        workspace=Path("/workspace"), directories=[], requested_files=[],
        modules=[], metrics=[],
    )
    with mock.patch.object(slopscout_mcp, "analyze_java", return_value=analysis) \
        as analyze:
      slopscout_mcp.rank_java_files()
    self.assertEqual(list(analyze.call_args.kwargs["directories"]), [])
    with self.assertRaisesRegex(ValueError, "non-negative"):
      slopscout_mcp.rank_java_files(limit=-1)


class CommandTest(unittest.TestCase):

  def setUp(self) -> None:
    self.temporary = tempfile.TemporaryDirectory()
    self.root = Path(self.temporary.name)
    self.source_root = self.root / "src" / "main" / "java" / "example"
    self.source_root.mkdir(parents=True)
    (self.source_root / "Example.java").write_text(
        "package example; class Example {}\n", encoding="utf-8"
    )
    self.ruleset = self.root / "ruleset.xml"
    self.ruleset.write_text("<ruleset/>\n", encoding="utf-8")
    self.example_path = "src/main/java/example/Example.java"

  def tearDown(self) -> None:
    self.temporary.cleanup()

  def run_command(
      self, report: dict[str, object], *arguments: str,
  ) -> tuple[int, str, str]:
    stdout = io.StringIO()
    stderr = io.StringIO()
    argv = [str(self.root), "--ruleset", str(self.ruleset), *arguments]
    with (
        mock.patch.object(slopscout.shutil, "which", return_value="pmd"),
        mock.patch.object(slopscout, "run_pmd", return_value=report),
        contextlib.redirect_stdout(stdout),
        contextlib.redirect_stderr(stderr),
    ):
      status = slopscout.main(argv)
    return status, stdout.getvalue(), stderr.getvalue()

  def test_text_output_remains_default(self) -> None:
    status, stdout, stderr = self.run_command(
        report_with_deep_ifs({self.example_path: 1})
    )
    self.assertEqual(status, 0)
    self.assertTrue(stdout.startswith("#|score|"))
    self.assertIn(self.example_path, stdout)
    self.assertEqual(stderr, "")

  def test_json_output_is_structured_and_includes_clean_files(self) -> None:
    (self.source_root / "Clean.java").write_text(
        "package example; class Clean {}\n", encoding="utf-8"
    )
    status, stdout, stderr = self.run_command(
        report_with_deep_ifs({self.example_path: 2}),
        "--format", "json", "--limit", "0",
    )
    document = json.loads(stdout)
    self.assertEqual(status, 0)
    self.assertEqual(stderr, "")
    self.assertEqual(document["schema_version"], 1)
    self.assertEqual(document["total_files"], 2)
    self.assertFalse(document["truncated"])
    self.assertEqual(document["files"][0]["score"], 12.0)
    self.assertEqual(
        sum(document["files"][0]["contributions"].values()), 12.0
    )
    self.assertEqual(
        document["files"][0]["contributions"]["deeply_nested_ifs"], 12.0
    )
    self.assertEqual(
        document["files"][0]["metrics"]["deeply_nested_ifs"], 2
    )
    self.assertEqual(document["files"][1]["score"], 0.0)
    self.assertEqual(document["summary"], {
        "target_score": 100.0,
        "files_analyzed": 2,
        "files_above_target": 0,
        "highest_score": 12.0,
        "highest_path": self.example_path,
    })

  def test_json_summary_identifies_work_above_the_target(self) -> None:
    status, stdout, stderr = self.run_command(
        report_with_deep_ifs({self.example_path: 17}),
        "--format", "json", "--limit", "0",
    )
    document = json.loads(stdout)
    self.assertEqual(status, 0)
    self.assertEqual(stderr, "")
    self.assertEqual(document["summary"]["files_above_target"], 1)
    self.assertEqual(document["summary"]["highest_score"], 102.0)
    self.assertEqual(document["summary"]["highest_path"], self.example_path)

  def test_absolute_maximum_is_inclusive(self) -> None:
    report = report_with_deep_ifs({self.example_path: 1})
    passing, _, _ = self.run_command(report, "--max-score", "6")
    failing, _, stderr = self.run_command(report, "--max-score", "5.9")
    self.assertEqual(passing, 0)
    self.assertEqual(failing, 3)
    self.assertIn("quality gate failed", stderr)

  def test_json_reports_maximum_failure_without_mixed_stderr(self) -> None:
    status, stdout, stderr = self.run_command(
        report_with_deep_ifs({self.example_path: 2}),
        "--max-score", "6", "--format", "json",
    )
    document = json.loads(stdout)
    self.assertEqual(status, 3)
    self.assertEqual(stderr, "")
    self.assertFalse(document["gate"]["passed"])
    self.assertEqual(
        document["gate"]["max_score_failures"][0]["path"],
        self.example_path,
    )

  def test_limit_does_not_limit_gate_evaluation(self) -> None:
    second_path = "src/main/java/example/Second.java"
    (self.source_root / "Second.java").write_text(
        "package example; class Second {}\n", encoding="utf-8"
    )
    status, stdout, stderr = self.run_command(
        report_with_deep_ifs({self.example_path: 2, second_path: 1}),
        "--max-score", "5", "--format", "json", "--limit", "1",
    )
    document = json.loads(stdout)
    self.assertEqual(status, 3)
    self.assertEqual(stderr, "")
    self.assertEqual(document["returned_files"], 1)
    self.assertTrue(document["truncated"])
    self.assertEqual(len(document["gate"]["max_score_failures"]), 2)


if __name__ == "__main__":
  unittest.main()
