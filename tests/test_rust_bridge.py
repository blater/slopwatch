from __future__ import annotations

import shutil
import tempfile
import unittest
from pathlib import Path

from slopslap_build import build_rust_adapter, build_structural_analyzer
from slopslap_runtime import AnalyzerProcessAdapter
from slopslap_runtime.protocol import AnalyzerRequest, ProtocolUnit, RequestedComponent


@unittest.skipUnless(shutil.which("go") and shutil.which("cargo"),
                     "Go and Rust toolchains are not installed")
class RustStructuralBridgeTests(unittest.TestCase):
  def test_rust_adapter_uses_the_shared_pmd_metric_contract(self) -> None:
    project = Path(__file__).parent.parent / "analyzers" / "structural"
    with tempfile.TemporaryDirectory() as temporary:
      root = Path(temporary)
      (root / "sample.rs").write_text(
          "struct Peer { value: i32 }\n"
          "struct Service { state: i32, peer: Peer }\n"
          "impl Service {\n"
          "  fn run(&self, other: Peer, ok: bool) -> i32 {\n"
          "    if ok && self.state > 0 { return other.value; }\n"
          "    match self.state { 0 => 0, _ => 1 }\n"
          "  }\n"
          "}\n"
          "#[cfg(test)] mod tests { #[test] fn inline_unit_test() { if true {} } }\n",
          encoding="utf-8",
      )
      host = root / "slopslap-structural"
      helper = root / "slopslap-structural-rust"
      build_structural_analyzer(host, project=project)
      build_rust_adapter(helper, project=project)
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
          root, (ProtocolUnit("rust-unit", "rust", ("sample.rs",)),), components,
      )
      result = AnalyzerProcessAdapter(timeout_seconds=30).run((host,), request)
      included_request = AnalyzerRequest.create(
          root, (ProtocolUnit("rust-unit", "rust", ("sample.rs",)),), components,
          options={"include_tests": True},
      )
      included = AnalyzerProcessAdapter(timeout_seconds=30).run((host,), included_request)

    self.assertEqual(result.records[-1].payload["status"], "success")
    observations = {
        (record.payload["component_id"], record.payload["subject"]["name"]): record.payload
        for record in result.records if record.record_type == "measurement"
    }
    measured_components = {component for component, _ in observations}
    self.assertEqual(measured_components, {
        "cognitive_complexity", "cyclomatic_method_complexity",
        "npath_complexity", "cyclomatic_class_complexity",
        "coupling_between_objects", "god_class",
    })
    self.assertEqual(observations[("cognitive_complexity", "run")]["value"], 3)
    self.assertEqual(observations[("cyclomatic_method_complexity", "run")]["value"], 4)
    self.assertEqual(observations[("npath_complexity", "run")]["value"], 5)
    self.assertEqual(observations[("coupling_between_objects", "Service")]["value"], 1)
    self.assertEqual(
        observations[("cyclomatic_class_complexity", "Service")]["value"], 4,
    )
    coverage = [record.payload for record in result.records
                if record.record_type == "coverage"]
    self.assertEqual(len(coverage), len(components))
    self.assertTrue(all(item["state"] == "complete" for item in coverage))
    default_subjects = {
        record.payload["subject"]["name"] for record in result.records
        if record.record_type == "measurement"
    }
    included_subjects = {
        record.payload["subject"]["name"] for record in included.records
        if record.record_type == "measurement"
    }
    self.assertNotIn("inline_unit_test", default_subjects)
    self.assertIn("inline_unit_test", included_subjects)


if __name__ == "__main__":
  unittest.main()
