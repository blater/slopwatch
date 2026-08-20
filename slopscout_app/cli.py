"""Slopscout command-line interface."""

from __future__ import annotations

import argparse
import json
import math
import sys
from pathlib import Path
from typing import Any

from slopscout_core import CoreError
from slopscout_runtime.errors import RuntimeError as AnalyzerRuntimeError

from .config import ConfigLoadError, discover_config, load_policy
from .dependencies import analyzer_dependencies, dependency_statuses, install_analyzer
from .orchestrator import AnalysisError, analyze
from .report import report_document
from .schema import configuration_document


COMMANDS = frozenset({"action", "analyze", "config", "install"})


def _json(document: Any) -> None:
  print(json.dumps(document, indent=2, sort_keys=True))


def _languages(value: str) -> list[str]:
  result = list(dict.fromkeys(item.strip() for item in value.split(",") if item.strip()))
  if not result:
    raise argparse.ArgumentTypeError("at least one language is required")
  return result


def _non_negative_score(value: str) -> float:
  try:
    score = float(value)
  except ValueError as error:
    raise argparse.ArgumentTypeError("must be a finite non-negative number") from error
  if not math.isfinite(score) or score < 0:
    raise argparse.ArgumentTypeError("must be a finite non-negative number")
  return score


def _non_negative_integer(value: str) -> int:
  try:
    number = int(value)
  except ValueError as error:
    raise argparse.ArgumentTypeError("must be a non-negative integer") from error
  if number < 0:
    raise argparse.ArgumentTypeError("must be a non-negative integer")
  return number


def _policy(configured: Path | None) -> tuple[Path | None, dict[str, Any]]:
  path = configured or discover_config(
      Path.cwd(), warn=lambda message: print(f"warning: {message}", file=sys.stderr),
  )
  return path, dict(load_policy(path))


def _add_analysis_options(parser: Any) -> None:
  parser.add_argument("--config", type=Path, help="use an exact configuration file")
  parser.add_argument(
      "--languages", type=_languages,
      help="analyze only a comma-separated list of languages",
  )
  parser.add_argument(
      "--format", choices=("text", "json"), default="text",
      help="select text or JSON output (default: text)",
  )
  parser.add_argument(
      "--limit", type=_non_negative_integer, default=50,
      help="maximum results to return; 0 returns all (default: 50)",
  )
  parser.add_argument(
      "--pass-score", metavar="SCORE", type=_non_negative_score,
      help="Maximum pass score. files whose score is above this value are failed.",
  )
  parser.add_argument(
      "--timeout", type=float, default=120,
      help="analyzer timeout in seconds (default: 120)",
  )


def _add_config_edit_options(parser: Any) -> None:
  parser.add_argument(
      "--port", type=int, default=0,
      help="configuration editor port; 0 selects an available port",
  )
  parser.add_argument(
      "--no-browser", action="store_true",
      help="start the configuration editor without opening a browser",
  )


def _add_action_options(parser: Any) -> None:
  parser.add_argument(
      "--workflow", type=Path, default=Path(".github/workflows/slopscout.yml"),
      help="workflow path (default: .github/workflows/slopscout.yml)",
  )


