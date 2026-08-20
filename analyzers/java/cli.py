#!/usr/bin/env python3
"""Protocol-v1 process adapter around the pinned PMD metric bridge."""

from __future__ import annotations

import glob
import json
import os
import re
import subprocess
import shutil
import sys
import tempfile
from pathlib import Path
from typing import Any

try:
  from .adapter import PMD_VERSION, decode_pmd_report
except ImportError:  # Direct execution from a source checkout.
  from adapter import PMD_VERSION, decode_pmd_report


def _emit(invocation: str, record_type: str, **payload: Any) -> None:
  print(json.dumps({
      "type": record_type, "protocol_version": 1,
      "invocation_id": invocation, **payload,
  }, separators=(",", ":"), sort_keys=True), flush=True)


def _classpath(options: dict[str, Any]) -> list[str]:
  configured = options.get("pmd_classpath")
  if configured is not None:
    if not isinstance(configured, list) or not configured \
        or any(not isinstance(item, str) or not item for item in configured):
      raise ValueError("pmd_classpath must be a non-empty list of absolute files")
    paths = [Path(item) for item in configured]
  else:
    bridge_dir = Path(__file__).resolve().parent
    patterns = (
        str(bridge_dir / "target" / "dependency" / "*"),
        f"/opt/homebrew/Cellar/pmd/{PMD_VERSION}/libexec/lib/*",
        f"/usr/local/Cellar/pmd/{PMD_VERSION}/libexec/lib/*",
        f"/opt/pmd-bin-{PMD_VERSION}/lib/*",
    )
    paths = [Path(item) for pattern in patterns for item in glob.glob(pattern)]
  if not paths or any(not item.is_absolute() or not item.is_file() for item in paths):
    raise ValueError(
        f"PMD {PMD_VERSION} libraries were not found; configure pmd_classpath"
    )
  return [str(item) for item in paths]


def _subject(document: dict[str, Any]) -> dict[str, Any]:
  return {
      "name": document["name"],
      "line": document["line"], "column": document["column"],
      "end_line": document["end_line"], "end_column": document["end_column"],
  }


def _failure(request: dict[str, Any], message: str) -> None:
  invocation = str(request.get("invocation_id", "invalid"))
  failed = []
  for unit in request.get("units", []):
    if not isinstance(unit, dict):
      continue
    unit_id = unit.get("unit_id")
    failed.append(unit_id)
    for path in unit.get("source_paths", []):
      for component in request.get("components", []):
        _emit(invocation, "coverage", unit_id=unit_id, path=path,
              component_id=component.get("component_id"),
              definition_version=component.get("definition_version"),
              state="failed", reason=message)
  _emit(invocation, "diagnostic", severity="error", code="JAVA_ANALYZER",
        message=message, unit_id=None, path=None)
  _emit(invocation, "terminal", status="failure", message=message,
        analyzed_unit_ids=[], failed_unit_ids=failed, skipped_unit_ids=[])


def _pmd_failure(returncode: int, stderr: str) -> str:
  match = re.search(r"(\d+) errors? occurred while executing PMD", stderr)
  if match:
    count = int(match.group(1))
    noun = "error" if count == 1 else "errors"
    return (
        f"PMD exited with status {returncode} after {count} analysis {noun}; "
        "no scores were produced"
    )
  detail = next((line.strip() for line in reversed(stderr.splitlines()) if line.strip()), "")
  suffix = f": {detail}" if detail else ""
  return f"PMD exited with status {returncode}{suffix}; no scores were produced"


