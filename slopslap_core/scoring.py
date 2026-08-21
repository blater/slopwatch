"""Definition-driven scoring, deduplication, coverage, and waivers."""

from __future__ import annotations

from collections import defaultdict
from dataclasses import dataclass
from datetime import date
from decimal import Decimal
from types import MappingProxyType
from typing import Any, Iterable, Mapping, Sequence

from .canonical import canonical_json, decimal_value
from .components import (
    Aggregator,
    ComponentCatalog,
    ComponentDescriptor,
    MeasurementKind,
    NumericType,
)
from .errors import ScoringError
from .formulas import (
    DEFAULT_FORMULA_REGISTRY,
    FormulaRegistry,
    round_score,
    scalar_contribution,
)
from .model import Coverage, CoverageState, Measurement, Scope
from .profile import CompletenessPolicy, ComponentPolicy, ProfileSet
from .waiver import Waiver, WaiverDisposition, WaiverStatus


@dataclass(frozen=True)
class SubjectContribution:
  subject_key: str
  value: Decimal
  contribution: Decimal


@dataclass(frozen=True)
class ComponentScore:
  component_id: str
  axis: str
  contribution: Decimal
  observed_contribution: Decimal
  observations: int
  deduplicated_observations: int
  subjects: tuple[SubjectContribution, ...]
  waivers: tuple[WaiverDisposition, ...]


@dataclass(frozen=True)
class FileScore:
  path: str
  language: str
  axes: Mapping[str, Decimal]
  observed_axes: Mapping[str, Decimal]
  components: Mapping[str, ComponentScore]
  total_score: Decimal
  observed_score: Decimal
  coverage: Mapping[str, CoverageState]
  complete: bool

  def __post_init__(self) -> None:
    object.__setattr__(self, "axes", MappingProxyType(dict(self.axes)))
    object.__setattr__(self, "observed_axes",
                       MappingProxyType(dict(self.observed_axes)))
    object.__setattr__(self, "components",
                       MappingProxyType(dict(self.components)))
    object.__setattr__(self, "coverage",
                       MappingProxyType(dict(self.coverage)))

  @property
  def valid_zero_score(self) -> bool:
    return self.complete and self.total_score == 0


@dataclass(frozen=True)
class ScoreResult:
  files: tuple[FileScore, ...]
  profile_set_hash: str


def _measurement_decimal(measurement: Measurement) -> Decimal:
  try:
    return decimal_value(measurement.value, "measurement value")
  except ValueError as error:
    raise ScoringError(str(error)) from error


def _location_key(measurement: Measurement) -> tuple[int, int, int, int] | None:
  if measurement.subject is None:
    return None
  location = measurement.subject.location
  return (location.start_line, location.start_column,
          location.end_line, location.end_column)


def _dedup_token(measurement: Measurement, token: str) -> Any:
  if token == "path":
    return measurement.path
  if token == "language":
    return measurement.language
  if token == "scope":
    return measurement.scope.value
  if token == "value":
    return _measurement_decimal(measurement)
  if token == "subject.location":
    return _location_key(measurement)
  if token == "subject.name":
    return None if measurement.subject is None else measurement.subject.name
  if token == "subject.symbol":
    return None if measurement.subject is None else measurement.subject.symbol
  if token == "provenance.rule":
    return measurement.provenance.rule
  if token.startswith("attributes."):
    name = token.removeprefix("attributes.")
    if name not in measurement.attributes:
      raise ScoringError(
          f"measurement {measurement.component_id} lacks dedup attribute {name}"
      )
    return measurement.attributes[name]
  raise ScoringError(f"unsupported descriptor deduplication token: {token}")


def _subject_key(measurement: Measurement) -> str:
  if measurement.subject is None:
    return measurement.path
  location = measurement.subject.location
  symbol = measurement.subject.symbol or measurement.subject.name
  return (f"{symbol}@{location.start_line}:{location.start_column}-"
          f"{location.end_line}:{location.end_column}")