def _parser() -> argparse.ArgumentParser:
  statuses = dependency_statuses()
  status_lines = ["analysis drivers:"]
  status_lines.extend(
      f"  {item['language']:<11} {'installed' if item['ready'] else 'not installed'}"
      for item in statuses
  )
  parser = argparse.ArgumentParser(
      prog="slopscout",
      usage="slopscout [COMMAND] [OPTIONS] [TARGET ...]",
      epilog=(
          "analysis: slopscout [OPTIONS] [TARGET ...]\n"
          "configuration editor: slopscout config edit [FILE|global] [OPTIONS]\n\n"
          "MCP server: slopscout-mcp\n"
          "MCP tools: rank_files, score_files, get_config\n\n"
          + "\n".join(status_lines)
      ),
      formatter_class=argparse.RawDescriptionHelpFormatter,
  )
  analysis_options = parser.add_argument_group("analysis options")
  _add_analysis_options(analysis_options)
  config_edit_options = parser.add_argument_group("config edit options")
  _add_config_edit_options(config_edit_options)
  action_options = parser.add_argument_group("action options")
  _add_action_options(action_options)
  commands = parser.add_subparsers(
      dest="command", title="commands", metavar="{action,analyze,config,install}",
  )
  action_parser = commands.add_parser(
      "action", help="manage the repository's GitHub Actions workflow",
  )
  action_parser.add_argument(
      "action_command", choices=("add", "list", "remove", "set-pass-score"),
  )
  action_parser.add_argument("score", nargs="?", type=_non_negative_score)
  _add_action_options(action_parser)
  analyze_parser = commands.add_parser("analyze", help="analyze Java, TypeScript, and Rust sources")
  analyze_parser.add_argument("targets", nargs="*", type=Path)
  _add_analysis_options(analyze_parser)

  config_parser = commands.add_parser(
      "config", help="inspect or edit configuration and analysis metadata",
  )
  config_parser.add_argument("--format", choices=("text", "json"), default="text")
  config_actions = config_parser.add_subparsers(dest="config_action")
  edit_parser = config_actions.add_parser("edit", help="create or edit a configuration")
  edit_parser.add_argument(
      "configuration", nargs="?", metavar="FILE|global",
      help="configuration file to edit, or 'global' for the global policy",
  )
  _add_config_edit_options(edit_parser)

  install_parser = commands.add_parser("install", help="install an analysis driver")
  install_parser.add_argument("language", choices=tuple(analyzer_dependencies()))
  return parser


def _config_command(output_format: str = "text") -> int:
  document = configuration_document(
      Path.cwd(), warn=lambda message: print(f"warning: {message}", file=sys.stderr),
  )
  if output_format == "json":
    _json(document)
    return 0
  print("CONFIGURATION")
  scope_labels = {
      "local": "local",
      "global_xdg": "global (XDG)",
      "global_home": "global (home)",
  }
  for location in document["configuration"]["locations"]:
    marker = ">" if location["selected"] else " "
    status = "exists" if location["exists"] else "missing"
    scope = scope_labels[location["scope"]]
    print(f"{marker} {scope:<14} {status:<8} {location['path']}")
  print("\nANALYZERS")
  for analyzer in document["analyzers"]:
    status = "installed" if analyzer["installed"] else "not installed"
    print(f"{analyzer['language']:<11} {status:<13} "
          f"{analyzer['dependency']}@{analyzer['version']} "
          f"via {analyzer['installation_method']} ({', '.join(analyzer['extensions'])})")
  print("\nCOMPONENTS")
  for component in document["components"]:
    languages = [language for language, support in component["support"].items()
                 if support not in ("unsupported", "not_applicable")]
    enabled = "enabled" if component["defaults"]["enabled"] else "disabled"
    print(f"{enabled:<8} {component['component_id']}@{component['definition_version']} "
          f"({', '.join(languages) if languages else 'no analyzer'})")
  return 0


def _install_command(language: str) -> int:
  dependency = analyzer_dependencies()[language]
  installed = install_analyzer(language)
  state = "installed" if installed else "already installed"
  print(f"{language}: {dependency.dependency}@{dependency.version} {state}")
  return 0


def _display_number(value: Any) -> str:
  if isinstance(value, int):
    return str(value)
  if isinstance(value, float):
    return f"{value:.6g}"
  return str(value)


def _component_values(item: dict[str, Any], component_id: str) -> list[Any] | None:
  component = item["components"].get(component_id)
  if component is None:
    return None
  return [subject["value"] for subject in component["subjects"]]


def _max_count(item: dict[str, Any], component_id: str) -> str:
  values = _component_values(item, component_id)
  if values is None:
    return "-"
  if not values:
    return "0/0"
  return f"{_display_number(max(values))}/{len(values)}"


