#!/usr/bin/env python3
"""Rank and optionally gate production Java files by design-debt signals."""

from __future__ import annotations

import argparse
import json
import math
import os
import re
import shutil
import subprocess
import sys
import xml.etree.ElementTree as ElementTree
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any, Iterable


COGNITIVE_THRESHOLD = 15
CYCLOMATIC_THRESHOLD = 10
CYCLOMATIC_CLASS_THRESHOLD = 80
NPATH_THRESHOLD = 200
COUPLING_THRESHOLD = 20
GOD_ATFD_THRESHOLD = 5
GOD_TCC_THRESHOLD = 100.0 / 3.0
REPORT_SCHEMA_VERSION = 1
TARGET_SCORE = 100.0
BUILD_FILE_NAMES = ("pom.xml", "build.gradle", "build.gradle.kts")
DEFAULT_ACTION_WORKFLOW = Path(".github/workflows/slopscout.yml")
ACTION_MANAGED_MARKER = "# Managed by slopscout; edit with `slopscout action`."
ACTION_SCORE_PATTERN = re.compile(r'^(\s*max-score:\s*)"[^"]+"\s*$', re.MULTILINE)


class ConfigurationError(RuntimeError):
  """The requested analysis cannot be configured."""


@dataclass
class FileMetrics:
  path: str
  cognitive_methods: list[int] = field(default_factory=list)
  cyclomatic_methods: list[int] = field(default_factory=list)
  cyclomatic_class: int = 0
  npath_methods: list[int] = field(default_factory=list)
  deep_ifs: int = 0
  coupling: int = 0
  god_wmc: int = 0
  god_atfd: int = 0
  god_tcc: float | None = None

  @property
  def is_god_class(self) -> bool:
    return self.god_tcc is not None

  @property
  def score_contributions(self) -> dict[str, float]:
    # A threshold-level violation contributes one severity unit. Log scaling
    # preserves ordering without allowing explosive NPath values to dominate.
    cognitive = 10.0 * sum(
        severity(value, COGNITIVE_THRESHOLD)
        for value in self.cognitive_methods
    )
    npath = 8.0 * sum(
        severity(value, NPATH_THRESHOLD) for value in self.npath_methods
    )
    cyclomatic = 5.0 * sum(
        severity(value, CYCLOMATIC_THRESHOLD)
        for value in self.cyclomatic_methods
    )
    if self.cyclomatic_class:
      cyclomatic += 5.0 * severity(
          self.cyclomatic_class, CYCLOMATIC_CLASS_THRESHOLD
      )
    deeply_nested_ifs = 6.0 * self.deep_ifs
    coupling = 0.0
    if self.coupling:
      coupling = 10.0 * severity(self.coupling, COUPLING_THRESHOLD)
    god_class = 0.0
    if self.is_god_class:
      # WMC is already represented by cyclomatic complexity. The additional
      # score covers the god-class conjunction and its structural dimensions.
      god_class = 30.0
      god_class += 8.0 * severity(self.god_atfd, GOD_ATFD_THRESHOLD)
      god_class += 8.0 * inverse_severity(self.god_tcc, GOD_TCC_THRESHOLD)
    return {
        "cognitive_complexity": cognitive,
        "npath_complexity": npath,
        "cyclomatic_complexity": cyclomatic,
        "deeply_nested_ifs": deeply_nested_ifs,
        "coupling": coupling,
        "god_class": god_class,
    }

  @property
  def score(self) -> float:
    return sum(self.score_contributions.values())


@dataclass
class ModuleAnalysis:
  root: Path
  java_version: str | None
  targets: list[Path]
  files: list[Path]


@dataclass
class AnalysisResult:
  workspace: Path
  directories: list[Path]
  requested_files: list[Path]
  modules: list[ModuleAnalysis]
  metrics: list[FileMetrics]


def severity(value: float, threshold: float) -> float:
  return 1.0 + math.log2(max(value, threshold) / threshold)


