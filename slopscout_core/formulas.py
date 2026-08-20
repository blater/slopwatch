"""Deterministic contribution formulas and compound evaluator registry."""

from __future__ import annotations

from decimal import Decimal, InvalidOperation, localcontext
import math
from types import MappingProxyType
from typing import Callable, Mapping

from .errors import ScoringError
from .model import Measurement
from .profile import ComponentPolicy


SCORE_QUANTUM = Decimal("0.000000000001")
MAX_SCORE_EXPONENT = 1000
CompoundEvaluator = Callable[[Measurement, ComponentPolicy], Decimal]


def round_score(value: Decimal) -> Decimal:
  if not value.is_finite() or value < 0:
    raise ScoringError("formula produced a negative or non-finite contribution")
  if value == 0:
    return Decimal(0)
  if value.adjusted() > MAX_SCORE_EXPONENT:
    raise ScoringError("score contribution overflowed the supported range")
  precision = max(50, value.adjusted() + 20)
  try:
    with localcontext() as context:
      context.prec = precision
      return value.quantize(SCORE_QUANTUM)
  except InvalidOperation as error:
    raise ScoringError("score contribution could not be rounded") from error


def decimal_log2(value: Decimal) -> Decimal:
  if value <= 0 or not value.is_finite():
    raise ScoringError("log2 input must be positive and finite")
  with localcontext() as context:
    context.prec = 60
    context.Emax = 999_999_999
    context.Emin = -999_999_999
    return value.ln() / Decimal(2).ln()


def scalar_contribution(formula: str, value: Decimal,
                        policy: ComponentPolicy) -> Decimal:
  threshold = policy.threshold
  if formula == "binary":
    result = policy.weight if value > 0 else Decimal(0)
  elif formula == "count":
    result = policy.weight * value
  elif formula == "log-count":
    result = policy.weight * decimal_log2(Decimal(1) + value)
  elif formula in {"log-ratio", "linear-over-threshold"}:
    if threshold is None or threshold <= 0:
      raise ScoringError(f"formula {formula} requires a positive threshold")
    if value < threshold:
      result = Decimal(0)
    elif formula == "linear-over-threshold":
      result = policy.weight * value / threshold
    else:
      result = policy.weight * (
          Decimal(1) + decimal_log2(value / threshold)
      )
  else:
    raise ScoringError(f"unknown contribution formula: {formula}")
  return round_score(result)


class FormulaRegistry:
  """Registry for named, versioned compound formulas only."""

  def __init__(self, compound: Mapping[str, CompoundEvaluator] | None = None):
    self._compound = MappingProxyType(dict(compound or {}))

  @property
  def compound(self) -> Mapping[str, CompoundEvaluator]:
    return self._compound

  def evaluate_compound(self, formula: str, measurement: Measurement,
                        policy: ComponentPolicy) -> Decimal:
    try:
      evaluator = self._compound[formula]
    except KeyError as error:
      raise ScoringError(
          f"compound formula is not registered: {formula}"
      ) from error
    return round_score(evaluator(measurement, policy))


def _attribute_decimal(measurement: Measurement, name: str) -> Decimal:
  value = measurement.attributes.get(name)
  if isinstance(value, bool) or not isinstance(value, (int, float, Decimal)):
    raise ScoringError(
        f"compound measurement {measurement.component_id} requires numeric "
        f"attribute {name!r}"
    )
  if isinstance(value, float):
    if not math.isfinite(value):
      raise ScoringError(f"compound attribute {name!r} must be finite")
    result = Decimal(str(value))
  else:
    result = Decimal(value)
  if not result.is_finite() or result < 0:
    raise ScoringError(f"compound attribute {name!r} must be non-negative")
  return result


def god_class_pmd_v1(measurement: Measurement,
                     policy: ComponentPolicy) -> Decimal:
  """Legacy PMD God Class conjunction, evaluated once per qualifying type."""
  wmc = _attribute_decimal(measurement, "wmc")
  atfd = _attribute_decimal(measurement, "atfd")
  if "tcc_percent" in measurement.attributes:
    tcc = _attribute_decimal(measurement, "tcc_percent")
  else:
    # The structured Java bridge exposes PMD's TCC ratio in the range 0..1.
    tcc = _attribute_decimal(measurement, "tcc") * Decimal(100)
  # PMD's God Class rule is a conjunction, not a score for every type whose
  # component happens to be requested.
  if wmc < 47 or atfd <= 5 or tcc >= Decimal(100) / Decimal(3):
    return Decimal(0)
  atfd_threshold = Decimal(5)
  tcc_threshold = Decimal(100) / Decimal(3)
  atfd_severity = Decimal(1) + decimal_log2(
      max(atfd, atfd_threshold) / atfd_threshold
  )
  bounded_tcc = max(min(tcc, tcc_threshold), Decimal(1))
  tcc_severity = Decimal(1) + decimal_log2(tcc_threshold / bounded_tcc)
  # Weight is an explicit component-level multiplier. Its standard value is 1.
  return policy.weight * (
      Decimal(30) + Decimal(8) * atfd_severity
      + Decimal(8) * tcc_severity
  )


DEFAULT_FORMULA_REGISTRY = FormulaRegistry({
    "god-class/pmd-v1": god_class_pmd_v1,
})
