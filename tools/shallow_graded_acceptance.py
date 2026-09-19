#!/usr/bin/env python3
"""Run the reviewed SHALLOW graded benchmark through the packaged CLI.

The benchmark is intentionally an input to this program.  This runner never
changes source snapshots, expected ranges, or scoring configuration.  A case
failure is recorded and execution continues so that one report contains the
whole acceptance picture.
"""

from __future__ import annotations

import argparse
import hashlib
import json
import math
import os
from pathlib import Path, PurePosixPath
import subprocess
import tempfile
from typing import Any, Iterable


ROOT = Path(__file__).resolve().parents[1]
DEFAULT_SYNTHETIC = ROOT / "docs/evidence/shallow-v4/graded-benchmark.json"
DEFAULT_REAL = ROOT / "docs/evidence/shallow-v4/graded-real-benchmark.json"
METRIC = "module_shallowness"
LANGUAGES = {"go", "java", "typescript", "rust", "ts", "rs"}
TIMEOUT_SECONDS = 120
ZERO_REASONS = {"recognized_role", "lowest_range_supported", "conservative_uncertainty"}
APPROVED_STATUSES = {
    "independently-reviewed-frozen",
    "independently-reviewed-frozen-before-scoring-implementation",
}
GROUP_ALIASES = {"mid": "mixed", "medium": "mixed", "intermediate": "mixed"}


class BenchmarkError(Exception):
    """A malformed or unfrozen benchmark, rather than an analyzer result."""


def number(value: Any) -> bool:
    return isinstance(value, (int, float)) and not isinstance(value, bool) and math.isfinite(float(value))


def text(value: Any) -> str:
    return str(value) if value is not None else ""


def posix_path(value: Any) -> str:
    """Normalize report and manifest paths without accepting filesystem escapes."""
    raw = text(value).replace("\\", "/")
    while raw.startswith("./"):
        raw = raw[2:]
    return str(PurePosixPath(raw)) if raw else ""


def safe_relative(value: Any, label: str) -> Path:
    raw = text(value).replace("\\", "/")
    path = PurePosixPath(raw)
    if not raw or path.is_absolute() or ".." in path.parts:
        raise BenchmarkError(f"{label} must be a relative path: {value!r}")
    return Path(*path.parts)


def digest(path: Path) -> str:
    hasher = hashlib.sha256()
    with path.open("rb") as stream:
        for block in iter(lambda: stream.read(1024 * 1024), b""):
            hasher.update(block)
    return hasher.hexdigest()


def load_json(path: Path) -> Any:
    try:
        return json.loads(path.read_text(encoding="utf-8"))
    except (OSError, UnicodeError, json.JSONDecodeError) as exc:
        raise BenchmarkError(f"cannot read JSON {path}: {exc}") from exc


def choose_benchmark(explicit: Path | None) -> Path:
    if explicit:
        return explicit if explicit.is_absolute() else ROOT / explicit
    return DEFAULT_SYNTHETIC if DEFAULT_SYNTHETIC.exists() else DEFAULT_REAL