def inverse_severity(value: float | None, threshold: float) -> float:
  if value is None:
    return 0.0
  return 1.0 + math.log2(threshold / max(min(value, threshold), 1.0))


def _number(description: str, pattern: str) -> int | None:
  match = re.search(pattern, description)
  return int(match.group(1)) if match else None


def collect_metrics(report: dict[str, Any]) -> list[FileMetrics]:
  results: list[FileMetrics] = []
  for file_report in report.get("files", []):
    metrics = FileMetrics(path=file_report["filename"])
    for violation in file_report.get("violations", []):
      rule = violation["rule"]
      description = violation["description"]
      if rule == "CognitiveComplexity":
        value = _number(description, r"cognitive complexity of (\d+)")
        if value is not None:
          metrics.cognitive_methods.append(value)
      elif rule == "CyclomaticComplexity":
        total = _number(
            description, r"total cyclomatic complexity of (\d+)"
        )
        value = _number(description, r"cyclomatic complexity of (\d+)"
        )
        if total is not None:
          metrics.cyclomatic_class = max(metrics.cyclomatic_class, total)
        elif value is not None:
          metrics.cyclomatic_methods.append(value)
      elif rule == "NPathComplexity":
        value = _number(description, r"NPath complexity of (\d+)")
        if value is not None:
          metrics.npath_methods.append(value)
      elif rule == "AvoidDeeplyNestedIfStmts":
        metrics.deep_ifs += 1
      elif rule == "CouplingBetweenObjects":
        value = _number(description, r"value of (\d+)")
        if value is not None:
          metrics.coupling = max(metrics.coupling, value)
      elif rule == "GodClass":
        match = re.search(
            r"WMC=(\d+), ATFD=(\d+), TCC=([\d.]+)%", description
        )
        if match:
          wmc, atfd, tcc = match.groups()
          metrics.god_wmc = int(wmc)
          metrics.god_atfd = int(atfd)
          metrics.god_tcc = float(tcc)
    results.append(metrics)
  return sorted(results, key=lambda item: (-item.score, item.path))


def project_relative_path(repo: Path, path: Path | str) -> str:
  candidate = Path(path)
  if not candidate.is_absolute():
    candidate = repo / candidate
  try:
    return candidate.resolve().relative_to(repo).as_posix()
  except ValueError:
    return candidate.resolve().as_posix()


def complete_file_inventory(
    metrics: list[FileMetrics], repo: Path, source_files: Iterable[Path],
) -> list[FileMetrics]:
  """Include zero-score production files and normalize all project paths."""
  by_path: dict[str, FileMetrics] = {}
  for item in metrics:
    item.path = project_relative_path(repo, item.path)
    by_path[item.path] = item
  for source_file in source_files:
    path = project_relative_path(repo, source_file)
    by_path.setdefault(path, FileMetrics(path=path))
  return sorted(by_path.values(), key=lambda item: (-item.score, item.path))


def discover_source_roots(repo: Path) -> list[Path]:
  """Find conventional production roots without assuming module names."""
  roots: list[Path] = []
  ignored = {".git", ".gradle", ".idea", "build", "node_modules", "out",
             "target"}
  for current, directories, _ in os.walk(repo):
    directories[:] = sorted(
        name for name in directories if name not in ignored
    )
    current_path = Path(current)
    candidate = current_path / "src" / "main" / "java"
    if candidate.is_dir():
      roots.append(candidate)
      # There cannot be another project root inside this source tree, and
      # avoiding it makes discovery scale with modules rather than Java files.
      if "src" in directories:
        directories.remove("src")
  if roots:
    return sorted(roots)

  # Support small projects that use src/ directly, or keep .java files at the
  # project root. Explicit --source-root values remain the escape hatch for
  # other layouts.
  source_dir = repo / "src"
  if source_dir.is_dir() and any(source_dir.rglob("*.java")):
    return [source_dir]
  if any(repo.glob("*.java")):
    return [repo]
  return []


