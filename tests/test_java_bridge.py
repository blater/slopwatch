from __future__ import annotations

import unittest
import tempfile
from pathlib import Path

from analyzers.java.adapter import BridgeDecodeError, decode_pmd_report
from analyzers.java.cli import _classpath, _pmd_failure


class JavaBridgeDecoderTest(unittest.TestCase):
  def test_pmd_failure_is_concise_and_rejects_scores(self) -> None:
    stderr = "traceback details\n28 errors occurred while executing PMD.\nmore advice"
    self.assertEqual(
        _pmd_failure(5, stderr),
        "PMD exited with status 5 after 28 analysis errors; no scores were produced",
    )

  def test_explicit_pmd_classpath_supports_managed_or_nonstandard_installations(self) -> None:
    with tempfile.TemporaryDirectory() as temporary:
      library = Path(temporary) / "pmd-java.jar"
      library.write_bytes(b"fixture")
      self.assertEqual(_classpath({"pmd_classpath": [str(library)]}),
                       [str(library)])

  def setUp(self) -> None:
    self.workspace = Path("/workspace")
    self.source = self.workspace / "src/main/java/Example.java"

  def report(self, violations: list[dict[str, object]]) -> dict[str, object]:
    return {
        "pmdVersion": "7.26.0",
        "files": [{"filename": str(self.source), "violations": violations}],
        "processingErrors": [],
        "configurationErrors": [],
    }

  @staticmethod
  def violation(description: str, rule: str = "SlopslapMetrics") -> dict[str, object]:
    return {
        "rule": rule,
        "description": description,
        "beginline": 3,
        "begincolumn": 5,
        "endline": 8,
        "endcolumn": 6,
    }

  def test_decodes_complete_method_and_type_metrics(self) -> None:
    report = self.report([
        self.violation(
            "SLOPSLAP_METRIC_V1|scope=type|kind=class|symbol=Example"
            "|wmc=17|atfd=6|tcc=0.25|fan_out=9"
        ),
        self.violation(
            "SLOPSLAP_METRIC_V1|scope=function|kind=method|symbol=run"
            "|cognitive=16|cyclomatic=11|npath=250"
        ),
        self.violation("nested", "AvoidDeeplyNestedIfStmts"),
    ])
    decoded = decode_pmd_report(
        report, workspace=self.workspace, requested_files=[self.source]
    )
    self.assertEqual(len(decoded["measurements"]), 7)
    values = {
        item["component_id"]: item["value"] for item in decoded["measurements"]
    }
    self.assertEqual(values["cognitive_complexity"], 16)
    self.assertEqual(values["cyclomatic_class_complexity"], 17)
    god = next(
        item for item in decoded["measurements"]
        if item["component_id"] == "god_class"
    )
    self.assertEqual(god["attributes"]["atfd"], 6)
    self.assertEqual(decoded["coverage"][0]["state"], "complete")

  def test_decodes_compact_constructor_metrics(self) -> None:
    decoded = decode_pmd_report(
        self.report([self.violation(
            "SLOPSLAP_METRIC_V1|scope=function|kind=compact_constructor"
            "|symbol=Record|cognitive=3|cyclomatic=4|npath=2"
        )]),
        workspace=self.workspace,
        requested_files=[self.source],
    )
    values = {
        item["component_id"]: item["value"]
        for item in decoded["measurements"]
    }
    self.assertEqual(values, {
        "cognitive_complexity": 3,
        "cyclomatic_method_complexity": 4,
        "npath_complexity": 2,
    })
    self.assertEqual(decoded["unavailable"], [])

  def test_decodes_interface_complexity_and_coupling_without_class_smell(self) -> None:
    decoded = decode_pmd_report(
        self.report([self.violation(
            "SLOPSLAP_METRIC_V1|scope=type|kind=interface|symbol=InputReader"
            "|wmc=9|atfd=1|tcc=unavailable|fan_out=8"
        )]),
        workspace=self.workspace,
        requested_files=[self.source],
    )
    by_component = {
        item["component_id"]: item for item in decoded["measurements"]
    }
    self.assertEqual(set(by_component), {
        "cyclomatic_class_complexity", "coupling_between_objects",
    })
    self.assertEqual(by_component["cyclomatic_class_complexity"]["value"], 9)
    self.assertEqual(by_component["coupling_between_objects"]["value"], 8)
    self.assertTrue(all(
        item["attributes"]["kind"] == "interface"
        for item in by_component.values()
    ))
    self.assertEqual(decoded["unavailable"], [])

  def test_rejects_missing_tcc_for_a_class(self) -> None:
    with self.assertRaisesRegex(BridgeDecodeError, "only for an interface"):
      decode_pmd_report(
          self.report([self.violation(
              "SLOPSLAP_METRIC_V1|scope=type|kind=class|symbol=Example"
              "|wmc=9|atfd=1|tcc=unavailable|fan_out=8"
          )]),
          workspace=self.workspace,
          requested_files=[self.source],
      )

  def test_rejects_wrong_pmd_version_and_unrequested_paths(self) -> None:
    wrong = self.report([])
    wrong["pmdVersion"] = "7.25.0"
    with self.assertRaisesRegex(BridgeDecodeError, "expected PMD"):
      decode_pmd_report(
          wrong, workspace=self.workspace, requested_files=[self.source]
      )
    with self.assertRaisesRegex(BridgeDecodeError, "unrequested"):
      decode_pmd_report(
          self.report([]),
          workspace=self.workspace,
          requested_files=[self.workspace / "Other.java"],
      )

  def test_rejects_unknown_fields(self) -> None:
    with self.assertRaisesRegex(BridgeDecodeError, "unknown"):
      decode_pmd_report(
          self.report([self.violation(
              "SLOPSLAP_METRIC_V1|scope=function|kind=method|symbol=run"
              "|cognitive=1|cyclomatic=1|npath=1|surprise=yes"
          )]),
          workspace=self.workspace,
          requested_files=[self.source],
      )


if __name__ == "__main__":
  unittest.main()