def _deduplicate(measurements: Sequence[Measurement],
                 descriptor: ComponentDescriptor) -> list[Measurement]:
  if not descriptor.deduplication_key:
    return list(measurements)
  deduplicated: dict[str, Measurement] = {}
  for measurement in measurements:
    key = canonical_json(tuple(
        _dedup_token(measurement, token)
        for token in descriptor.deduplication_key
    ))
    existing = deduplicated.get(key)
    if existing is not None:
      if descriptor.measurement_kind is not MeasurementKind.COUNT \
          and _measurement_decimal(existing) != _measurement_decimal(measurement):
        raise ScoringError(
            f"conflicting duplicate observations for {descriptor.component_id} "
            f"at {_subject_key(measurement)}"
        )
      continue
    deduplicated[key] = measurement
  return [deduplicated[key] for key in sorted(deduplicated)]


def _aggregate(values: Sequence[Decimal], aggregator: Aggregator) -> Decimal:
  if not values:
    return Decimal(0)
  if aggregator is Aggregator.SUM:
    return round_score(sum(values, Decimal(0)))
  if aggregator is Aggregator.MAX:
    return max(values)
  raise ScoringError(f"unsupported aggregator: {aggregator}")


def _apply_cap(value: Decimal, cap: Decimal | None) -> Decimal:
  return value if cap is None else min(value, cap)


def _validate_measurement(measurement: Measurement,
                          descriptor: ComponentDescriptor,
                          catalog: ComponentCatalog) -> None:
  if measurement.definition_version != descriptor.definition_version:
    raise ScoringError(
        f"definition mismatch for {measurement.component_id}: expected "
        f"{descriptor.definition_version}, got {measurement.definition_version}"
    )
  if measurement.language not in catalog.languages \
      or not descriptor.support_for(measurement.language).configurable:
    raise ScoringError(
        f"component {measurement.component_id} is not supported for "
        f"{measurement.language}"
    )
  if measurement.scope not in descriptor.scopes:
    raise ScoringError(
        f"component {measurement.component_id} does not support scope "
        f"{measurement.scope.value}"
    )
  if measurement.scope is Scope.PROJECT:
    raise ScoringError(
        "project-scoped measurements belong in project diagnostics, "
        "not file scoring"
    )
  value = _measurement_decimal(measurement)
  if descriptor.numeric_type is NumericType.INTEGER \
      and value != value.to_integral_value():
    raise ScoringError(
        f"component {measurement.component_id} requires integer observations"
    )
  if not descriptor.value_constraint.accepts(value):
    raise ScoringError(
        f"measurement value {value} violates the constraints for "
        f"{measurement.component_id}"
    )


def _comparison_value(measurement: Measurement,
                      descriptor: ComponentDescriptor) -> Decimal:
  selector = descriptor.waiver_value
  if selector == "value":
    return _measurement_decimal(measurement)
  if selector.startswith("attributes."):
    attribute = selector.removeprefix("attributes.")
    if attribute not in measurement.attributes:
      raise ScoringError(
          f"measurement lacks waiver comparison attribute {attribute!r}"
      )
    try:
      return decimal_value(measurement.attributes[attribute],
                           f"waiver attribute {attribute}")
    except ValueError as error:
      raise ScoringError(str(error)) from error
  raise ScoringError(f"unsupported waiver comparison selector: {selector}")


def _matching_waivers(waivers: Sequence[Waiver], path: str, language: str,
                      component_id: str) -> tuple[Waiver, ...]:
  return tuple(waiver for waiver in waivers
               if waiver.matches(path, language, component_id))


def _waive_count(measurements: Sequence[Measurement],
                 matching: Sequence[Waiver], as_of: date,
                 path: str, language: str, component_id: str
                 ) -> tuple[list[Measurement], list[WaiverDisposition]]:
  if not measurements:
    return [], []
  count = Decimal(len(measurements))
  dispositions: list[WaiverDisposition] = []
  applied = False
  for waiver in matching:
    if waiver.is_expired(as_of):
      status = WaiverStatus.EXPIRED
    elif count <= waiver.allow_up_to:
      status = WaiverStatus.APPLIED
      applied = True
    else:
      status = WaiverStatus.REJECTED_CEILING
    dispositions.append(WaiverDisposition(
        waiver.waiver_id, status, path, language, component_id,
        count, waiver.allow_up_to, None, waiver.reason, waiver.expires,
    ))
  return ([] if applied else list(measurements), dispositions)