def nearest_module_root(path: Path) -> Path | None:
  current = path if path.is_dir() else path.parent
  for candidate in (current, *current.parents):
    if any((candidate / name).is_file() for name in BUILD_FILE_NAMES):
      return candidate
  return None


def _common_path(paths: Iterable[Path]) -> Path:
  resolved = [str(path.resolve()) for path in paths]
  return Path(os.path.commonpath(resolved))


def infer_workspace(
    directories: list[Path], files: list[Path], fallback: Path,
) -> Path:
  candidates = directories + [path.parent for path in files]
  if not candidates:
    return fallback.resolve()
  common = _common_path(candidates)
  for candidate in (common, *common.parents):
    if (candidate / ".git").exists():
      return candidate
  module_roots = [
      root for path in candidates if (root := nearest_module_root(path))
  ]
  if module_roots:
    module_common = _common_path(module_roots)
    if module_common != Path(module_common.anchor):
      return module_common
  return fallback.resolve() if common == Path(common.anchor) else common


def _dedupe_nested_directories(directories: Iterable[Path]) -> list[Path]:
  selected: list[Path] = []
  for directory in sorted(set(directories), key=lambda path: (len(path.parts),
                                                               str(path))):
    if not any(directory == parent or directory.is_relative_to(parent)
               for parent in selected):
      selected.append(directory)
  return selected


def resolve_directory_targets(directory: Path) -> list[Path]:
  if not directory.is_dir():
    raise ConfigurationError(
        f"analysis directory is not a directory: {directory}"
    )
  roots = discover_source_roots(directory)
  if roots:
    return roots
  if any(directory.rglob("*.java")):
    return [directory]
  raise ConfigurationError(f"no Java source files found under {directory}")


def default_ruleset_path() -> Path:
  """Locate the bundled ruleset in a source checkout or installed environment."""
  candidates = (
      Path(__file__).resolve().parent / "slopscout-ruleset.xml",
      Path(sys.prefix) / "share" / "slopscout" / "slopscout-ruleset.xml",
  )
  return next((path for path in candidates if path.is_file()), candidates[0])


def _resolve_maven_value(value: str, properties: dict[str, str]) -> str:
  match = re.fullmatch(r"\$\{([^}]+)\}", value.strip())
  return properties.get(match.group(1), value) if match else value


def _maven_java_version(build_file: Path) -> str | None:
  try:
    root = ElementTree.parse(build_file).getroot()
  except (ElementTree.ParseError, OSError):
    return None

  def local_name(element: ElementTree.Element) -> str:
    return element.tag.rsplit("}", 1)[-1]

  properties: dict[str, str] = {}
  for element in root.iter():
    if local_name(element) == "properties":
      properties.update({
          local_name(child): (child.text or "").strip() for child in element
      })
      break

  for name in ("maven.compiler.release", "maven.compiler.source",
               "java.version"):
    value = properties.get(name)
    if value and re.fullmatch(r"(?:1\.)?\d+", value):
      return value

  for element in root.iter():
    if local_name(element) not in {"release", "source"} or not element.text:
      continue
    value = _resolve_maven_value(element.text, properties).strip()
    if re.fullmatch(r"(?:1\.)?\d+", value):
      return value
  return None


def _gradle_java_version(build_file: Path) -> str | None:
  gradle_patterns = (
      r"options\.release(?:\.set\s*\(\s*|\s*=\s*)((?:1\.)?\d+)",
      r"JavaLanguageVersion\.of\(\s*((?:1\.)?\d+)\s*\)",
      r"JavaVersion\.VERSION_((?:1_)?\d+)",
      r"(?:source|target)Compatibility\s*=\s*['\"]?((?:1\.)?\d+)",
  )
  text = build_file.read_text(encoding="utf-8")
  for pattern in gradle_patterns:
    match = re.search(pattern, text)
    if match:
      return match.group(1).replace("_", ".")
  return None


def _java_version_order(version: str) -> int:
  if version.startswith("1."):
    return int(version.removeprefix("1."))
  return int(version)