def verify_freeze_manifest(spec: dict[str, Any], manifest_path: Path) -> dict[str, Any]:
    """Verify a declared freeze digest, when the benchmark supplies one."""
    candidates: list[tuple[Any, Any]] = []
    for key in ("freeze_manifest", "manifest"):
        if key in spec:
            candidates.append((key, spec[key]))
    for key in ("freeze_manifest_path", "manifest_path"):
        if key in spec:
            candidates.append((key, {"path": spec[key], "sha256": spec.get(key.replace("_path", "_sha256"))}))
    if "freeze_manifest_sha256" in spec:
        candidates.append(("freeze_manifest_sha256", {"path": spec.get("freeze_manifest_path"), "sha256": spec["freeze_manifest_sha256"]}))
    if "manifest_sha256" in spec:
        candidates.append(("manifest_sha256", {"path": spec.get("manifest_path"), "sha256": spec["manifest_sha256"]}))
    if not candidates:
        return {"declared": False, "verified": False}

    # A plain manifest object is allowed to carry the digest directly.
    key, declaration = candidates[-1]
    if isinstance(declaration, str):
        declaration = {"path": None, "sha256": declaration}
    if not isinstance(declaration, dict):
        raise BenchmarkError(f"{key} must be an object with path and sha256")
    expected = declaration.get("sha256") or declaration.get("digest")
    relative = declaration.get("path") or declaration.get("file")
    if not expected or not relative:
        raise BenchmarkError(f"{key} must declare both path and sha256")
    relative_path = safe_relative(relative, f"{key}.path")
    path = (manifest_path.parent / relative_path).resolve()
    root = manifest_path.parent.resolve()
    if root != path and root not in path.parents:
        raise BenchmarkError(f"{key}.path escapes the benchmark directory: {relative!r}")
    if not path.is_file():
        raise BenchmarkError(f"freeze manifest is missing: {path}")
    actual = digest(path)
    if actual.lower() != text(expected).lower():
        raise BenchmarkError(f"freeze manifest digest mismatch for {relative}: expected {expected}, got {actual}")
    return {"declared": True, "verified": True, "path": str(relative), "sha256": actual}


def verify_case_shape(case: Any, index: int) -> dict[str, Any]:
    if not isinstance(case, dict):
        raise BenchmarkError(f"case {index} is not an object")
    case = dict(case)
    if "target" not in case and "path" in case:
        case["target"] = case["path"]
    for field in ("id", "language", "target", "files", "expected_range"):
        if field not in case:
            raise BenchmarkError(f"case {index} is missing {field}")
    if not isinstance(case["id"], str) or not case["id"]:
        raise BenchmarkError(f"case {index} has an invalid id")
    if case["language"] not in LANGUAGES:
        raise BenchmarkError(f"case {case['id']} has unsupported language {case['language']!r}")
    expected = case["expected_range"]
    if not isinstance(expected, list) or len(expected) != 2 or not all(number(v) for v in expected):
        raise BenchmarkError(f"case {case['id']} expected_range must contain two numbers")
    if float(expected[0]) < 0 or float(expected[1]) > 100 or expected[0] > expected[1]:
        raise BenchmarkError(f"case {case['id']} has an invalid expected_range")
    if not isinstance(case["files"], (dict, list)):
        raise BenchmarkError(f"case {case['id']} files must be an object or list")
    group = case.get("group") or case.get("kind")
    if group:
        case["group"] = GROUP_ALIASES.get(text(group).lower(), text(group).lower())
    return case


def source_entries(case: dict[str, Any], benchmark_path: Path) -> list[tuple[str, bytes, dict[str, Any]]]:
    """Return (workspace path, bytes, manifest metadata) for either schema form."""
    files = case["files"]
    if isinstance(files, dict):
        items: Iterable[tuple[Any, Any]] = files.items()
    else:
        items = ((item.get("path"), item) for item in files if isinstance(item, dict))
    entries: list[tuple[str, bytes, dict[str, Any]]] = []
    for raw_path, value in items:
        workspace_path = safe_relative(raw_path, f"case {case['id']} file path")
        metadata = value if isinstance(value, dict) else {"source": value}
        if "source" in metadata or "content" in metadata:
            source = metadata.get("source", metadata.get("content"))
            if not isinstance(source, str):
                raise BenchmarkError(f"case {case['id']} source for {raw_path} is not text")
            content = source.encode("utf-8")
        elif "snapshot" in metadata:
            snapshot = safe_relative(metadata["snapshot"], f"case {case['id']} snapshot")
            source_path = (benchmark_path.parent / snapshot).resolve()
            base = benchmark_path.parent.resolve()
            if base != source_path and base not in source_path.parents:
                raise BenchmarkError(f"case {case['id']} snapshot escapes manifest directory: {snapshot}")
            try:
                content = source_path.read_bytes()
            except OSError as exc:
                raise BenchmarkError(f"case {case['id']} snapshot missing for {raw_path}: {source_path}") from exc
        else:
            raise BenchmarkError(f"case {case['id']} file {raw_path} needs source or snapshot")
        declared = metadata.get("sha256")
        actual = hashlib.sha256(content).hexdigest()
        if declared and text(declared).lower() != actual:
            raise BenchmarkError(f"case {case['id']} hash mismatch for {raw_path}: expected {declared}, got {actual}")
        entries.append((str(workspace_path), content, metadata))
    if not entries:
        raise BenchmarkError(f"case {case['id']} has no files")
    return entries