def main() -> int:
  raw = sys.stdin.readline()
  try:
    request = json.loads(raw)
  except (json.JSONDecodeError, TypeError):
    return 2
  invocation = request.get("invocation_id")
  if request.get("type") != "request" or request.get("protocol_version") != 1 \
      or not isinstance(invocation, str):
    return 2
  units = request.get("units")
  components = request.get("components")
  if not isinstance(units, list) or not isinstance(components, list):
    _failure(request, "request units and components must be arrays")
    return 0
  try:
    workspace = Path(request["workspace"]).resolve(strict=True)
    bridge_dir = Path(__file__).resolve().parent
    bridge_jar = bridge_dir / "target" / "slopscout-pmd-bridge-0.1.0.jar"
    ruleset = bridge_dir / "slopscout-bridge-ruleset.xml"
    if not bridge_jar.is_file():
      raise ValueError("Java bridge is not installed")
    classpath = [str(bridge_jar), *_classpath(dict(request.get("options", {})))]
    java = request.get("options", {}).get("java_path") or shutil.which("java")
    if not isinstance(java, str) or not Path(java).is_absolute():
      raise ValueError("java_path must be absolute")
    all_paths: list[str] = []
    path_units: dict[str, str] = {}
    for unit in units:
      if unit.get("language") != "java":
        raise ValueError("Java analyzer received a non-Java unit")
      for path in unit.get("source_paths", []):
        candidate = (workspace / path).resolve(strict=True)
        candidate.relative_to(workspace)
        all_paths.append(path)
        path_units[path] = unit["unit_id"]
    report_path = Path(os.environ.get("SLOPSCOUT_WORK_DIR", tempfile.gettempdir())) / "pmd.json"
    command = [java, "-cp", os.pathsep.join(classpath),
               "net.sourceforge.pmd.cli.PmdCli", "check", "-d",
               ",".join(str(workspace / item) for item in all_paths),
               "-R", str(ruleset), "-f", "json", "-r", str(report_path)]
    completed = subprocess.run(command, cwd=workspace, stdin=subprocess.DEVNULL,
                               stdout=subprocess.DEVNULL, stderr=subprocess.PIPE,
                               text=True, timeout=120, check=False)
    if completed.returncode not in (0, 4):
      raise ValueError(_pmd_failure(completed.returncode, completed.stderr))
    decoded = decode_pmd_report(
        json.loads(report_path.read_text(encoding="utf-8")), workspace=workspace,
        requested_files=[workspace / item for item in all_paths],
    )
    requested = {(item["component_id"], item["definition_version"])
                 for item in components}
    for item in decoded["measurements"]:
      if (item["component_id"], item["definition_version"]) not in requested:
        continue
      _emit(invocation, "measurement", unit_id=path_units[item["path"]],
            component_id=item["component_id"],
            definition_version=item["definition_version"], path=item["path"],
            language="java", scope=item["scope"], value=item["value"],
            subject=_subject(item["subject"]), attributes=item["attributes"],
            provenance={key: value for key, value in item["provenance"].items()
                        if key in {"analyzer", "analyzer_version", "rule"}})
    unavailable = {(item["path"], item["component_id"]): item["reason"]
                   for item in decoded["unavailable"]}
    failed_paths = {item["path"] for item in decoded["coverage"]
                    if item["state"] != "complete"}
    for path in all_paths:
      for component in components:
        reason = unavailable.get((path, component["component_id"]))
        state = "unavailable" if reason else ("failed" if path in failed_paths else "complete")
        _emit(invocation, "coverage", unit_id=path_units[path], path=path,
              component_id=component["component_id"],
              definition_version=component["definition_version"], state=state,
              reason=reason or ("PMD did not report the source" if state == "failed" else ""))
    analyzed = [unit["unit_id"] for unit in units]
    for unit in units:
      _emit(invocation, "execution_plan", unit_id=unit["unit_id"],
            parser_modes=[f"pmd-java-{PMD_VERSION}"],
            kernels=["pmd_ast", "structural_metrics", "java_design_metrics"],
            discovered_source_count=len(unit["source_paths"]),
            parsed_source_count=len(unit["source_paths"]))
    _emit(invocation, "terminal", status="success", message="",
          analyzed_unit_ids=analyzed, failed_unit_ids=[], skipped_unit_ids=[])
  except Exception as error:
    _failure(request, str(error))
  return 0


if __name__ == "__main__":
  raise SystemExit(main())
