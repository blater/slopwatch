"""Versioned component definitions and their catalogue."""

from __future__ import annotations

import re
from dataclasses import dataclass, field
from decimal import Decimal
from enum import Enum
from types import MappingProxyType
from typing import Iterable, Mapping

from .canonical import decimal_value
from .errors import CatalogError
from .model import Scope, validate_identifier


class MeasurementKind(str, Enum):
  CONTINUOUS = "continuous"
  COUNT = "count"
  COMPOUND = "compound"


class NumericType(str, Enum):
  INTEGER = "integer"
  DECIMAL = "decimal"


class Aggregator(str, Enum):
  SUM = "sum"
  MAX = "max"


class Severity(str, Enum):
  ERROR = "error"
  WARNING = "warning"
  INFO = "info"


class EvidencePosture(str, Enum):
  ORACLE_ALIGNED = "oracle_aligned"
  ESTABLISHED = "established"
  SUPPORTED = "supported"
  EXPERIMENTAL = "experimental"


class SupportLevel(str, Enum):
  CONFORMANT = "conformant"
  SUPPORTED = "supported"
  BEST_EFFORT = "best_effort"
  NOT_APPLICABLE = "not_applicable"
  UNSUPPORTED = "unsupported"

  @property
  def configurable(self) -> bool:
    return self in {
        SupportLevel.CONFORMANT,
        SupportLevel.SUPPORTED,
        SupportLevel.BEST_EFFORT,
    }


@dataclass(frozen=True)
class NumericConstraint:
  minimum: Decimal = Decimal(0)
  maximum: Decimal | None = None

  def __post_init__(self) -> None:
    try:
      minimum = decimal_value(self.minimum, "constraint minimum")
      maximum = (None if self.maximum is None else
                 decimal_value(self.maximum, "constraint maximum"))
    except ValueError as error:
      raise CatalogError(str(error)) from error
    if maximum is not None and maximum < minimum:
      raise CatalogError("constraint maximum cannot be below minimum")
    object.__setattr__(self, "minimum", minimum)
    object.__setattr__(self, "maximum", maximum)

  def accepts(self, value: Decimal) -> bool:
    return value >= self.minimum and (
        self.maximum is None or value <= self.maximum
    )


@dataclass(frozen=True)
class DefaultComponentPolicy:
  enabled: bool
  severity: Severity
  weight: Decimal
  formula: str
  threshold: Decimal | None = None
  cap: Decimal | None = None

  def __post_init__(self) -> None:
    if not isinstance(self.severity, Severity):
      try:
        object.__setattr__(self, "severity", Severity(self.severity))
      except (TypeError, ValueError) as error:
        raise CatalogError(f"invalid default severity: {self.severity}") \
            from error
    try:
      weight = decimal_value(self.weight, "default weight")
      threshold = (None if self.threshold is None else
                   decimal_value(self.threshold, "default threshold"))
      cap = None if self.cap is None else decimal_value(self.cap, "default cap")
    except ValueError as error:
      raise CatalogError(str(error)) from error
    if weight < 0 or (threshold is not None and threshold <= 0) \
        or (cap is not None and cap < 0):
      raise CatalogError("default weight/cap must be non-negative and threshold positive")
    if not isinstance(self.formula, str) or not self.formula:
      raise CatalogError("default formula is required")
    object.__setattr__(self, "weight", weight)
    object.__setattr__(self, "threshold", threshold)
    object.__setattr__(self, "cap", cap)


_TAXONOMY = re.compile(r"^[a-z][a-z0-9_]*(?:\.[a-z][a-z0-9_]*)*$")