def find_target(report: dict[str, Any], target: str) -> dict[str, Any] | None:
    wanted = posix_path(target)
    exact = [item for item in report.get("files", []) if posix_path(item.get("path")) == wanted]
    if len(exact) == 1:
        return exact[0]
    if "/" not in wanted:
        basename = [item for item in report.get("files", []) if PurePosixPath(posix_path(item.get("path"))).name == wanted]
        if len(basename) == 1:
            return basename[0]
    return None


def target_boundaries(report: dict[str, Any], metric: dict[str, Any], target: str) -> list[dict[str, Any]]:
    depth = report.get("depth") or {}
    ids = metric.get("depth_boundary_ids") or []
    selected = [depth[item] for item in ids if item in depth and isinstance(depth[item], dict)]
    if selected:
        return selected
    wanted = posix_path(target)
    return [item for item in depth.values() if isinstance(item, dict) and any(posix_path(path) == wanted for path in item.get("files", []))]


def nested_value(objects: Iterable[Any], keys: tuple[str, ...]) -> Any:
    pending = list(objects)
    while pending:
        obj = pending.pop(0)
        if isinstance(obj, dict):
            for key in keys:
                if key in obj and obj[key] not in (None, "", []):
                    return obj[key]
            pending.extend(obj.values())
        elif isinstance(obj, list):
            pending.extend(obj)
    return None


def collect_named(value: Any, keys: tuple[str, ...], values: list[Any], limit: int = 100) -> None:
    if len(values) >= limit:
        return
    if isinstance(value, dict):
        for key in keys:
            if key in value and value[key] not in (None, "", []):
                values.append(value[key])
        for child in value.values():
            collect_named(child, keys, values, limit)
    elif isinstance(value, list):
        for child in value:
            collect_named(child, keys, values, limit)


def has_material_limitation(value: Any) -> bool:
    """Recognize concrete unresolved work, excluding generic precision markers."""
    if isinstance(value, dict):
        return any(has_material_limitation(child) for child in value.values())
    if isinstance(value, list):
        return any(has_material_limitation(child) for child in value)
    if not isinstance(value, str):
        return False
    marker = value.lower()
    if marker == "unsupported_execution_alternatives":
        return False
    return any(token in marker for token in (
        "unresolved", "call_budget_limit", "source_limit", "unsupported_outcome",
        "unknown_burden", "unknown_effect", "dependency_limit",
    ))


def limitations(metric: dict[str, Any], boundaries: list[dict[str, Any]], report: dict[str, Any], target: str) -> list[Any]:
    values: list[Any] = []
    keys = ("limitations", "limitation", "uncertainty", "unresolved", "unresolved_reasons")
    collect_named(metric, keys, values)
    for boundary in boundaries:
        collect_named(boundary, keys, values)
    for diagnostic in report.get("diagnostics") or []:
        if not isinstance(diagnostic, dict):
            continue
        path = posix_path(diagnostic.get("path"))
        if path in ("", posix_path(target)):
            values.append({key: diagnostic[key] for key in ("severity", "code", "message") if key in diagnostic})
    return values[:50]


def score_sort_check(report: dict[str, Any], target_file: dict[str, Any]) -> tuple[bool, str | None]:
    files = report.get("files") or []
    numeric = [item for item in files if isinstance(item, dict) and number(item.get("score"))]
    if not number(target_file.get("score")):
        return False, "target SCORE is missing or non-numeric"
    for left, right in zip(numeric, numeric[1:]):
        if float(left["score"]) < float(right["score"]):
            return False, "report files are not sorted by descending SCORE"
    ranks = [item.get("rank") for item in numeric]
    if all(isinstance(rank, int) for rank in ranks) and ranks != list(range(1, len(ranks) + 1)):
        return False, "numeric SCORE ranks are not contiguous"
    target_rank = target_file.get("rank")
    if isinstance(target_rank, int) and numeric:
        expected_rank = next((index + 1 for index, item in enumerate(numeric) if item is target_file), None)
        if expected_rank is not None and target_rank != expected_rank:
            return False, f"target rank is {target_rank}, expected {expected_rank}"
    return True, None


