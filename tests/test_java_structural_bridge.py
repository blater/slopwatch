from __future__ import annotations

import shutil
import tempfile
import unittest
from pathlib import Path

from slopslap_build import build_java_adapter, build_structural_analyzer
from slopslap_runtime import AnalyzerProcessAdapter
from slopslap_runtime.protocol import AnalyzerRequest, ProtocolUnit, RequestedComponent


@unittest.skipUnless(
    all(shutil.which(program) for program in ("go", "java", "javac", "jar")),
    "Go and JDK toolchains are not installed",
)
class JavaStructuralBridgeTests(unittest.TestCase):
  def test_java_adapter_uses_shared_metrics_and_package_test_filter(self) -> None:
    project = Path(__file__).parent.parent / "analyzers" / "structural"
    with tempfile.TemporaryDirectory() as temporary:
      root = Path(temporary)
      production = root / "src/main/java/example/Service.java"
      production.parent.mkdir(parents=True)
      production.write_text(
          "package example;\n"
          "final class Peer { int value; }\n"
          "final class Service {\n"
          "  private int state; private Peer peer;\n"
          "  int run(boolean ok) {\n"
          "    if (ok && state > 0) { return peer.value; }\n"
          "    return state;\n"
          "  }\n"
          "}\n",
          encoding="utf-8",
      )
      test_source = root / "src/main/java/example/test/ServiceTest.java"
      test_source.parent.mkdir(parents=True)
      test_source.write_text(
          "package example.test;\n"
          "final class ServiceTest { void testRun() { if (true) { return; } } }\n",
          encoding="utf-8",
      )
      host = root / "slopslap-structural"
      helper = root / "slopslap-structural-java.jar"
      build_structural_analyzer(host, project=project)
      build_java_adapter(helper, project=project)
      components = tuple(
          RequestedComponent(component, version)
          for component, version in (
              ("cognitive_complexity", "pmd-sonar-v1"),
              ("cyclomatic_method_complexity", "pmd-v1"),
              ("npath_complexity", "pmd-v1"),
              ("deeply_nested_if", "pmd-v1"),
              ("cyclomatic_class_complexity", "pmd-v1"),
              ("coupling_between_objects", "pmd-v1"),
              ("god_class", "pmd-v1"),
          )
      )
      paths = (
          "src/main/java/example/Service.java",
          "src/main/java/example/test/ServiceTest.java",
      )
      request = AnalyzerRequest.create(
          root, (ProtocolUnit("java-unit", "java", paths),), components,
      )
      included_request = AnalyzerRequest.create(
          root, (ProtocolUnit("java-unit", "java", paths),), components,
          options={"include_tests": True},
      )
      process = AnalyzerProcessAdapter(timeout_seconds=30)
      result = process.run((host,), request)
      included = process.run((host,), included_request)

    self.assertEqual(result.records[-1].payload["status"], "success")
    observations = {
        (record.payload["component_id"], record.payload["subject"]["name"]):
        record.payload
        for record in result.records if record.record_type == "measurement"
    }
    self.assertEqual(observations[("cognitive_complexity", "run")]["value"], 2)
    self.assertEqual(
        observations[("cyclomatic_method_complexity", "run")]["value"], 3,
    )
    self.assertEqual(observations[("npath_complexity", "run")]["value"], 3)
    self.assertEqual(
        observations[("cyclomatic_class_complexity", "Service")]["value"], 3,
    )
    default_subjects = {
        record.payload["subject"]["name"] for record in result.records
        if record.record_type == "measurement"
    }
    included_subjects = {
        record.payload["subject"]["name"] for record in included.records
        if record.record_type == "measurement"
    }
    self.assertNotIn("testRun", default_subjects)
    self.assertIn("testRun", included_subjects)


if __name__ == "__main__":
  unittest.main()