def detect_java_version(
    repo: Path, source_roots: Iterable[Path] = (),
) -> str | None:
  """Return the highest PMD Java version declared by root or module builds."""
  project_dirs = {repo}
  for source_root in source_roots:
    for parent in source_root.parents:
      if parent == repo or repo in parent.parents:
        if any((parent / name).is_file() for name in BUILD_FILE_NAMES):
          project_dirs.add(parent)
      else:
        break

  versions: list[str] = []
  for project_dir in sorted(project_dirs):
    pom = project_dir / "pom.xml"
    if pom.is_file():
      version = _maven_java_version(pom)
      if version is not None:
        versions.append(version)
    for filename in ("build.gradle.kts", "build.gradle"):
      build_file = project_dir / filename
      if build_file.is_file():
        version = _gradle_java_version(build_file)
        if version is not None:
          versions.append(version)
  if not versions:
    return None
  return "java-" + max(versions, key=_java_version_order)


def run_pmd(
    pmd: str,
    ruleset: Path,
    repo: Path,
    source_roots: Iterable[Path],
    java_version: str | None,
) -> dict[str, Any]:
  command = [
      pmd,
      "check",
      "--no-progress",
      "--no-fail-on-violation",
      "--no-cache",
      "--format",
      "json",
      "--rulesets",
      str(ruleset),
      "--relativize-paths-with",
      str(repo),
  ]
  if java_version:
    command.extend(("--use-version", java_version))
  for source_root in source_roots:
    command.extend(("--dir", str(source_root)))
  completed = subprocess.run(
      command, capture_output=True, check=False, text=True
  )
  if completed.returncode:
    detail = completed.stderr.strip() or completed.stdout.strip()
    raise RuntimeError(f"PMD failed with exit {completed.returncode}:\n{detail}")
  try:
    report = json.loads(completed.stdout)
  except json.JSONDecodeError as error:
    raise RuntimeError(f"PMD did not produce valid JSON: {error}") from error
  errors = report.get("processingErrors", []) + report.get(
      "configurationErrors", []
  )
  if errors:
    detail = "\n".join(str(error) for error in errors)
    raise RuntimeError(f"PMD reported analysis errors:\n{detail}")
  return report


def _resolved_paths(paths: Iterable[Path], base: Path) -> list[Path]:
  results: list[Path] = []
  seen: set[Path] = set()
  for path in paths:
    resolved = (path if path.is_absolute() else base / path).resolve()
    if resolved not in seen:
      seen.add(resolved)
      results.append(resolved)
  return results