def run_case(binary: Path, case: dict[str, Any], benchmark_path: Path) -> dict[str, Any]:
    result: dict[str, Any] = {
        "id": case["id"], "language": case["language"], "target": case["target"],
        "group": case.get("group"), "kind": case.get("kind"),
        "expected_range": case["expected_range"], "allow_not_applicable": bool(case.get("allow_not_applicable")),
        "exclude_from_semantic_numeric_coverage": bool(case.get("exclude_from_semantic_numeric_coverage")),
        "failures": [], "numeric": False, "not_applicable": False,
    }
    try:
        entries = source_entries(case, benchmark_path)
        with tempfile.TemporaryDirectory(prefix="shallow-graded-") as directory:
            workspace = Path(directory)
            for relative, content, _ in entries:
                path = workspace / relative
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_bytes(content)
            if case["language"] in {"go"} and not any(Path(path).name == "go.mod" for path, _, _ in entries):
                (workspace / "go.mod").write_text("module benchmark.local\n\ngo 1.20\n", encoding="utf-8")
                result["go_mod_auto"] = True
            command = [str(binary), "-format", "json", "."]
            try:
                process = subprocess.run(command, cwd=workspace, capture_output=True, text=True, timeout=TIMEOUT_SECONDS)
            except (OSError, subprocess.TimeoutExpired) as exc:
                result["failures"].append(f"CLI invocation failed: {exc}")
                return finish_case(result)
            if process.returncode != 0:
                result["failures"].append(process.stderr.strip() or f"CLI exited {process.returncode}")
                return finish_case(result)
            try:
                report = json.loads(process.stdout)
            except json.JSONDecodeError as exc:
                result["failures"].append(f"CLI did not return JSON: {exc}")
                return finish_case(result)
            return evaluate_report(case, report, result)
    except (BenchmarkError, OSError, UnicodeError) as exc:
        result["failures"].append(str(exc))
    return finish_case(result)


