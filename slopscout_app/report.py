"""Stable serialization of language-neutral score results."""

from __future__ import annotations

from decimal import Decimal, InvalidOperation
from typing import Any, Iterable, Mapping

from slopscout_core import ScoreResult


def _json_number(value: Decimal) -> int | float:
  return int(value) if value == value.to_integral_value() else float(value)


def report_document(result: ScoreResult, *, calibrated: bool,
                    diagnostics: Iterable[Mapping[str, Any]] = (),
                    execution_plans: Iterable[Mapping[str, Any]] = (),
                    configuration: str | None = None,
                    limit: int = 0,
                    pass_score: float | int | Decimal | None = None) -> dict[str, Any]:
  if limit < 0:
    raise ValueError("limit must be non-negative")
  try:
    threshold = None if pass_score is None else Decimal(str(pass_score))
  except InvalidOperation as error:
    raise ValueError("pass_score must be a finite non-negative number") from error
  if threshold is not None and (not threshold.is_finite() or threshold < 0):
    raise ValueError("pass_score must be a finite non-negative number")
  files = []
  for rank, item in enumerate(result.files, 1):
    file_document = {
        "rank": rank,
        "path": item.path,
        "language": item.language,
        "score": _json_number(item.total_score),
        "observed_score": _json_number(item.observed_score),
        "complete": item.complete,
        "valid_zero_score": item.valid_zero_score,
        "axes": {key: _json_number(value) for key, value in sorted(item.axes.items())},
        "observed_axes": {
            key: _json_number(value) for key, value in sorted(item.observed_axes.items())
        },
        "coverage": {key: value.value for key, value in sorted(item.coverage.items())},
        "components": {
            component_id: {
                "contribution": _json_number(component.contribution),
                "observed_contribution": _json_number(component.observed_contribution),
                "observations": component.observations,
                "deduplicated_observations": component.deduplicated_observations,
                "subjects": [
                    {"subject": subject.subject_key,
                     "value": _json_number(subject.value),
                     "contribution": _json_number(subject.contribution)}
                    for subject in component.subjects
                ],
                "waivers": [
                    {"waiver_id": waiver.waiver_id, "status": waiver.status.value,
                     "observed": _json_number(waiver.comparison_value),
                     "allow_up_to": _json_number(waiver.allow_up_to),
                     "subject": waiver.subject_key,
                     "reason": waiver.reason,
                     "expires": None if waiver.expires is None else waiver.expires.isoformat()}
                    for waiver in component.waivers
                ],
            }
            for component_id, component in sorted(item.components.items())
        },
    }
    if threshold is not None:
      file_document["passed"] = item.complete and item.total_score <= threshold
    files.append(file_document)
  selected = files if limit == 0 else files[:limit]
  summary = {
      "files_analyzed": len(files),
      "complete_files": sum(item["complete"] for item in files),
  }
  if threshold is not None:
    summary.update({
        "pass_score": _json_number(threshold),
        "passed": all(item["passed"] for item in files),
        "failed_files": sum(not item["passed"] for item in files),
    })
  return {
      "schema_version": 3,
      "configuration": configuration,
      "profile_set_hash": result.profile_set_hash,
      "calibrated": calibrated,
      "summary": summary,
      "returned_files": len(selected),
      "truncated": len(selected) < len(files),
      "files": selected,
      "diagnostics": list(diagnostics),
      "execution_plans": list(execution_plans),
  }