def analyze_java(
    directories: Iterable[Path] = (),
    files: Iterable[Path] = (),
    *,
    ruleset: Path | None = None,
    java_version: str | None = None,
    pmd: str | None = None,
    base: Path | None = None,
) -> AnalysisResult:
  """Analyze recursive directory scopes and exact Java files."""
  invocation_directory = (base or Path.cwd()).resolve()
  requested_directories = _resolved_paths(directories, invocation_directory)
  requested_files = _resolved_paths(files, invocation_directory)
  if not requested_directories and not requested_files:
    requested_directories = [invocation_directory]

  analysis_directories: list[Path] = []
  for directory in requested_directories:
    analysis_directories.extend(resolve_directory_targets(directory))
  analysis_directories = _dedupe_nested_directories(analysis_directories)

  for source_file in requested_files:
    if not source_file.is_file():
      raise ConfigurationError(f"analysis file does not exist: {source_file}")
    if source_file.suffix.lower() != ".java":
      raise ConfigurationError(
          f"analysis file is not Java source: {source_file}"
      )

  workspace = infer_workspace(
      requested_directories, requested_files, invocation_directory
  )
  selected_ruleset = (ruleset or default_ruleset_path()).resolve()
  if not selected_ruleset.is_file():
    raise ConfigurationError(f"PMD ruleset not found: {selected_ruleset}")
  selected_pmd = pmd or shutil.which("pmd")
  if not selected_pmd:
    raise ConfigurationError("pmd is not on PATH")

  groups: dict[Path, dict[str, list[Path]]] = {}
  for directory in analysis_directories:
    module_root = nearest_module_root(directory) or workspace
    groups.setdefault(module_root, {"directories": [], "files": []})[
        "directories"
    ].append(directory)
  for source_file in requested_files:
    module_root = nearest_module_root(source_file) or workspace
    groups.setdefault(module_root, {"directories": [], "files": []})[
        "files"
    ].append(source_file)

  collected: list[FileMetrics] = []
  all_source_files: set[Path] = set()
  modules: list[ModuleAnalysis] = []
  targets_by_version: dict[str | None, list[Path]] = {}
  for module_root, group in sorted(groups.items(), key=lambda item: str(item[0])):
    group_directories = _dedupe_nested_directories(group["directories"])
    group_files = _resolved_paths(group["files"], invocation_directory)
    covered_files = {
        source_file
        for directory in group_directories
        for source_file in directory.rglob("*.java")
    }
    exact_targets = [
        source_file for source_file in group_files
        if source_file not in covered_files
    ]
    source_files = sorted(covered_files | set(group_files))
    if not source_files:
      continue
    targets = [*group_directories, *exact_targets]
    detected_version = java_version or detect_java_version(module_root, targets)
    targets_by_version.setdefault(detected_version, []).extend(targets)
    all_source_files.update(source_files)
    modules.append(ModuleAnalysis(
        root=module_root,
        java_version=detected_version,
        targets=targets,
        files=source_files,
    ))

  # PMD startup and ruleset initialization are significant in multi-module
  # builds. Modules that use the same language version can be analyzed in one
  # invocation without changing their separately reported metadata.
  for detected_version, targets in targets_by_version.items():
    report = run_pmd(
        selected_pmd,
        selected_ruleset,
        workspace,
        _dedupe_nested_directories(targets),
        detected_version,
    )
    collected.extend(collect_metrics(report))

  metrics = complete_file_inventory(collected, workspace, all_source_files)
  return AnalysisResult(
      workspace=workspace,
      directories=requested_directories,
      requested_files=requested_files,
      modules=modules,
      metrics=metrics,
  )


def metric_to_document(item: FileMetrics, rank: int) -> dict[str, Any]:
  god_class = None
  if item.is_god_class:
    god_class = {
        "wmc": item.god_wmc,
        "atfd": item.god_atfd,
        "tcc_percent": item.god_tcc,
    }
  return {
      "rank": rank,
      "path": item.path,
      "score": item.score,
      "contributions": item.score_contributions,
      "metrics": {
          "cognitive_methods": item.cognitive_methods,
          "cyclomatic_methods": item.cyclomatic_methods,
          "cyclomatic_class": item.cyclomatic_class,
          "npath_methods": item.npath_methods,
          "deeply_nested_ifs": item.deep_ifs,
          "coupling_between_objects": item.coupling,
          "god_class": god_class,
      },
  }


def metric_max(values: list[int]) -> str:
  return "-" if not values else f"{max(values)}/{len(values)}"


def render_table(
    metrics: list[FileMetrics], limit: int, compact: bool = False,
) -> str:
  selected = metrics if limit == 0 else metrics[:limit]
  rows = []
  for rank, item in enumerate(selected, 1):
    god = "-"
    if item.is_god_class:
      god = f"{item.god_wmc}/{item.god_atfd}/{item.god_tcc:.1f}%"
    cyclomatic = "-"
    if item.cyclomatic_class or item.cyclomatic_methods:
      class_total = str(item.cyclomatic_class or "-")
      method_max = max(item.cyclomatic_methods, default=0)
      cyclomatic = f"{class_total}/{method_max or '-'}"
    if compact:
      rows.append([str(rank), f"{item.score:.1f}", god, item.path])
    else:
      rows.append([
          str(rank),
          f"{item.score:.1f}",
          god,
          metric_max(item.cognitive_methods),
          metric_max(item.npath_methods),
          cyclomatic,
          str(item.deep_ifs or "-"),
          str(item.coupling or "-"),
          item.path,
      ])

  headers = (["#", "score", "god W/A/T", "path"] if compact else [
      "#", "score", "god W/A/T", "cog max/#", "npath max/#",
      "cyclo tot/max", "deep", "CBO", "file",
  ])
  widths = [len(header) for header in headers]
  for row in rows:
    for index, value in enumerate(row):
      widths[index] = max(widths[index], len(value))

  def format_row(row: list[str]) -> str:
    cells = []
    for index, value in enumerate(row):
      cells.append(value.rjust(widths[index]) if index < len(row) - 1
                   else value.ljust(widths[index]))
    return "|".join(cells).rstrip()

  lines = [format_row(headers), format_row(["-" * width for width in widths])]
  lines.extend(format_row(row) for row in rows)
  return "\n".join(lines)