def _waive_subjects(measurements: Sequence[Measurement],
                    descriptor: ComponentDescriptor,
                    matching: Sequence[Waiver], as_of: date,
                    path: str, language: str
                    ) -> tuple[list[Measurement], list[WaiverDisposition]]:
  surviving: list[Measurement] = []
  dispositions: list[WaiverDisposition] = []
  for measurement in measurements:
    comparison = _comparison_value(measurement, descriptor)
    removed = False
    subject = _subject_key(measurement)
    for waiver in matching:
      if waiver.is_expired(as_of):
        status = WaiverStatus.EXPIRED
      elif comparison <= waiver.allow_up_to:
        status = WaiverStatus.APPLIED
        removed = True
      else:
        status = WaiverStatus.REJECTED_CEILING
      dispositions.append(WaiverDisposition(
          waiver.waiver_id, status, path, language,
          descriptor.component_id, comparison, waiver.allow_up_to, subject,
          waiver.reason, waiver.expires,
      ))
    if not removed:
      surviving.append(measurement)
  return surviving, dispositions


class ScoringEngine:
  def __init__(self, catalog: ComponentCatalog,
               formulas: FormulaRegistry = DEFAULT_FORMULA_REGISTRY) -> None:
    self.catalog = catalog
    self.formulas = formulas

  def _contribution(self, observations: Sequence[Measurement],
                    descriptor: ComponentDescriptor,
                    policy: ComponentPolicy
                    ) -> tuple[Decimal, tuple[SubjectContribution, ...]]:
    if not observations:
      return Decimal(0), ()
    subjects: list[SubjectContribution] = []
    if descriptor.measurement_kind is MeasurementKind.COUNT:
      count = Decimal(len(observations))
      contribution = scalar_contribution(policy.formula, count, policy)
      subjects.append(SubjectContribution("deduplicated_count", count,
                                          contribution))
    else:
      values: list[Decimal] = []
      for observation in observations:
        value = _measurement_decimal(observation)
        if descriptor.measurement_kind is MeasurementKind.CONTINUOUS:
          contribution = scalar_contribution(policy.formula, value, policy)
        else:
          contribution = self.formulas.evaluate_compound(
              policy.formula, observation, policy
          )
        values.append(contribution)
        subjects.append(SubjectContribution(
            _subject_key(observation), value, contribution
        ))
      contribution = _aggregate(values, descriptor.aggregator)
    return round_score(_apply_cap(contribution, policy.cap)), tuple(subjects)

  def score(self, measurements: Iterable[Measurement], profiles: ProfileSet,
            coverage: Iterable[Coverage] | None = None,
            *, as_of: date | None = None) -> ScoreResult:
    observations = tuple(measurements)
    coverage_supplied = coverage is not None
    coverage_records = tuple(coverage or ())
    as_of = as_of or date.today()
    grouped: dict[tuple[str, str, str], list[Measurement]] = defaultdict(list)
    file_keys: set[tuple[str, str]] = set()
    for measurement in observations:
      try:
        descriptor = self.catalog.get(measurement.component_id)
      except ValueError as error:
        raise ScoringError(str(error)) from error
      _validate_measurement(measurement, descriptor, self.catalog)
      if measurement.language not in profiles.profiles:
        raise ScoringError(
            f"measurement language is not in the ProfileSet: "
            f"{measurement.language}"
        )
      grouped[(measurement.language, measurement.path,
               measurement.component_id)].append(measurement)
      file_keys.add((measurement.language, measurement.path))

    coverage_by_file: dict[tuple[str, str], dict[str, CoverageState]] = \
        defaultdict(dict)
    for item in coverage_records:
      if item.language not in profiles.profiles:
        raise ScoringError(
            f"coverage language is not in the ProfileSet: {item.language}"
        )
      try:
        descriptor = self.catalog.get(item.component_id)
      except ValueError as error:
        raise ScoringError(str(error)) from error
      if not descriptor.support_for(item.language).configurable:
        raise ScoringError(
            f"coverage component {item.component_id} is unsupported for "
            f"{item.language}"
        )
      if item.definition_version is not None \
          and item.definition_version != descriptor.definition_version:
        raise ScoringError(
            f"coverage definition mismatch for {item.component_id}: expected "
            f"{descriptor.definition_version}, got {item.definition_version}"
        )
      file_coverage = coverage_by_file[(item.language, item.path)]
      existing = file_coverage.get(item.component_id)
      if existing is not None and existing is not item.state:
        raise ScoringError(
            f"conflicting coverage for {item.path}/{item.component_id}"
        )
      file_coverage[item.component_id] = item.state
      file_keys.add((item.language, item.path))

    file_scores: list[FileScore] = []
    for language, path in sorted(file_keys):
      profile = profiles.for_language(language)
      reported_coverage = coverage_by_file.get((language, path), {})
      normalized_coverage: dict[str, CoverageState] = {}
      complete = True
      for component_id, policy in profile.components.items():
        if not policy.enabled:
          continue
        state = (reported_coverage.get(component_id)
                 if coverage_supplied else CoverageState.COMPLETE)
        if state is None:
          state = CoverageState.NOT_REQUESTED
        normalized_coverage[component_id] = state
        if state is not CoverageState.COMPLETE:
          complete = False

      component_scores: dict[str, ComponentScore] = {}
      axes: dict[str, Decimal] = defaultdict(Decimal)
      observed_axes: dict[str, Decimal] = defaultdict(Decimal)
      for component_id, policy in profile.components.items():
        if not policy.enabled:
          continue
        descriptor = self.catalog.get(component_id)
        raw = grouped.get((language, path, component_id), [])
        state = normalized_coverage[component_id]
        scoreable = (state is CoverageState.COMPLETE
                     or profile.completeness is CompletenessPolicy.ALLOW_PARTIAL)
        if not scoreable:
          deduplicated: list[Measurement] = []
        else:
          deduplicated = _deduplicate(raw, descriptor)
        observed_contribution, subjects = self._contribution(
            deduplicated, descriptor, policy
        )
        matching = _matching_waivers(
            profiles.waivers, path, language, component_id
        )
        if descriptor.measurement_kind is MeasurementKind.COUNT:
          remaining, dispositions = _waive_count(
              deduplicated, matching, as_of, path, language, component_id
          )
        else:
          remaining, dispositions = _waive_subjects(
              deduplicated, descriptor, matching, as_of, path, language
          )
        contribution, _ = self._contribution(
            remaining, descriptor, policy
        )
        component_score = ComponentScore(
            component_id=component_id,
            axis=descriptor.axis,
            contribution=contribution,
            observed_contribution=observed_contribution,
            observations=len(raw),
            deduplicated_observations=len(deduplicated),
            subjects=subjects,
            waivers=tuple(dispositions),
        )
        component_scores[component_id] = component_score
        axes[descriptor.axis] += contribution
        observed_axes[descriptor.axis] += observed_contribution
      axes = {axis: round_score(value) for axis, value in axes.items()}
      observed_axes = {axis: round_score(value)
                       for axis, value in observed_axes.items()}
      total = round_score(sum(axes.values(), Decimal(0)))
      observed_score = round_score(sum(observed_axes.values(), Decimal(0)))
      file_scores.append(FileScore(
          path=path,
          language=language,
          axes=axes,
          observed_axes=observed_axes,
          components=component_scores,
          total_score=total,
          observed_score=observed_score,
          coverage=normalized_coverage,
          complete=complete,
      ))
    file_scores.sort(key=lambda item: (-item.total_score, item.path,
                                       item.language))
    return ScoreResult(tuple(file_scores), profiles.profile_set_hash)
