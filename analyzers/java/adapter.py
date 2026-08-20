"""Strict decoder for the PMD 7.26.0 Slopscout bridge report."""

from __future__ import annotations

from dataclasses import dataclass
from pathlib import Path
from typing import Any, Iterable, Mapping


BRIDGE_PREFIX = "SLOPSCOUT_METRIC_V1"
PMD_VERSION = "7.26.0"


class BridgeDecodeError(ValueError):
  """The bridge returned a malformed record."""


@dataclass(frozen=True)
class _Location:
  line: int
  column: int
  end_line: int
  end_column: int

  def document(self) -> dict[str, int]:
    return {
        "line": self.line,
        "column": self.column,
        "end_line": self.end_line,
        "end_column": self.end_column,
    }


def _integer(value: str, field: str) -> int:
  try:
    parsed = int(value)
  except ValueError as error:
    raise BridgeDecodeError(f"{field} must be an integer, got {value!r}") from error
  if parsed < 0:
    raise BridgeDecodeError(f"{field} must be non-negative")
  return parsed


def _decimal(value: str, field: str) -> float:
  try:
    parsed = float(value)
  except ValueError as error:
    raise BridgeDecodeError(f"{field} must be numeric, got {value!r}") from error
  if not 0.0 <= parsed <= 1.0:
    raise BridgeDecodeError(f"{field} must be between zero and one")
  return parsed


def _fields(description: str) -> dict[str, str]:
  parts = description.split("|")
  if not parts or parts[0] != BRIDGE_PREFIX:
    raise BridgeDecodeError("metric record has an unknown bridge prefix")
  result: dict[str, str] = {}
  for part in parts[1:]:
    key, separator, value = part.partition("=")
    if not separator or not key or not value:
      raise BridgeDecodeError(f"malformed bridge field: {part!r}")
    if key in result:
      raise BridgeDecodeError(f"duplicate bridge field: {key}")
    result[key] = value
  return result


def _require(fields: Mapping[str, str], expected: set[str]) -> None:
  actual = set(fields)
  if actual != expected:
    missing = sorted(expected - actual)
    unknown = sorted(actual - expected)
    raise BridgeDecodeError(
        f"bridge fields do not match schema; missing={missing}, unknown={unknown}"
    )


def _path(filename: object, workspace: Path) -> str:
  if not isinstance(filename, str) or not filename:
    raise BridgeDecodeError("PMD filename must be a non-empty string")
  candidate = Path(filename).resolve()
  try:
    return candidate.relative_to(workspace).as_posix()
  except ValueError as error:
    raise BridgeDecodeError(
        f"PMD reported a path outside the workspace: {candidate}"
    ) from error


def _location(violation: Mapping[str, Any]) -> _Location:
  values = []
  for key in ("beginline", "begincolumn", "endline", "endcolumn"):
    value = violation.get(key)
    if not isinstance(value, int) or isinstance(value, bool) or value < 1:
      raise BridgeDecodeError(f"PMD violation {key} must be a positive integer")
    values.append(value)
  return _Location(*values)


def _measurement(
    component_id: str,
    definition_version: str,
    path: str,
    scope: str,
    value: int | float,
    symbol: str,
    location: _Location,
    attributes: Mapping[str, Any] | None = None,
) -> dict[str, Any]:
  return {
      "type": "measurement",
      "component_id": component_id,
      "definition_version": definition_version,
      "language": "java",
      "path": path,
      "scope": scope,
      "value": value,
      "subject": {"name": symbol, **location.document()},
      "attributes": dict(attributes or {}),
      "provenance": {
          "analyzer": "slopscout-java-pmd",
          "analyzer_version": "0.1.0",
          "oracle": f"PMD {PMD_VERSION}",
          "rule": "SlopscoutMetrics",
      },
  }