def evaluate_report(case: dict[str, Any], report: dict[str, Any], result: dict[str, Any] | None = None) -> dict[str, Any]:
    """Apply identical publication and band checks to isolated or workspace reports."""
    if result is None:
        result = {"id": case["id"], "language": case["language"], "target": case["target"],
                  "group": case.get("group"), "expected_range": case["expected_range"],
                  "failures": [], "numeric": False, "not_applicable": False,
                  "exclude_from_semantic_numeric_coverage": bool(case.get("exclude_from_semantic_numeric_coverage"))}
    result["report_schema_version"] = report.get("schema_version")
    target_file = find_target(report, case["target"])
    if target_file is None:
        result["failures"].append(f"target file {case['target']!r} is absent from report")
        return finish_case(result)
    metric = target_file.get("components", {}).get(METRIC)
    if not isinstance(metric, dict):
        result["failures"].append(f"target has no {METRIC} component")
        return finish_case(result)
    boundaries = target_boundaries(report, metric, case["target"])
    states = [item.get("state") for item in boundaries if item.get("state")]
    boundary_estimated = any(bool(item.get("estimated")) for item in boundaries)
    raw_metadata = [item.get("raw") for item in boundaries if isinstance(item.get("raw"), dict)]
    state = metric.get("depth_state") or (states[0] if states else None)
    estimated = bool(metric.get("depth_estimated")) or boundary_estimated
    raw_max = metric.get("raw_max")
    case_limitations = limitations(metric, boundaries, report, case["target"])
    uncertainty_values: list[Any] = []
    collect_named(metric, ("uncertain_burden", "unknown_burden"), uncertainty_values)
    for boundary in boundaries:
        collect_named(boundary, ("uncertain_burden", "unknown_burden"), uncertainty_values)
    material_uncertainty = any(has_material_limitation(value) for value in case_limitations) or any(
        number(value) and float(value) > 0 for value in uncertainty_values
    )
    explicit_na = state == "not_applicable" or "not_applicable" in states
    result.update({
        "raw_max": raw_max, "contribution": metric.get("contribution"),
        "score": target_file.get("score"), "observed_score": target_file.get("observed_score"),
        "rank": target_file.get("rank"), "depth_state": state, "estimated": estimated,
        "semantic_coverage": target_file.get("coverage", {}).get(METRIC),
        "boundaries": [{key: value for key, value in item.items() if key in ("id", "scope", "state", "estimated", "policy_revision", "inventory_fingerprint", "files", "raw")} for item in boundaries],
        "limitations": case_limitations,
        "zero_reason": nested_value([metric, *raw_metadata, *boundaries], ("zero_reason",)),
        "zero_basis": nested_value([metric, *raw_metadata, *boundaries], ("penalty_basis", "reason")),
        "support": nested_value([metric, *raw_metadata, *boundaries], ("support", "supports", "evidence", "obligations")),
    })
    result["material_uncertainty"] = material_uncertainty
    result["not_applicable"] = explicit_na
    result["numeric"] = number(raw_max)
    if result["numeric"]:
        if not 0 <= float(raw_max) <= 100:
            result["failures"].append(f"raw_max {raw_max!r} is outside 0..100")
        low, high = case["expected_range"]
        if float(raw_max) < float(low) or float(raw_max) > float(high):
            result["failures"].append(f"raw_max {raw_max} is outside expected range {case['expected_range']}")
        if float(raw_max) == 0 and not explicit_na:
            reason = result.get("zero_reason")
            category = (reason.get("category") or reason.get("kind") or reason.get("code")) if isinstance(reason, dict) else reason
            if category not in ZERO_REASONS:
                result["failures"].append(
                    "zero-valued SHALLOW requires explicit zero_reason category "
                    "recognized_role, lowest_range_supported, or conservative_uncertainty"
                )
    elif not (case.get("allow_not_applicable") and explicit_na):
        result["failures"].append("SHALLOW raw_max is missing or non-numeric")
    if case.get("allow_not_applicable") and not result["numeric"] and not explicit_na:
        result["failures"].append("allow_not_applicable requires an explicit not_applicable state")
    if not number(result.get("contribution")) and not explicit_na:
        result["failures"].append("SHALLOW contribution is missing or non-numeric")
    if (not number(result.get("score")) or not number(result.get("observed_score"))) and not explicit_na:
        result["failures"].append("target SCORE/observed_score is missing or non-numeric")
    sort_ok, sort_error = score_sort_check(report, target_file)
    result["score_sort_ok"] = sort_ok
    if sort_error:
        result["failures"].append(sort_error)
    result["report_diagnostics"] = report.get("diagnostics") or []
    return finish_case(result)


def finish_case(result: dict[str, Any]) -> dict[str, Any]:
    state = result.get("depth_state")
    reason = result.get("zero_reason")
    zero_category = (reason.get("category") or reason.get("kind") or reason.get("code")) if isinstance(reason, dict) else reason
    if result.get("not_applicable"):
        result["analysis_mode"] = "not_applicable"
    elif result.get("estimated"):
        result["analysis_mode"] = "estimated"
    elif state in {"measured", "complete"}:
        result["analysis_mode"] = "precise"
    elif state:
        result["analysis_mode"] = "failed_or_partial"
    else:
        result["analysis_mode"] = "unknown"
    result["zero_category"] = zero_category
    result["precise_complete"] = (
        result["analysis_mode"] == "precise"
        and result.get("semantic_coverage") == "complete"
        and zero_category != "conservative_uncertainty"
    )
    result["semantic_applicable"] = not result.get("not_applicable") and not result.get("exclude_from_semantic_numeric_coverage")
    result["semantic_numeric"] = result["semantic_applicable"] and result.get("numeric", False)
    result["semantic_complete"] = result["semantic_applicable"] and result["precise_complete"]
    result["passed"] = not result["failures"]
    result["behavioral_conformance"] = result["passed"]
    result["semantic_conformance"] = result["semantic_applicable"] and result["passed"]
    return result