@dataclass(frozen=True)
class ComponentDescriptor:
  component_id: str
  definition_version: str
  axis: str
  taxonomy_path: str
  measurement_kind: MeasurementKind
  numeric_type: NumericType
  scopes: frozenset[Scope]
  deduplication_key: tuple[str, ...]
  aggregator: Aggregator
  default_policy: DefaultComponentPolicy
  allowed_formulas: frozenset[str]
  value_constraint: NumericConstraint
  support: Mapping[str, SupportLevel]
  required_capability: str
  evidence_posture: EvidencePosture
  documentation: str
  waiver_value: str = "value"
  threshold_constraint: NumericConstraint = NumericConstraint(Decimal(1))

  def __post_init__(self) -> None:
    enum_fields = (
        ("measurement_kind", MeasurementKind),
        ("numeric_type", NumericType),
        ("aggregator", Aggregator),
        ("evidence_posture", EvidencePosture),
    )
    for field_name, enum_type in enum_fields:
      value = getattr(self, field_name)
      if not isinstance(value, enum_type):
        try:
          object.__setattr__(self, field_name, enum_type(value))
        except (TypeError, ValueError) as error:
          raise CatalogError(f"invalid component {field_name}: {value}") \
              from error
    try:
      validate_identifier(self.component_id, "component_id")
      validate_identifier(self.axis, "axis")
    except ValueError as error:
      raise CatalogError(str(error)) from error
    if not self.definition_version.strip():
      raise CatalogError("component definition_version is required")
    if not _TAXONOMY.fullmatch(self.taxonomy_path):
      raise CatalogError(f"invalid taxonomy path: {self.taxonomy_path!r}")
    for field_name in ("required_capability", "documentation"):
      if not isinstance(getattr(self, field_name), str) \
          or not getattr(self, field_name).strip():
        raise CatalogError(f"component {field_name} is required")
    if not self.scopes:
      raise CatalogError("a component must support at least one scope")
    if self.default_policy.formula not in self.allowed_formulas:
      raise CatalogError("default formula is not in allowed_formulas")
    if self.default_policy.threshold is not None \
        and not self.threshold_constraint.accepts(
            self.default_policy.threshold
        ):
      raise CatalogError("default threshold violates its constraint")
    if self.measurement_kind is MeasurementKind.COUNT \
        and not self.deduplication_key:
      raise CatalogError("count components require a deduplication key")
    if self.measurement_kind is MeasurementKind.CONTINUOUS \
        and self.default_policy.threshold is None:
      raise CatalogError("continuous components require a default threshold")
    if self.numeric_type is NumericType.INTEGER:
      for number in (self.default_policy.threshold,
                     self.value_constraint.minimum,
                     self.value_constraint.maximum,
                     self.threshold_constraint.minimum,
                     self.threshold_constraint.maximum):
        if number is not None and number != number.to_integral_value():
          raise CatalogError("integer component constraints must be integral")
    normalized_support: dict[str, SupportLevel] = {}
    for language, level in self.support.items():
      try:
        validate_identifier(language, "language")
        normalized_support[language] = (level if isinstance(level, SupportLevel)
                                        else SupportLevel(level))
      except (ValueError, TypeError) as error:
        raise CatalogError(f"invalid support entry for {language!r}") from error
    if not normalized_support:
      raise CatalogError("component support metadata cannot be empty")
    if self.evidence_posture is EvidencePosture.EXPERIMENTAL \
        and self.default_policy.enabled and self.default_policy.weight > 0:
      raise CatalogError("experimental components must be opt-in or zero-weight")
    if SupportLevel.BEST_EFFORT in normalized_support.values() \
        and self.default_policy.enabled and self.default_policy.weight > 0:
      raise CatalogError("best-effort components must be opt-in or zero-weight")
    object.__setattr__(self, "support", MappingProxyType(normalized_support))
    object.__setattr__(self, "scopes", frozenset(self.scopes))
    object.__setattr__(self, "allowed_formulas", frozenset(self.allowed_formulas))
    object.__setattr__(self, "deduplication_key", tuple(self.deduplication_key))
    valid_tokens = {"path", "language", "scope", "value",
                    "subject.location", "subject.name", "subject.symbol",
                    "provenance.rule"}
    for token in self.deduplication_key:
      if token not in valid_tokens and not token.startswith("attributes."):
        raise CatalogError(f"invalid deduplication key token: {token}")
    if self.waiver_value != "value" \
        and not self.waiver_value.startswith("attributes."):
      raise CatalogError(f"invalid waiver comparison selector: {self.waiver_value}")

  def support_for(self, language: str) -> SupportLevel:
    return self.support.get(language, SupportLevel.UNSUPPORTED)


class ComponentCatalog:
  """Immutable registry used by validation, scoring, reports, and UIs."""

  def __init__(self, descriptors: Iterable[ComponentDescriptor],
               languages: Iterable[str]) -> None:
    language_set = frozenset(languages)
    if not language_set:
      raise CatalogError("catalogue must register at least one language")
    for language in language_set:
      try:
        validate_identifier(language, "language")
      except ValueError as error:
        raise CatalogError(str(error)) from error
    by_id: dict[str, ComponentDescriptor] = {}
    for descriptor in descriptors:
      if descriptor.component_id in by_id:
        raise CatalogError(
            f"duplicate component ID: {descriptor.component_id}"
        )
      missing = language_set.difference(descriptor.support)
      extra = set(descriptor.support).difference(language_set)
      if missing or extra:
        raise CatalogError(
            f"component {descriptor.component_id} support must cover exactly "
            f"the registered languages; missing={sorted(missing)}, "
            f"extra={sorted(extra)}"
        )
      by_id[descriptor.component_id] = descriptor
    self._languages = language_set
    self._descriptors = MappingProxyType(by_id)

  @property
  def languages(self) -> frozenset[str]:
    return self._languages

  @property
  def descriptors(self) -> Mapping[str, ComponentDescriptor]:
    return self._descriptors

  def get(self, component_id: str) -> ComponentDescriptor:
    try:
      return self._descriptors[component_id]
    except KeyError as error:
      raise CatalogError(f"unknown component ID: {component_id}") from error

  def for_language(self, language: str) -> tuple[ComponentDescriptor, ...]:
    if language not in self._languages:
      raise CatalogError(f"unknown language: {language}")
    return tuple(
        descriptor for descriptor in self._descriptors.values()
        if descriptor.support_for(language).configurable
    )

  def definition_versions(self) -> Mapping[str, str]:
    return MappingProxyType({
        component_id: descriptor.definition_version
        for component_id, descriptor in self._descriptors.items()
    })