def explain() -> str:
  return """\
The score is a refactoring triage heuristic and a measurable forcing function
for coding agents. Each PMD violation starts at one unit at its configured
threshold; severity above the threshold is 1 + log2(measure / threshold).
Weights are:

  cognitive method 10    NPath method 8       cyclomatic method 5
  cyclomatic class 5     deeply nested if 6   coupling 10
  PMD GodClass 30 + ATFD 8 + low-cohesion TCC 8

Class-level cyclomatic complexity already represents GodClass WMC, so WMC is
shown but is not scored twice. Columns ending in /n show maximum/count;
cyclo is class total/highest violating method; god is WMC/ATFD/TCC. A dash
means PMD found no threshold violation for that signal, not necessarily zero.
Rows are pipe-delimited, so fields can be selected with cut -d'|' -fN.
"""


def build_report_document(
    analysis: AnalysisResult,
    limit: int,
    max_score: float | None,
    ordered_metrics: list[FileMetrics] | None = None,
) -> dict[str, Any]:
  metrics = analysis.metrics
  output_metrics = ordered_metrics if ordered_metrics is not None else metrics
  selected = output_metrics if limit == 0 else output_metrics[:limit]
  ranks = {item.path: rank for rank, item in enumerate(metrics, 1)}
  highest = metrics[0] if metrics else None
  max_score_failures = (
      [] if max_score is None
      else [item for item in metrics if item.score > max_score]
  )
  gate = {
      "enabled": max_score is not None,
      "passed": not max_score_failures,
      "max_score": max_score,
      "max_score_failures": [
          {"path": item.path, "score": item.score}
          for item in max_score_failures
      ],
  }
  return {
      "schema_version": REPORT_SCHEMA_VERSION,
      "project": str(analysis.workspace),
      "scope": {
          "directories": [
              project_relative_path(analysis.workspace, path)
              for path in analysis.directories
          ],
          "files": [
              project_relative_path(analysis.workspace, path)
              for path in analysis.requested_files
          ],
      },
      "modules": [
          {
              "root": project_relative_path(analysis.workspace, module.root),
              "java_version": module.java_version,
              "files_analyzed": len(module.files),
          }
          for module in analysis.modules
      ],
      "summary": {
          "target_score": TARGET_SCORE,
          "files_analyzed": len(metrics),
          "files_above_target": sum(
              item.score > TARGET_SCORE for item in metrics
          ),
          "highest_score": highest.score if highest is not None else None,
          "highest_path": highest.path if highest is not None else None,
      },
      "total_files": len(metrics),
      "returned_files": len(selected),
      "truncated": len(selected) < len(output_metrics),
      "files": [
          metric_to_document(item, ranks[item.path])
          for item in selected
      ],
      "gate": gate,
  }