def row_score(row: dict[str, Any]) -> float | None:
    value = row.get("raw_max")
    return float(value) if number(value) else None


def selected_cases(spec: dict[str, Any], substring: str | None) -> list[dict[str, Any]]:
    raw = spec.get("cases")
    if not isinstance(raw, list):
        raise BenchmarkError("benchmark cases must be an array")
    cases = [verify_case_shape(item, index) for index, item in enumerate(raw)]
    if substring:
        cases = [case for case in cases if substring in case["id"]]
    return cases


def reference_rows(spec: dict[str, Any], key: str) -> list[dict[str, Any]]:
    value = spec.get(key, [])
    if isinstance(value, list):
        return [item for item in value if isinstance(item, dict)]
    if isinstance(value, dict):
        return [{**item, "id": item.get("id", item_id)} if isinstance(item, dict) else {"id": item_id, "value": item} for item_id, item in value.items()]
    return []


def compare_pairs(rows: dict[str, dict[str, Any]], spec: dict[str, Any]) -> list[dict[str, Any]]:
    failures: list[dict[str, Any]] = []
    checks: list[dict[str, Any]] = []
    for comparison in reference_rows(spec, "comparisons"):
        lower, higher = comparison.get("lower"), comparison.get("higher")
        minimum = comparison.get("min_delta", 0)
        item = {"lower": lower, "higher": higher, "min_delta": minimum}
        left, right = rows.get(text(lower)), rows.get(text(higher))
        if not number(minimum) or left is None or right is None or row_score(left) is None or row_score(right) is None:
            item["passed"] = False
            item["failure"] = "comparison needs two numeric case results and numeric min_delta"
        else:
            delta = row_score(right) - row_score(left)
            item.update({"delta": delta, "passed": delta >= float(minimum)})
            if not item["passed"]:
                item["failure"] = f"delta {delta:g} is below required {minimum:g}"
        checks.append(item)
        if not item["passed"]:
            failures.append({"kind": "comparison", **item})
    for invariant in reference_rows(spec, "invariants"):
        baseline, variant = invariant.get("baseline"), invariant.get("variant")
        tolerance = invariant.get("tolerance", 0)
        item = {"baseline": baseline, "variant": variant, "tolerance": tolerance, "kind": invariant.get("kind", "invariance"), "no_increase": bool(invariant.get("no_increase"))}
        left, right = rows.get(text(baseline)), rows.get(text(variant))
        if not number(tolerance) or left is None or right is None or row_score(left) is None or row_score(right) is None:
            item["passed"] = False
            item["failure"] = "invariant needs two numeric case results and numeric tolerance"
        else:
            delta = row_score(right) - row_score(left)
            item.update({"delta": delta, "passed": abs(delta) <= float(tolerance)})
            if item["no_increase"] and delta > 0:
                item["passed"] = False
            if item["kind"] == "uncertainty" and item["no_increase"] and row_score(left) > float(tolerance) and row_score(right) == 0:
                item["passed"] = False
                item["failure"] = "uncertainty variant auto-zeroed a positive baseline"
            elif item["no_increase"] and delta > 0:
                item["failure"] = f"no_increase violated by positive delta {delta:g}"
            elif not item["passed"]:
                item["failure"] = f"delta {delta:g} exceeds tolerance {tolerance:g}"
        checks.append(item)
        if not item["passed"]:
            failures.append({"kind": "invariant", **item})
    return [{"checks": checks, "failures": failures}] if checks else []


def extract_baseline(path: Path) -> dict[str, dict[str, Any]]:
    data = load_json(path)
    rows = data.get("results") if isinstance(data, dict) else data
    if isinstance(rows, dict):
        rows = list(rows.values())
    if not isinstance(rows, list):
        raise BenchmarkError(f"baseline {path} has no results array")
    return {text(item.get("id")): item for item in rows if isinstance(item, dict) and item.get("id")}


