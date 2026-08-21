from __future__ import annotations

import tempfile
import unittest
from contextlib import redirect_stderr, redirect_stdout
from io import StringIO
from pathlib import Path
from unittest.mock import patch

from slopslap_app.cli import _analysis_table, _config_command, main as cli_main
from slopslap_app.config import (
    CONFIG_NAME, ConfigLocations, discover_config, dump_policy, load_policy,
)
from slopslap_app.orchestrator import AnalysisError, _require_success, discover_sources
from slopslap_app.registry import standard_provider_registry
from slopslap_app.schema import effective_profile_document, schema_document
from slopslap_app.ui import _configuration_path
from slopslap_runtime.protocol import ProtocolRecord


class ConfigurationTests(unittest.TestCase):
  def test_policy_round_trip_and_current_directory_discovery(self) -> None:
    policy = {
        "schema": 1,
        "extends": "slopslap-balanced-v1",
        "languages": {
            "typescript": {
                "components": {"unsafe_type_boundary": {"weight": 12}},
            },
        },
        "waivers": [{
            "id": "known_boundary", "path": "src/**", "language": "typescript",
            "component": "unsafe_type_boundary", "allow_up_to": 2,
            "reason": "bounded migration debt",
        }],
    }
    with tempfile.TemporaryDirectory() as temporary:
      root = Path(temporary)
      config = root / CONFIG_NAME
      config.write_text(dump_policy(policy), encoding="utf-8")
      self.assertEqual(
          discover_config(root, environment={}, home=root / "home"), config.resolve(),
      )
      self.assertEqual(load_policy(config), policy)

  def test_empty_command_prints_help_and_driver_status(self) -> None:
    statuses = (
        {"language": "java", "ready": True},
        {"language": "rust", "ready": False},
        {"language": "typescript", "ready": True},
    )
    output = StringIO()
    with patch("slopslap_app.cli.dependency_statuses", return_value=statuses), \
         redirect_stdout(output):
      self.assertEqual(cli_main([]), 0)
    self.assertIn("analysis drivers:", output.getvalue())
    self.assertIn("java        installed", output.getvalue())
    self.assertIn("rust        not installed", output.getvalue())

  def test_help_flag_prints_driver_status(self) -> None:
    statuses = ({"language": "java", "ready": False},)
    output = StringIO()
    with patch("slopslap_app.cli.dependency_statuses", return_value=statuses), \
         redirect_stdout(output), self.assertRaises(SystemExit) as stopped:
      cli_main(["-h"])
    self.assertEqual(stopped.exception.code, 0)
    self.assertIn("analysis drivers:", output.getvalue())
    self.assertIn("{action,analyze,config,install}", output.getvalue())
    self.assertIn("MCP server: slopslap-mcp", output.getvalue())
    self.assertIn("MCP tools: rank_files, score_files, get_config", output.getvalue())
    self.assertIn("analysis options:", output.getvalue())
    self.assertIn("config edit options:", output.getvalue())
    self.assertIn(
        "--pass-score SCORE", output.getvalue(),
    )
    self.assertIn("Maximum pass score. files whose score is above this", output.getvalue())
    self.assertIn("value are failed.", output.getvalue())
    for option in ("--config", "--languages", "--format", "--limit",
                   "--pass-score", "--timeout", "--port", "--no-browser",
                   "--workflow"):
      self.assertIn(option, output.getvalue())

  def test_target_shorthand_is_language_neutral(self) -> None:
    captured = []
    with patch("slopslap_app.cli._analyze", side_effect=lambda args: captured.append(args) or 0), \
         redirect_stdout(StringIO()):
      self.assertEqual(cli_main([
          "src", "changed.ts", "Changed.java", "--pass-score", "42",
          "--limit", "20",
      ]), 0)
    args = captured[0]
    self.assertEqual(args.command, "analyze")
    self.assertEqual(
        args.targets, [Path("src"), Path("changed.ts"), Path("Changed.java")],
    )
    self.assertEqual(args.pass_score, 42)
    self.assertEqual(args.limit, 20)

  def test_java_specific_cli_options_are_absent(self) -> None:
    for option in ("--java-version", "--ruleset", "--max-score"):
      with self.subTest(option=option), redirect_stdout(StringIO()), \
           patch("sys.stderr", StringIO()), self.assertRaises(SystemExit) as stopped:
        cli_main([option, "value"])
      self.assertEqual(stopped.exception.code, 2)

  def test_analysis_table_has_optional_pass_and_language_neutral_metrics(self) -> None:
    document = {"files": [{
        "rank": 1, "score": 24, "passed": False, "path": "src/example.ts",
        "components": {
            "cognitive_complexity": {
                "subjects": [{"value": 18}, {"value": 4}],
            },
            "cyclomatic_method_complexity": {
                "subjects": [{"value": 12}],
            },
            "deeply_nested_if": {"subjects": []},
        },
    }]}
    table = _analysis_table(document, include_pass=True)
    header, row = table.splitlines()
    self.assertTrue(header.startswith("PASS"))
    self.assertNotIn("LANG", header)
    self.assertIn("COG MAX/#", header)
    self.assertIn("NPATH MAX/#", header)
    self.assertIn("CYCLO TOT/MAX", header)
    self.assertIn("DEEP", header)
    self.assertIn("18/2", row)
    self.assertIn("-", row)

  def test_analyzer_failure_is_an_error_not_a_score_report(self) -> None:
    output = StringIO()
    errors = StringIO()
    with patch("slopslap_app.cli.analyze", side_effect=AnalysisError(
        "java analyzer failed: PMD exited with status 5; no scores were produced",
    )), redirect_stdout(output), redirect_stderr(errors):
      self.assertEqual(cli_main(["analyze", "."]), 2)
    self.assertEqual(output.getvalue(), "")
    self.assertIn("error: java analyzer failed", errors.getvalue())
    self.assertIn("no scores were produced", errors.getvalue())

  def test_failed_analyzer_terminal_aborts_before_scoring(self) -> None:
    records = (
        ProtocolRecord("diagnostic", {
            "severity": "error", "message": "driver could not analyze the sources",
        }),
        ProtocolRecord("terminal", {"status": "failure", "message": "failed"}),
    )
    with self.assertRaisesRegex(
        AnalysisError, "java analyzer failed: driver could not analyze the sources",
    ):
      _require_success(records, "java")

  def test_config_discovery_precedence_and_xdg_warning(self) -> None:
    with tempfile.TemporaryDirectory() as temporary:
      root = Path(temporary)
      current = root / "project"
      home = root / "home"
      xdg = root / "xdg"
      current.mkdir()
      home.mkdir()
      (xdg / "slopslap").mkdir(parents=True)
      local_config = current / CONFIG_NAME
      xdg_config = xdg / "slopslap" / "config.toml"
      home_config = home / CONFIG_NAME
      xdg_config.write_text("schema = 1\n", encoding="utf-8")
      home_config.write_text("schema = 1\n", encoding="utf-8")

      warnings: list[str] = []
      self.assertEqual(
          discover_config(current, environment={"XDG_CONFIG_HOME": str(xdg)},
                          home=home, warn=warnings.append),
          xdg_config.resolve(),
      )
      self.assertEqual(len(warnings), 1)
      self.assertIn(str(home_config), warnings[0])

      local_config.write_text("schema = 1\n", encoding="utf-8")
      warnings.clear()
      self.assertEqual(
          discover_config(current, environment={"XDG_CONFIG_HOME": str(xdg)},
                          home=home, warn=warnings.append),
          local_config.resolve(),
      )
      self.assertEqual(warnings, [])

  def test_config_discovery_falls_back_to_home(self) -> None:
    with tempfile.TemporaryDirectory() as temporary:
      root = Path(temporary)
      current = root / "project"
      home = root / "home"
      current.mkdir()
      home.mkdir()
      home_config = home / CONFIG_NAME
      home_config.write_text("schema = 1\n", encoding="utf-8")
      self.assertEqual(
          discover_config(current, environment={}, home=home), home_config.resolve(),
      )

  def test_config_edit_creates_current_directory_policy_when_none_exists(self) -> None:
    with tempfile.TemporaryDirectory() as temporary:
      workspace = Path(temporary)
      with patch("slopslap_app.ui.Path.cwd", return_value=workspace), \
           patch("slopslap_app.ui.discover_config", return_value=None), \
           redirect_stdout(StringIO()):
        selected = _configuration_path(None)
      self.assertEqual(selected, (workspace / CONFIG_NAME).resolve())
      self.assertEqual(load_policy(selected)["extends"], "slopslap-balanced-v1")

  def test_config_edit_global_creates_xdg_policy_when_no_global_exists(self) -> None:
    with tempfile.TemporaryDirectory() as temporary:
      root = Path(temporary)
      locations = ConfigLocations(
          local=root / "project" / CONFIG_NAME,
          xdg=root / "xdg" / "slopslap" / "config.toml",
          home=root / "home" / CONFIG_NAME,
      )
      with patch("slopslap_app.ui.discover_global_config", return_value=None), \
           patch("slopslap_app.ui.config_locations", return_value=locations), \
           redirect_stdout(StringIO()):
        selected = _configuration_path("global")
      self.assertEqual(selected, locations.xdg)
      self.assertEqual(load_policy(selected)["extends"], "slopslap-balanced-v1")

  def test_config_edit_filename_creates_exact_policy(self) -> None:
    with tempfile.TemporaryDirectory() as temporary:
      current = Path(temporary)
      with patch("slopslap_app.ui.Path.cwd", return_value=current), \
           redirect_stdout(StringIO()):
        selected = _configuration_path("policies/team.toml")
      self.assertEqual(selected, (current / "policies/team.toml").resolve())
      self.assertEqual(load_policy(selected)["extends"], "slopslap-balanced-v1")

  def test_config_edit_routes_to_configuration_ui(self) -> None:
    with patch("slopslap_app.ui.serve", return_value=0) as serve, \
         redirect_stdout(StringIO()):
      self.assertEqual(cli_main(["config", "edit", "global", "--no-browser"]), 0)
    serve.assert_called_once_with("global", 0, open_browser=False)

  def test_action_add_list_set_pass_score_and_remove(self) -> None:
    with tempfile.TemporaryDirectory() as temporary:
      workflow = Path(temporary) / ".github/workflows/slopslap.yml"
      common = ["--workflow", str(workflow)]
      output = StringIO()
      with redirect_stdout(output):
        self.assertEqual(cli_main(["action", "add", *common]), 0)
      content = workflow.read_text(encoding="utf-8")
      self.assertNotIn("pass-score:", content)

      output = StringIO()
      with redirect_stdout(output):
        self.assertEqual(cli_main(["action", "list", *common]), 0)
      self.assertIn("is installed", output.getvalue())

      with redirect_stdout(StringIO()):
        self.assertEqual(cli_main([
            "action", "set-pass-score", "72.5", *common,
        ]), 0)
      self.assertIn(
          'pass-score: "72.5"', workflow.read_text(encoding="utf-8"),
      )

      with redirect_stdout(StringIO()):
        self.assertEqual(cli_main(["action", "remove", *common]), 0)
      self.assertFalse(workflow.exists())

  def test_composite_action_pass_score_is_optional_and_drives_cli_flag(self) -> None:
    action = (Path(__file__).parent.parent / "action.yml").read_text(encoding="utf-8")
    self.assertIn("pass-score:", action)
    self.assertIn("required: false", action)
    self.assertIn('--pass-score "$SLOPSLAP_PASS_SCORE"', action)
    self.assertIn("slopslap install java", action)
    self.assertIn("slopslap install typescript", action)
    self.assertIn("slopslap install rust", action)

  def test_config_command_combines_locations_analyzers_and_components(self) -> None:
    with tempfile.TemporaryDirectory() as temporary:
      root = Path(temporary)
      locations = ConfigLocations(
          local=root / "project" / CONFIG_NAME,
          xdg=root / "xdg" / "slopslap" / "config.toml",
          home=root / "home" / CONFIG_NAME,
      )
      locations.xdg.parent.mkdir(parents=True)
      locations.xdg.write_text("schema = 1\n", encoding="utf-8")
      output = StringIO()
      with patch("slopslap_app.schema.config_locations", return_value=locations), \
           patch("slopslap_app.schema.discover_config", return_value=locations.xdg), \
           patch("slopslap_app.schema.dependency_statuses", return_value=({
               "language": "java", "ready": True, "dependency": "java-driver",
               "version": "1", "installation_method": "maven",
           },)), \
           patch("slopslap_app.schema.catalog_document", return_value={
               "schema": 1, "profile": "profile", "languages": ["java"],
               "analyzers": [{
                   "language": "java", "extensions": [".java"],
                   "dependency": "java-driver", "version": "1",
                   "installation_method": "maven", "execution_method": "python",
                   "source": "source", "entrypoint": "cli.py", "ready_path": "ready",
               }],
               "components": [{
                   "component_id": "metric", "definition_version": "v1",
                   "support": {"java": "supported"}, "defaults": {"enabled": True},
               }],
           }), \
           redirect_stdout(output):
        self.assertEqual(_config_command(), 0)
      self.assertIn("CONFIGURATION", output.getvalue())
      self.assertIn("> global (XDG)", output.getvalue())
      self.assertIn("ANALYZERS", output.getvalue())
      self.assertIn("java-driver@1", output.getvalue())
      self.assertIn("COMPONENTS", output.getvalue())
      self.assertIn("metric@v1", output.getvalue())

  def test_language_override_does_not_leak(self) -> None:
    policy = {"schema": 1, "languages": {"typescript": {"components": {
        "cognitive_complexity": {"weight": 77},
    }}}}
    result = effective_profile_document(policy, ["java", "typescript"])
    self.assertEqual(result["languages"]["typescript"]["components"]
                     ["cognitive_complexity"]["weight"], "77")
    self.assertEqual(result["languages"]["java"]["components"]
                     ["cognitive_complexity"]["weight"], "10")
    self.assertFalse(result["calibrated"])