def _cyclomatic(item: dict[str, Any]) -> str:
  type_values = _component_values(item, "cyclomatic_class_complexity")
  method_values = _component_values(item, "cyclomatic_method_complexity")
  if type_values is None and method_values is None:
    return "-"
  type_total = "-" if type_values is None else _display_number(max(type_values, default=0))
  method_max = "-" if method_values is None else _display_number(max(method_values, default=0))
  return f"{type_total}/{method_max}"


def _depth(item: dict[str, Any]) -> str:
  nested = _component_values(item, "deeply_nested_if")
  if nested is not None:
    return _display_number(sum(nested))
  rust = _component_values(item, "rust_deep_nesting")
  if rust is not None:
    return _display_number(max(rust, default=0))
  return "-"


def _analysis_table(document: dict[str, Any], *, include_pass: bool) -> str:
  headers = ["RANK", "SCORE", "COG MAX/#", "NPATH MAX/#",
             "CYCLO TOT/MAX", "DEEP", "PATH"]
  if include_pass:
    headers.insert(0, "PASS")
  rows: list[list[str]] = []
  for item in document["files"]:
    row = [
        str(item["rank"]), _display_number(item["score"]),
        _max_count(item, "cognitive_complexity"),
        _max_count(item, "npath_complexity"),
        _cyclomatic(item), _depth(item), item["path"],
    ]
    if include_pass:
      row.insert(0, "yes" if item["passed"] else "NO")
    rows.append(row)
  widths = [len(header) for header in headers]
  for row in rows:
    for index, value in enumerate(row):
      widths[index] = max(widths[index], len(value))

  def formatted(row: list[str]) -> str:
    return "  ".join(
        value.ljust(widths[index]) if index == len(row) - 1
        else value.rjust(widths[index])
        for index, value in enumerate(row)
    ).rstrip()

  return "\n".join([formatted(headers), *(formatted(row) for row in rows)])


def _analyze(args: argparse.Namespace) -> int:
  workspace = Path.cwd().resolve()
  config_path, policy = _policy(args.config)
  outcome = analyze(workspace, targets=args.targets, policy=policy,
                    languages=args.languages, timeout_seconds=args.timeout)
  document = report_document(outcome.scores, calibrated=outcome.profiles.calibrated,
                             diagnostics=outcome.diagnostics,
                             execution_plans=outcome.execution_plans,
                             configuration=None if config_path is None else str(config_path),
                             limit=args.limit, pass_score=args.pass_score)
  if args.format == "json":
    _json(document)
  else:
    print(_analysis_table(document, include_pass=args.pass_score is not None))
    if document["truncated"]:
      print(f"showing {document['returned_files']} of "
            f"{document['summary']['files_analyzed']} files; use --limit 0 for all")
    print(f"profile {document['profile_set_hash']} "
          f"({'calibrated' if document['calibrated'] else 'customized'})")
  if args.pass_score is not None and not document["summary"]["passed"]:
    return 3
  return 0


def main(argv: list[str]) -> int:
  if argv and argv[0] not in COMMANDS and argv[0] not in ("-h", "--help"):
    argv = ["analyze", *argv]
  parser = _parser()
  args = parser.parse_args(argv)
  if args.command is None:
    parser.print_help()
    return 0
  try:
    if args.command == "action":
      from .action import manage_action
      if args.action_command == "set-pass-score" and args.score is None:
        parser.error("action set-pass-score requires SCORE")
      if args.action_command != "set-pass-score" and args.score is not None:
        parser.error(f"action {args.action_command} does not accept SCORE")
      return manage_action(args.action_command, args.workflow, args.score)
    if args.command == "install":
      return _install_command(args.language)
    if args.command == "config":
      if args.config_action == "edit":
        from .ui import serve
        return serve(args.configuration, args.port, open_browser=not args.no_browser)
      return _config_command(args.format)
    if args.command == "analyze":
      return _analyze(args)
  except (AnalysisError, ConfigLoadError, CoreError, AnalyzerRuntimeError, OSError, ValueError) as error:
    print(f"error: {error}", file=sys.stderr)
    return 2
  return 2