def bands(values: Iterable[Any]) -> dict[str, int]:
    result = {"0": 0, "1-25": 0, "26-64": 0, "65-100": 0}
    for raw in values:
        if not number(raw):
            continue
        value = float(raw)
        if value == 0:
            result["0"] += 1
        elif value <= 25:
            result["1-25"] += 1
        elif value <= 64:
            result["26-64"] += 1
        elif value <= 100:
            result["65-100"] += 1
    return result


def language_summary(rows: list[dict[str, Any]]) -> dict[str, Any]:
    applicable = [row for row in rows if not row.get("not_applicable")]
    numeric = [row for row in applicable if row.get("numeric")]
    semantic_applicable = [row for row in applicable if not row.get("exclude_from_semantic_numeric_coverage")]
    semantic_numeric = [row for row in semantic_applicable if row.get("numeric")]
    excluded = [row for row in applicable if row.get("exclude_from_semantic_numeric_coverage")]
    estimated = [row for row in semantic_applicable if row.get("estimated")]
    uncertain = [row for row in semantic_applicable if row.get("material_uncertainty")]
    precise_complete = [row for row in semantic_applicable if row.get("precise_complete")]
    states: dict[str, int] = {}
    for row in rows:
        state = text(row.get("depth_state")) or "unknown"
        states[state] = states.get(state, 0) + 1
    semantic_states: dict[str, int] = {}
    for row in semantic_applicable:
        state = text(row.get("depth_state")) or "unknown"
        semantic_states[state] = semantic_states.get(state, 0) + 1
    return {
        "total": len(rows), "applicable": len(applicable), "numeric": len(numeric),
        "not_applicable": len(rows) - len(applicable),
        "numeric_coverage": (len(numeric) / len(applicable)) if applicable else None,
        "semantic_applicable": len(semantic_applicable),
        "semantic_numeric": len(semantic_numeric),
        "semantic_numeric_coverage": (len(semantic_numeric) / len(semantic_applicable)) if semantic_applicable else None,
        "excluded_from_semantic_numeric_coverage": len(excluded),
        "excluded_numeric": sum(bool(row.get("numeric")) for row in excluded),
        "estimated": len(estimated), "estimated_rate": (len(estimated) / len(semantic_applicable)) if semantic_applicable else None,
        "uncertain": len(uncertain), "uncertainty_rate": (len(uncertain) / len(semantic_applicable)) if semantic_applicable else None,
        "precise_complete": len(precise_complete),
        "semantic_complete_rate": (len(precise_complete) / len(semantic_applicable)) if semantic_applicable else None,
        "semantic_conformance": sum(bool(row.get("semantic_conformance")) for row in rows),
        "behavioral_conformance": sum(bool(row.get("behavioral_conformance")) for row in rows),
        "semantic_coverage": semantic_states, "all_state_distribution": states,
        "bands": bands(row.get("raw_max") for row in numeric),
        "failures": sum(bool(row.get("failures")) for row in rows),
    }