def _decode_metric(
    violation: Mapping[str, Any], path: str,
) -> tuple[list[dict[str, Any]], list[dict[str, Any]]]:
  description = violation.get("description")
  if not isinstance(description, str):
    raise BridgeDecodeError("bridge violation description must be a string")
  fields = _fields(description)
  location = _location(violation)
  scope = fields.get("scope")
  symbol = fields.get("symbol", "")
  if not symbol:
    raise BridgeDecodeError("bridge metric symbol is missing")

  measurements: list[dict[str, Any]] = []
  unavailable: list[dict[str, Any]] = []
  if scope == "function":
    _require(fields, {
        "scope", "kind", "symbol", "cognitive", "cyclomatic", "npath",
    })
    common = {"kind": fields["kind"]}
    specifications = (
        ("cognitive", "cognitive_complexity", "pmd-sonar-v1"),
        ("cyclomatic", "cyclomatic_method_complexity", "pmd-v1"),
        ("npath", "npath_complexity", "pmd-v1"),
    )
    for field, component, version in specifications:
      raw_value = fields[field]
      if raw_value == "unavailable":
        unavailable.append({
            "component_id": component,
            "path": path,
            "scope": "function",
            "subject": symbol,
            "reason": f"PMD {PMD_VERSION} does not support {component} on {fields['kind']}",
        })
        continue
      measurements.append(_measurement(
          component, version, path, "function", _integer(raw_value, field),
          symbol, location, common,
      ))
    return measurements, unavailable

  if scope == "type":
    _require(fields, {
        "scope", "kind", "symbol", "wmc", "atfd", "tcc", "fan_out",
    })
    kind = fields["kind"]
    wmc = _integer(fields["wmc"], "wmc")
    atfd = _integer(fields["atfd"], "atfd")
    fan_out = _integer(fields["fan_out"], "fan_out")
    measurements.extend((
        _measurement(
            "cyclomatic_class_complexity", "pmd-v1", path, "type", wmc,
            symbol, location, {"kind": kind},
        ),
        _measurement(
            "coupling_between_objects", "pmd-v1", path, "type", fan_out,
            symbol, location, {"kind": kind},
        ),
    ))
    raw_tcc = fields["tcc"]
    if raw_tcc == "unavailable":
      if kind != "interface":
        raise BridgeDecodeError("TCC may be unavailable only for an interface")
    else:
      tcc = _decimal(raw_tcc, "tcc")
      measurements.append(_measurement(
          "god_class", "pmd-v1", path, "type", wmc, symbol, location,
          {"kind": kind, "wmc": wmc, "atfd": atfd, "tcc": tcc},
      ))
    return measurements, unavailable

  raise BridgeDecodeError(f"unsupported bridge metric scope: {scope!r}")


def decode_pmd_report(
    report: Mapping[str, Any],
    *,
    workspace: Path,
    requested_files: Iterable[Path],
) -> dict[str, Any]:
  """Decode a complete PMD bridge report into protocol-shaped records."""
  workspace = workspace.resolve()
  if report.get("pmdVersion") != PMD_VERSION:
    raise BridgeDecodeError(
        f"expected PMD {PMD_VERSION}, got {report.get('pmdVersion')!r}"
    )
  files = report.get("files")
  if not isinstance(files, list):
    raise BridgeDecodeError("PMD report files must be a list")

  requested = {
      path.resolve().relative_to(workspace).as_posix() for path in requested_files
  }
  measurements: list[dict[str, Any]] = []
  unavailable: list[dict[str, Any]] = []
  deep_counts: dict[str, int] = {}
  seen: set[str] = set()

  for file_report in files:
    if not isinstance(file_report, Mapping):
      raise BridgeDecodeError("PMD file report must be an object")
    path = _path(file_report.get("filename"), workspace)
    if path not in requested:
      raise BridgeDecodeError(f"PMD reported an unrequested file: {path}")
    seen.add(path)
    violations = file_report.get("violations")
    if not isinstance(violations, list):
      raise BridgeDecodeError("PMD violations must be a list")
    for violation in violations:
      if not isinstance(violation, Mapping):
        raise BridgeDecodeError("PMD violation must be an object")
      rule = violation.get("rule")
      if rule == "SlopscoutMetrics":
        decoded, missing = _decode_metric(violation, path)
        measurements.extend(decoded)
        unavailable.extend(missing)
      elif rule == "AvoidDeeplyNestedIfStmts":
        deep_counts[path] = deep_counts.get(path, 0) + 1
        measurements.append(_measurement(
            "deeply_nested_if", "pmd-v1", path, "expression", 1,
            "if", _location(violation), {},
        ))
      else:
        raise BridgeDecodeError(f"unexpected PMD rule in bridge report: {rule!r}")

  processing_errors = report.get("processingErrors", [])
  configuration_errors = report.get("configurationErrors", [])
  if not isinstance(processing_errors, list) or not isinstance(configuration_errors, list):
    raise BridgeDecodeError("PMD errors must be lists")
  diagnostics = [
      {"level": "error", "kind": "pmd_processing", "detail": item}
      for item in processing_errors
  ] + [
      {"level": "error", "kind": "pmd_configuration", "detail": item}
      for item in configuration_errors
  ]

  # The custom rule emits at least a type observation for ordinary source files.
  # Package-info/module-info and parse-failed files may legitimately lack one;
  # coverage remains explicit instead of fabricating a zero measurement.
  coverage = []
  for path in sorted(requested):
    state = "failed" if diagnostics else ("complete" if path in seen else "unavailable")
    coverage.append({
        "type": "coverage",
        "language": "java",
        "path": path,
        "state": state,
        "reason": None if state == "complete" else "PMD emitted no complete file report",
    })

  return {
      "measurements": measurements,
      "unavailable": unavailable,
      "coverage": coverage,
      "diagnostics": diagnostics,
      "deeply_nested_if_counts": deep_counts,
  }
