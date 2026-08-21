from __future__ import annotations

import shutil
import tempfile
import unittest
from decimal import Decimal
from pathlib import Path
from unittest.mock import patch

from slopslap_app.orchestrator import analyze
from slopslap_runtime import AnalyzerProcessAdapter
from slopslap_runtime.protocol import AnalyzerRequest, ProtocolUnit, RequestedComponent
from slopslap_build import build_structural_analyzer


@unittest.skipUnless(shutil.which("go"), "Go toolchain is not installed")
class GoStructuralBridgeTests(unittest.TestCase):
  def test_go_host_satisfies_the_python_protocol(self) -> None:
    repository = Path(__file__).parent.parent
    project = repository / "analyzers" / "structural"
    with tempfile.TemporaryDirectory() as temporary:
      root = Path(temporary)
      source = root / "sample.go"
      source.write_text(
          "package sample\n"
          "type Service struct { state int }\n"
          "func (s *Service) Run(ok bool) { if ok && s.state > 0 {} }\n",
          encoding="utf-8",
      )
      binary = root / "slopslap-structural"
      build_structural_analyzer(binary, project=project)
      components = (
          RequestedComponent("cognitive_complexity", "pmd-sonar-v1"),
          RequestedComponent("cyclomatic_method_complexity", "pmd-v1"),
          RequestedComponent("npath_complexity", "pmd-v1"),
          RequestedComponent("deeply_nested_if", "pmd-v1"),
          RequestedComponent("cyclomatic_class_complexity", "pmd-v1"),
          RequestedComponent("coupling_between_objects", "pmd-v1"),
          RequestedComponent("god_class", "pmd-v1"),
      )
      request = AnalyzerRequest.create(
          root, (ProtocolUnit("go-unit", "go", ("sample.go",)),), components,
      )
      result = AnalyzerProcessAdapter(timeout_seconds=30).run((binary,), request)
      self.assertEqual(result.records[-1].payload["status"], "success")
      measured = {record.payload["component_id"] for record in result.records
                  if record.record_type == "measurement"}
      self.assertEqual(measured, {
          "cognitive_complexity", "cyclomatic_method_complexity",
          "npath_complexity", "cyclomatic_class_complexity",
          "coupling_between_objects", "god_class",
      })

      observations = {
          record.payload["component_id"]: record.payload
          for record in result.records
          if record.record_type == "measurement"
      }
      self.assertEqual(observations["cognitive_complexity"]["value"], 2)
      self.assertEqual(observations["cyclomatic_method_complexity"]["value"], 3)
      self.assertEqual(observations["npath_complexity"]["value"], 3)
      self.assertEqual(observations["cyclomatic_class_complexity"]["value"], 3)
      self.assertEqual(observations["coupling_between_objects"]["value"], 0)
      self.assertEqual(observations["god_class"]["attributes"], {
          "atfd": 0, "kind": "struct", "tcc": 0, "wmc": 3,
      })
      coverage = [record.payload for record in result.records
                  if record.record_type == "coverage"]
      self.assertEqual(len(coverage), len(components))
      self.assertTrue(all(item["state"] == "complete" for item in coverage))

  def test_production_sources_remain_below_the_quality_target(self) -> None:
    repository = Path(__file__).parent.parent
    project = repository / "analyzers" / "structural"
    with tempfile.TemporaryDirectory() as temporary:
      binary = Path(temporary) / "slopslap-structural"
      build_structural_analyzer(binary, project=project)
      with patch("slopslap_app.orchestrator.ensure_analyzer",
                 return_value=(str(binary),)):
        outcome = analyze(repository, targets=(project,), languages=("go",))

    offenders = {
        item.path: item.total_score
        for item in outcome.scores.files
        if item.total_score >= Decimal(50)
    }
    self.assertEqual(offenders, {})


if __name__ == "__main__":
  unittest.main()