def parse_args(argv: list[str]) -> argparse.Namespace:
  parser = argparse.ArgumentParser(
      prog="slopscout",
      description="Measure production Java design debt using PMD."
  )
  parser.add_argument(
      "directories",
      type=Path,
      nargs="*",
      help=("Java project, module, source, or package directories to recurse "
            "(default: current directory)"),
  )
  parser.add_argument(
      "--repo",
      type=Path,
      help="project directory (compatibility alias for the positional argument)",
  )
  parser.add_argument(
      "--source-root",
      type=Path,
      action="append",
      default=[],
      help=("production Java source root, relative to the project directory "
            "unless absolute; repeat for multiple roots (default: auto-detect)"),
  )
  parser.add_argument(
      "--file",
      type=Path,
      action="append",
      default=[],
      dest="files",
      help="exact Java source file to analyze; repeat for multiple files",
  )
  parser.add_argument(
      "--ruleset",
      type=Path,
      default=default_ruleset_path(),
      help="PMD ruleset (default: bundled slopscout-ruleset.xml)",
  )
  parser.add_argument(
      "--java-version",
      help=("PMD Java language version such as java-25 "
            "(default: infer from Maven/Gradle, then use PMD default)"),
  )
  parser.add_argument(
      "-l", "--limit",
      type=int,
      default=50,
      help="number of results to print; 0 prints all (default: 50)",
  )
  parser.add_argument(
      "-c", "--compact",
      action="store_true",
      help="show only rank, score, GodClass metrics, and path",
  )
  parser.add_argument(
      "--format",
      choices=("text", "json"),
      default="text",
      dest="output_format",
      help="output format (default: text)",
  )
  parser.add_argument(
      "--max-score",
      type=float,
      help=("fail if any production Java file has an unrounded score above "
            "this value"),
  )
  parser.add_argument(
      "--explain", action="store_true", help="explain scoring after the table"
  )
  args = parser.parse_args(argv)
  if args.repo is not None and args.directories:
    parser.error("specify directories positionally or with --repo, not both")
  if len(args.directories) > 1 and args.source_root:
    parser.error("--source-root cannot be combined with multiple directories")
  if args.repo is not None:
    args.directories = [args.repo]
  if args.source_root:
    project = args.directories[0] if args.directories else Path.cwd()
    args.directories = [
        root if root.is_absolute() else project / root
        for root in args.source_root
    ]
  if args.limit < 0:
    parser.error("--limit must be non-negative")
  if args.max_score is not None and (
      not math.isfinite(args.max_score) or args.max_score < 0
  ):
    parser.error("--max-score must be a finite non-negative number")
  if args.output_format == "json" and args.explain:
    parser.error("--explain is only available with text output")
  return args


def validate_max_score(value: str) -> float:
  try:
    score = float(value)
  except ValueError as error:
    raise argparse.ArgumentTypeError("must be a finite non-negative number") from error
  if not math.isfinite(score) or score < 0:
    raise argparse.ArgumentTypeError("must be a finite non-negative number")
  return score


def action_workflow(score: float) -> str:
  return f'''{ACTION_MANAGED_MARKER}
name: Slopscout

on:
  pull_request:
  push:
    branches: [main]
  workflow_dispatch:

permissions:
  contents: read

jobs:
  score:
    name: Java design-debt gate
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-java@v5
        with:
          distribution: temurin
          java-version: '21'
      - name: Install PMD
        run: |
          curl --fail --location --silent --show-error \\
            --output "$RUNNER_TEMP/pmd.zip" \\
            https://github.com/pmd/pmd/releases/download/pmd_releases%2F7.26.0/pmd-bin-7.26.0.zip
          unzip -q "$RUNNER_TEMP/pmd.zip" -d "$RUNNER_TEMP"
          echo "$RUNNER_TEMP/pmd-bin-7.26.0/bin" >> "$GITHUB_PATH"
      - uses: blater/slop-scout@main
        with:
          max-score: "{score:g}"
'''


def parse_action_args(argv: list[str]) -> argparse.Namespace:
  parser = argparse.ArgumentParser(
      prog="slopscout action",
      description="Manage the repository's Slopscout GitHub Actions workflow.",
  )
  parser.add_argument(
      "command", choices=("add", "list", "remove", "set-max-score"),
  )
  parser.add_argument(
      "score", nargs="?", type=validate_max_score,
      help="maximum allowed score (required for add and set-max-score)",
  )
  parser.add_argument(
      "--workflow", type=Path, default=DEFAULT_ACTION_WORKFLOW,
      help="workflow path (default: .github/workflows/slopscout.yml)",
  )
  args = parser.parse_args(argv)
  if args.command in ("add", "set-max-score") and args.score is None:
    parser.error(f"{args.command} requires SCORE")
  if args.command in ("list", "remove") and args.score is not None:
    parser.error(f"{args.command} does not accept SCORE")
  return args


