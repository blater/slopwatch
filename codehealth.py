#!/usr/bin/env python3
"""Rank production Java files by PMD design-debt signals."""

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
  def score(self) -> float:
    # A threshold-level violation contributes one severity unit. Log scaling
    # preserves ordering without allowing explosive NPath values to dominate.
    score = 0.0
    score += 10.0 * sum(
        severity(value, COGNITIVE_THRESHOLD)
        for value in self.cognitive_methods
    )
    score += 8.0 * sum(
        severity(value, NPATH_THRESHOLD) for value in self.npath_methods
    )
    score += 5.0 * sum(
        severity(value, CYCLOMATIC_THRESHOLD)
        for value in self.cyclomatic_methods
    )
    if self.cyclomatic_class:
      score += 5.0 * severity(
          self.cyclomatic_class, CYCLOMATIC_CLASS_THRESHOLD
      )
    score += 6.0 * self.deep_ifs
    if self.coupling:
      score += 10.0 * severity(self.coupling, COUPLING_THRESHOLD)
    if self.is_god_class:
      # WMC is already represented by cyclomatic complexity. The additional
      # score covers the god-class conjunction and its structural dimensions.
      score += 30.0
      score += 8.0 * severity(self.god_atfd, GOD_ATFD_THRESHOLD)
      score += 8.0 * inverse_severity(self.god_tcc, GOD_TCC_THRESHOLD)
    return score


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
        if any((parent / name).is_file() for name in (
            "pom.xml", "build.gradle", "build.gradle.kts"
        )):
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
The score is a refactoring triage heuristic, not a quality gate. Each PMD
violation starts at one unit at its configured threshold; severity above the
threshold is 1 + log2(measure / threshold). Weights are:

  cognitive method 10    NPath method 8       cyclomatic method 5
  cyclomatic class 5     deeply nested if 6   coupling 10
  PMD GodClass 30 + ATFD 8 + low-cohesion TCC 8

Class-level cyclomatic complexity already represents GodClass WMC, so WMC is
shown but is not scored twice. Columns ending in /n show maximum/count;
cyclo is class total/highest violating method; god is WMC/ATFD/TCC. A dash
means PMD found no threshold violation for that signal, not necessarily zero.
Rows are pipe-delimited, so fields can be selected with cut -d'|' -fN.
"""


def parse_args(argv: list[str]) -> argparse.Namespace:
  tool_dir = Path(__file__).resolve().parent
  parser = argparse.ArgumentParser(
      prog="codehealth",
      description="Rank production Java design debt using PMD."
  )
  parser.add_argument(
      "directory",
      type=Path,
      nargs="?",
      default=None,
      help="Java project directory (default: current directory)",
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
      "--ruleset",
      type=Path,
      default=tool_dir / "codehealth-ruleset.xml",
      help="PMD ruleset (default: codehealth-ruleset.xml beside this script)",
  )
  parser.add_argument(
      "--java-version",
      help=("PMD Java language version such as java-21 "
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
      "--explain", action="store_true", help="explain scoring after the table"
  )
  args = parser.parse_args(argv)
  if args.repo is not None and args.directory is not None:
    parser.error("specify the project using either DIRECTORY or --repo, not both")
  args.directory = args.repo or args.directory or Path.cwd()
  if args.limit < 0:
    parser.error("--limit must be non-negative")
  return args


def main(argv: list[str]) -> int:
  args = parse_args(argv)
  repo = args.directory.resolve()
  pmd = shutil.which("pmd")
  if not pmd:
    print("error: pmd is not on PATH", file=sys.stderr)
    return 2
  roots = [
      (root if root.is_absolute() else repo / root).resolve()
      for root in args.source_root
  ] or discover_source_roots(repo)
  if not roots:
    print(f"error: no production Java source roots found under {repo}",
          file=sys.stderr)
    return 2
  missing_roots = [str(root) for root in roots if not root.is_dir()]
  if missing_roots:
    print("error: source root is not a directory: " + ", ".join(missing_roots),
          file=sys.stderr)
    return 2
  ruleset = args.ruleset.resolve()
  if not ruleset.is_file():
    print(f"error: PMD ruleset not found: {ruleset}", file=sys.stderr)
    return 2
  java_version = args.java_version or detect_java_version(repo, roots)
  try:
    report = run_pmd(pmd, ruleset, repo, roots, java_version)
  except RuntimeError as error:
    print(f"error: {error}", file=sys.stderr)
    return 1
  metrics = collect_metrics(report)
  print(render_table(metrics, args.limit, args.compact))
  if args.explain:
    print()
    print(explain())
  return 0


if __name__ == "__main__":
  raise SystemExit(main(sys.argv[1:]))