class RegistryTests(unittest.TestCase):
  def test_registry_and_schema_share_definitions(self) -> None:
    schema = schema_document()
    registry = standard_provider_registry()
    definitions = {(item["component_id"], item["definition_version"])
                   for item in schema["components"]}
    for provider in registry.all_providers():
      self.assertTrue(provider.capabilities)
      for capability in provider.capabilities:
        self.assertIn((capability.component_id, capability.definition_version), definitions)
    self.assertEqual(
        {item.component_id for item in registry.capabilities("rust")},
        {"rust_large_function", "rust_deep_nesting", "rust_unsafe_block",
         "rust_panic_macro"},
    )

  def test_source_discovery_is_language_neutral_and_ignores_build_outputs(self) -> None:
    with tempfile.TemporaryDirectory() as temporary:
      root = Path(temporary)
      (root / "src").mkdir()
      (root / "src" / "A.java").write_text("class A {}", encoding="utf-8")
      (root / "src" / "a.ts").write_text("const a = 1", encoding="utf-8")
      (root / "src" / "lib.rs").write_text("fn a() {}", encoding="utf-8")
      (root / "node_modules").mkdir()
      (root / "node_modules" / "ignored.ts").write_text("", encoding="utf-8")
      self.assertEqual(discover_sources(root), {
          "java": ("src/A.java",), "rust": ("src/lib.rs",),
          "typescript": ("src/a.ts",),
      })

  def test_source_discovery_uses_extensions_without_content_inspection(self) -> None:
    with tempfile.TemporaryDirectory() as temporary:
      root = Path(temporary)
      (root / "looks-like-typescript.java").write_text(
          "export const value: number = 1;", encoding="utf-8",
      )
      (root / "looks-like-java.ts").write_text(
          "public final class Example {}", encoding="utf-8",
      )
      self.assertEqual(discover_sources(root), {
          "java": ("looks-like-typescript.java",),
          "typescript": ("looks-like-java.ts",),
      })


if __name__ == "__main__":
  unittest.main()
