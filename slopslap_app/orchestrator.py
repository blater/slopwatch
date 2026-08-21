"""Language-neutral analysis orchestration over isolated analyzer processes."""

from __future__ import annotations

import shutil
import os
import subprocess
from dataclasses import dataclass
from pathlib import Path
from typing import Any, Iterable, Mapping

from slopslap_core import (
    Coverage, CoverageState, Measurement, Provenance, Scope, ScoringEngine,
    SourceLocation, Subject, balanced_profile, resolve_profile_set,
    standard_catalog,
)
from slopslap_runtime import AnalyzerProcessAdapter, AnalyzerRequest
from slopslap_runtime.errors import ValidationError as RuntimeValidationError
from slopslap_runtime.protocol import (
    ProtocolRecord, ProtocolUnit, RequestedComponent, decode_number,
)

from .registry import standard_provider_registry
from .dependencies import ensure_analyzer


_IGNORED_DIRECTORIES = frozenset({
    ".git", ".gradle", ".idea", ".mypy_cache", ".pytest_cache",
    ".ruff_cache", "build", "coverage", "dist", "node_modules", "out",
    "target", "vendor",
})
_TEST_DIRECTORIES = frozenset({
    "__tests__", "integration-test", "integration-tests", "integrationtest",
    "integrationtests", "spec", "specs", "test", "test-fixtures",
    "testfixtures", "tests",
})
_TYPESCRIPT_TEST_SUFFIXES = (
    ".spec.cts", ".spec.mts", ".spec.ts", ".spec.tsx",
    ".test.cts", ".test.mts", ".test.ts", ".test.tsx",
)


class AnalysisError(RuntimeError):
  """A requested provider cannot produce a valid analysis."""


@dataclass(frozen=True)
class AnalysisOutcome:
  scores: Any
  profiles: Any
  diagnostics: tuple[Mapping[str, Any], ...]
  execution_plans: tuple[Mapping[str, Any], ...]


def _is_test_source(path: Path, language: str) -> bool:
  if any(part.lower() in _TEST_DIRECTORIES for part in path.parts[:-1]):
    return True
  if language == "go":
    return path.name.lower().endswith("_test.go")
  if language == "typescript":
    return path.name.lower().endswith(_TYPESCRIPT_TEST_SUFFIXES)
  return False


def discover_sources(workspace: Path, targets: Iterable[Path] = (), *,
                     include_tests: bool = False) -> dict[str, tuple[str, ...]]:
  workspace = workspace.resolve()
  registry = standard_provider_registry()
  roots = tuple(targets) or (workspace,)
  grouped: dict[str, set[str]] = {}
  for raw in roots:
    target = raw if raw.is_absolute() else workspace / raw
    target = target.resolve(strict=True)
    try:
      target.relative_to(workspace)
    except ValueError as error:
      raise AnalysisError(f"analysis target escapes workspace: {target}") from error
    candidates = (target,) if target.is_file() else (
        item for item in target.rglob("*")
        if item.is_file() and not any(part in _IGNORED_DIRECTORIES
                                      for part in item.relative_to(workspace).parts)
    )
    for candidate in candidates:
      relative = candidate.relative_to(workspace).as_posix()
      try:
        language = registry.language_for_path(relative)
      except RuntimeValidationError:
        continue
      if not include_tests and _is_test_source(Path(relative), language):
        continue
      grouped.setdefault(language, set()).add(relative)
  return {language: tuple(sorted(paths)) for language, paths in sorted(grouped.items())}


def _location(subject: Mapping[str, Any]) -> SourceLocation:
  if all(key in subject for key in ("line", "column", "end_line", "end_column")):
    return SourceLocation(int(subject["line"]), int(subject["column"]),
                          int(subject["end_line"]), int(subject["end_column"]))
  start, end = subject.get("start"), subject.get("end")
  if isinstance(start, Mapping) and isinstance(end, Mapping):
    return SourceLocation(int(start["line"]), int(start["column"]),
                          int(end["line"]), int(end["column"]))
  raise AnalysisError("analyzer subject has no supported source location")


def _convert(records: Iterable[ProtocolRecord], request: AnalyzerRequest
             ) -> tuple[list[Measurement], list[Coverage], list[Mapping[str, Any]], list[Mapping[str, Any]]]:
  languages = {unit.unit_id: unit.language for unit in request.units}
  measurements: list[Measurement] = []
  coverage: list[Coverage] = []
  diagnostics: list[Mapping[str, Any]] = []
  plans: list[Mapping[str, Any]] = []
  for record in records:
    payload = record.payload
    if record.record_type == "measurement":
      raw_subject = payload["subject"]
      raw_provenance = payload["provenance"]
      measurements.append(Measurement(
          component_id=str(payload["component_id"]),
          definition_version=str(payload["definition_version"]),
          language=languages[str(payload["unit_id"])], path=str(payload["path"]),
          scope=Scope(str(payload["scope"])),
          value=decode_number(payload["value"]),
          subject=Subject(str(raw_subject.get("name", "")), _location(raw_subject),
                          raw_subject.get("symbol")),
          provenance=Provenance(
              str(raw_provenance["analyzer"]),
              str(raw_provenance["analyzer_version"]),
              str(raw_provenance["rule"]),
          ), attributes=payload["attributes"],
      ))
    elif record.record_type == "coverage":
      coverage.append(Coverage(
          analysis_unit_id=str(payload["unit_id"]),
          component_id=str(payload["component_id"]),
          language=languages[str(payload["unit_id"])], path=str(payload["path"]),
          state=CoverageState(str(payload["state"])),
          detail=str(payload["reason"]) or None,
          definition_version=str(payload["definition_version"]),
      ))
    elif record.record_type == "diagnostic":
      diagnostics.append(dict(payload))
    elif record.record_type == "execution_plan":
      plans.append(dict(payload))
  return measurements, coverage, diagnostics, plans