def managed_action_score(content: str) -> float | None:
  if ACTION_MANAGED_MARKER not in content:
    return None
  match = ACTION_SCORE_PATTERN.search(content)
  return float(match.group(0).split('"')[1]) if match else None


def manage_action(argv: list[str]) -> int:
  args = parse_action_args(argv)
  workflow = args.workflow
  if args.command == "list":
    if not workflow.is_file():
      print(f"Slopscout action is not installed ({workflow}).")
      return 0
    score = managed_action_score(workflow.read_text(encoding="utf-8"))
    if score is None:
      print(f"{workflow} exists but is not managed by slopscout.", file=sys.stderr)
      return 2
    print(f"Slopscout action is installed at {workflow} (max score: {score:g}).")
    return 0
  if args.command == "add":
    if workflow.exists():
      print(f"refusing to overwrite existing workflow: {workflow}", file=sys.stderr)
      return 2
    workflow.parent.mkdir(parents=True, exist_ok=True)
    workflow.write_text(action_workflow(args.score), encoding="utf-8")
    print(f"Added Slopscout action at {workflow} (max score: {args.score:g}).")
    return 0
  if not workflow.is_file():
    print(f"Slopscout action is not installed ({workflow}).", file=sys.stderr)
    return 2
  content = workflow.read_text(encoding="utf-8")
  if managed_action_score(content) is None:
    print(f"refusing to modify unmanaged workflow: {workflow}", file=sys.stderr)
    return 2
  if args.command == "remove":
    workflow.unlink()
    print(f"Removed Slopscout action at {workflow}.")
    return 0
  updated, changes = ACTION_SCORE_PATTERN.subn(
      lambda match: f'{match.group(1)}"{args.score:g}"', content, count=1,
  )
  if changes != 1:
    print(f"unable to find max-score in {workflow}", file=sys.stderr)
    return 2
  workflow.write_text(updated, encoding="utf-8")
  print(f"Updated Slopscout max score to {args.score:g} in {workflow}.")
  return 0


def main(argv: list[str]) -> int:
  if argv and argv[0] == "action":
    return manage_action(argv[1:])
  args = parse_args(argv)
  try:
    analysis = analyze_java(
        args.directories,
        args.files,
        ruleset=args.ruleset,
        java_version=args.java_version,
    )
  except ConfigurationError as error:
    print(f"error: {error}", file=sys.stderr)
    return 2
  except RuntimeError as error:
    print(f"error: {error}", file=sys.stderr)
    return 1
  metrics = analysis.metrics
  max_score_failures = []
  if args.max_score is not None:
    max_score_failures = [
        item for item in metrics if item.score > args.max_score
    ]
  gate_passed = not max_score_failures

  if args.output_format == "json":
    document = build_report_document(analysis, args.limit, args.max_score)
    print(json.dumps(document, indent=2, sort_keys=True))
  else:
    print(render_table(metrics, args.limit, args.compact))
    if args.explain:
      print()
      print(explain())
    if max_score_failures:
      noun = "file" if len(max_score_failures) == 1 else "files"
      verb = "exceeds" if len(max_score_failures) == 1 else "exceed"
      highest = max_score_failures[0]
      print(
          f"quality gate failed: {len(max_score_failures)} {noun} {verb} "
          f"--max-score {args.max_score:g}; highest is {highest.path} "
          f"({highest.score:.6g})",
          file=sys.stderr,
      )
  return 0 if gate_passed else 3


def cli() -> None:
  raise SystemExit(main(sys.argv[1:]))


if __name__ == "__main__":
  cli()