def distribution(rows: list[dict[str, Any]]) -> dict[str, Any]:
    by_language: dict[str, list[dict[str, Any]]] = {}
    for row in rows:
        by_language.setdefault(text(row.get("language")), []).append(row)
    return {language: language_summary(group) for language, group in sorted(by_language.items())}


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--binary", type=Path, default=ROOT / "build/slopmark")
    parser.add_argument("--output", type=Path, default=ROOT / "build/shallow-graded-acceptance.json")
    parser.add_argument("--baseline", type=Path, help="prior runner JSON for before/after distributions")
    parser.add_argument("--case", help="run cases whose id contains this substring")
    parser.add_argument("--benchmark", type=Path, help="benchmark manifest (defaults to synthetic, then real)")
    args = parser.parse_args()
    output = args.output if args.output.is_absolute() else ROOT / args.output
    payload: dict[str, Any] = {"benchmark": None, "results": [], "failures": [], "summary": {}}
    try:
        benchmark_path = choose_benchmark(args.benchmark).resolve()
        spec = load_json(benchmark_path)
        if not isinstance(spec, dict):
            raise BenchmarkError("benchmark root must be an object")
        payload["benchmark"] = str(benchmark_path)
        status = text(spec.get("status")).lower().replace("_", "-")
        if status not in APPROVED_STATUSES:
            raise BenchmarkError(
                f"benchmark status {spec.get('status')!r} is not explicitly independently-reviewed-frozen"
            )
        if spec.get("ranges_set_before_new_implementation") is False:
            raise BenchmarkError("benchmark ranges were not frozen before implementation")
        payload["freeze_manifest"] = verify_freeze_manifest(spec, benchmark_path)
        cases = selected_cases(spec, args.case)
        if not cases:
            raise BenchmarkError("no benchmark cases match --case" if args.case else "benchmark contains no cases")
        binary = args.binary if args.binary.is_absolute() else ROOT / args.binary
        binary = binary.resolve()
        if not binary.is_file() or not os.access(binary, os.X_OK):
            raise BenchmarkError(f"packaged CLI is not executable: {binary}")
        rows = [run_case(binary, case, benchmark_path) for case in cases]
        payload["results"] = rows
        row_map = {row["id"]: row for row in rows}
        comparison_results = compare_pairs(row_map, spec)
        if comparison_results:
            payload["comparisons"] = comparison_results[0]["checks"]
            payload["failures"].extend(comparison_results[0]["failures"])
        positive = [row for row, case in zip(rows, cases) if case.get("group") in {"high", "mixed"} and float(case["expected_range"][1]) > 0]
        applicable = [row for row in rows if not row.get("not_applicable")]
        if applicable and all(row.get("numeric") and float(row.get("raw_max")) == 0 for row in applicable) and positive:
            payload["failures"].append({"kind": "suite", "failure": "all applicable SHALLOW results are zero despite positive high/mixed fixtures"})
        baseline_rows: dict[str, dict[str, Any]] = {}
        if args.baseline:
            baseline_path = args.baseline if args.baseline.is_absolute() else ROOT / args.baseline
            baseline_rows = extract_baseline(baseline_path.resolve())
            before_after = []
            for row in rows:
                before = baseline_rows.get(row["id"])
                item = {"id": row["id"], "before": before.get("raw_max") if before else None, "after": row.get("raw_max")}
                if before is None:
                    item["failure"] = "baseline has no matching case"
                    payload["failures"].append({"kind": "baseline", **item})
                elif number(item["before"]) and number(item["after"]):
                    item["delta"] = float(item["after"]) - float(item["before"])
                before_after.append(item)
            payload["before_after"] = before_after
        after_distribution = distribution(rows)
        payload["summary"] = {"languages": after_distribution, "after": after_distribution}
        for language, summary in after_distribution.items():
            payload["summary"][language] = summary
        if baseline_rows:
            # Older before reports predate the semantic exclusion field. Carry
            # the frozen case declaration onto matching rows so before/after
            # summaries apply the same applicability denominator.
            before_rows = []
            for row in rows:
                before = baseline_rows.get(row["id"])
                if before is not None:
                    before_rows.append({**before, "exclude_from_semantic_numeric_coverage": row.get("exclude_from_semantic_numeric_coverage", False)})
            before_distribution = distribution(before_rows)
            payload["summary"]["before"] = before_distribution
            payload["baseline_complete"] = len(before_rows) == len(rows)
        payload["failures"].extend({"kind": "case", "id": row["id"], "failure": failure} for row in rows for failure in row.get("failures", []))
        payload["passed"] = not payload["failures"] and all(row.get("passed") for row in rows)
    except BenchmarkError as exc:
        payload["failures"].append({"kind": "benchmark", "failure": str(exc)})
        payload["passed"] = False
    output.parent.mkdir(parents=True, exist_ok=True)
    output.write_text(json.dumps(payload, indent=2, sort_keys=False) + "\n", encoding="utf-8")
    for row in payload.get("results", []):
        print(f"{'PASS' if row.get('passed') else 'FAIL'} {row.get('language')}/{row.get('id')}" + (": " + "; ".join(row.get("failures", [])) if row.get("failures") else ""), flush=True)
    print(f"Report: {output}")
    print(json.dumps(payload.get("summary", {}), indent=2))
    return 0 if payload.get("passed") else 1


if __name__ == "__main__":
    raise SystemExit(main())