def _require_success(records: Iterable[ProtocolRecord], language: str) -> None:
  records = tuple(records)
  terminal = next(
      (record.payload for record in reversed(records)
       if record.record_type == "terminal"),
      None,
  )
  if terminal is None or terminal.get("status") == "success":
    return
  errors = [
      str(record.payload["message"]).strip()
      for record in records
      if record.record_type == "diagnostic"
      and record.payload.get("severity") == "error"
      and str(record.payload.get("message", "")).strip()
  ]
  message = errors[0] if errors else str(terminal.get("message", "")).strip()
  if not message:
    message = f"terminal status was {terminal.get('status')}"
  raise AnalysisError(f"{language} analyzer failed: {message}")


def _analyzer_environment(language: str) -> Mapping[str, str] | None:
  if language != "go":
    return None
  go = shutil.which("go")
  if go is None:
    raise AnalysisError("Go 1.22+ is required for Go semantic analysis")
  completed = subprocess.run(
      (go, "env", "GOROOT"), stdin=subprocess.DEVNULL,
      stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True,
      timeout=10, check=False,
  )
  root = Path(completed.stdout.strip())
  if completed.returncode != 0 or not root.is_absolute() or not root.is_dir():
    raise AnalysisError("the Go toolchain did not provide a usable GOROOT")
  return {"GOROOT": str(root.resolve())}


def analyze(workspace: Path, *, targets: Iterable[Path] = (),
            policy: Mapping[str, Any] | None = None,
            languages: Iterable[str] | None = None,
            include_tests: bool = False,
            backends: Mapping[str, str] | None = None,
            timeout_seconds: float = 120) -> AnalysisOutcome:
  workspace = workspace.resolve()
  discovered = discover_sources(workspace, targets, include_tests=include_tests)
  selected = tuple(dict.fromkeys(languages or discovered.keys()))
  if not selected:
    raise AnalysisError("no supported Go, Java, TypeScript, or Rust source files found")
  missing = [language for language in selected if language not in discovered]
  if missing:
    raise AnalysisError(f"no source files discovered for: {', '.join(missing)}")
  backend_overrides = dict(backends or {})
  irrelevant = sorted(set(backend_overrides).difference(selected))
  if irrelevant:
    raise AnalysisError(
        f"backend override has no selected source language: {', '.join(irrelevant)}"
    )
  analyzer_commands = {
      language: ensure_analyzer(language, backend=backend_overrides.get(language))
      for language in selected
  }
  catalog = standard_catalog()
  profiles = resolve_profile_set(
      catalog, balanced_profile(), selected, policy or {},
  )
  all_measurements: list[Measurement] = []
  all_coverage: list[Coverage] = []
  diagnostics: list[Mapping[str, Any]] = []
  plans: list[Mapping[str, Any]] = []
  registry = standard_provider_registry()
  process = AnalyzerProcessAdapter(timeout_seconds=timeout_seconds)
  invoked = 0
  for language in selected:
    enabled = profiles.for_language(language).enabled_components
    components = tuple(
        RequestedComponent(component_id, catalog.get(component_id).definition_version)
        for component_id in enabled
    )
    if not components:
      diagnostics.append({
          "type": "diagnostic", "severity": "info",
          "code": "language.no_enabled_components", "language": language,
          "message": f"{language} sources were discovered but its profile enables no components",
      })
      continue
    for component in components:
      if not registry.supports(language, component):
        raise AnalysisError(f"provider {language} does not support {component.component_id}")
    unit = ProtocolUnit(f"{language}-unit", language, discovered[language])
    options: dict[str, Any] = {}
    options["include_tests"] = include_tests
    if language == "typescript":
      options["typescript_types"] = "auto"
    elif language == "java":
      java = shutil.which("java")
      if java is not None:
        options["java_path"] = str(Path(java).resolve())
      if backend_overrides.get("java") == "pmd":
        pmd_home = os.environ.get("SLOPSLAP_PMD_HOME")
        if pmd_home:
          libraries = sorted(str(item.resolve())
                             for item in (Path(pmd_home) / "lib").glob("*")
                             if item.is_file())
          if libraries:
            options["pmd_classpath"] = libraries
    request = AnalyzerRequest.create(workspace, (unit,), components,
                                     options=options, limits={"max_seconds": int(timeout_seconds)})
    result = process.run(
        analyzer_commands[language], request,
        environment=_analyzer_environment(language),
    )
    invoked += 1
    _require_success(result.records, language)
    converted = _convert(result.records, request)
    all_measurements.extend(converted[0])
    all_coverage.extend(converted[1])
    diagnostics.extend(converted[2])
    plans.extend(converted[3])
  if invoked == 0:
    raise AnalysisError(
        "the selected language profiles enable no analysis components; "
        "enable an experimental Rust component in .slopslap.toml"
    )
  scores = ScoringEngine(catalog).score(all_measurements, profiles, all_coverage)
  return AnalysisOutcome(scores, profiles, tuple(diagnostics), tuple(plans))
