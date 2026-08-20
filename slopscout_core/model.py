"""Immutable, language-neutral analysis observations and coverage."""

from __future__ import annotations

import re
import math
from dataclasses import dataclass, field
from decimal import Decimal
from enum import Enum
from pathlib import PurePosixPath
from typing import Any, Mapping

from .canonical import decimal_value, frozen_mapping
from .errors import ModelValidationError


_IDENTIFIER = re.compile(r"^[a-z][a-z0-9_-]*$")


def validate_identifier(value: str, field_name: str) -> None:
  if not isinstance(value, str) or not _IDENTIFIER.fullmatch(value):
    raise ModelValidationError(
        f"{field_name} must match {_IDENTIFIER.pattern}: {value!r}"
    )


def validate_relative_path(path: str, field_name: str = "path") -> None:
  if not isinstance(path, str) or not path or "\\" in path:
    raise ModelValidationError(f"{field_name} must be a non-empty POSIX path")
  candidate = PurePosixPath(path)
  if candidate.is_absolute() or path.startswith("./"):
    raise ModelValidationError(f"{field_name} must be workspace-relative")
  if any(part in ("", ".", "..") for part in path.split("/")):
    raise ModelValidationError(f"{field_name} is not canonical: {path!r}")


class Scope(str, Enum):
  PROJECT = "project"
  FILE = "file"
  TYPE = "type"
  FUNCTION = "function"
  EXPRESSION = "expression"


class CoverageState(str, Enum):
  COMPLETE = "complete"
  UNAVAILABLE = "unavailable"
  FAILED = "failed"
  NOT_REQUESTED = "not_requested"


@dataclass(frozen=True)
class SourceLocation:
  start_line: int
  start_column: int
  end_line: int
  end_column: int

  def __post_init__(self) -> None:
    values = (self.start_line, self.start_column, self.end_line,
              self.end_column)
    if any(isinstance(value, bool) or not isinstance(value, int)
           for value in values):
      raise ModelValidationError("source locations must contain integers")
    if self.start_line < 1 or self.end_line < 1:
      raise ModelValidationError("source lines are one-based")
    if self.start_column < 0 or self.end_column < 0:
      raise ModelValidationError("source columns cannot be negative")
    if (self.end_line, self.end_column) < (
        self.start_line, self.start_column
    ):
      raise ModelValidationError("source location end precedes its start")


@dataclass(frozen=True)
class Subject:
  name: str
  location: SourceLocation
  symbol: str | None = None

  def __post_init__(self) -> None:
    if not isinstance(self.name, str):
      raise ModelValidationError("subject name must be a string")
    if self.symbol is not None and not isinstance(self.symbol, str):
      raise ModelValidationError("subject symbol must be a string")


@dataclass(frozen=True)
class Provenance:
  analyzer: str
  analyzer_version: str
  rule: str

  def __post_init__(self) -> None:
    for field_name in ("analyzer", "analyzer_version", "rule"):
      value = getattr(self, field_name)
      if not isinstance(value, str) or not value.strip():
        raise ModelValidationError(f"provenance {field_name} is required")


@dataclass(frozen=True)
class Measurement:
  component_id: str
  definition_version: str
  language: str
  path: str
  scope: Scope
  value: int | Decimal
  subject: Subject | None
  provenance: Provenance
  attributes: Mapping[str, Any] = field(default_factory=dict)

  def __post_init__(self) -> None:
    validate_identifier(self.component_id, "component_id")
    if not isinstance(self.definition_version, str) \
        or not self.definition_version.strip():
      raise ModelValidationError("definition_version is required")
    validate_identifier(self.language, "language")
    validate_relative_path(self.path)
    if not isinstance(self.scope, Scope):
      try:
        object.__setattr__(self, "scope", Scope(self.scope))
      except (TypeError, ValueError) as error:
        raise ModelValidationError(f"invalid measurement scope: {self.scope}") \
            from error
    try:
      normalized = decimal_value(self.value, "measurement value")
    except ValueError as error:
      raise ModelValidationError(str(error)) from error
    if normalized < 0:
      raise ModelValidationError("measurement value cannot be negative")
    if isinstance(self.value, int):
      normalized_value: int | Decimal = self.value
    else:
      normalized_value = normalized
    object.__setattr__(self, "value", normalized_value)
    if not isinstance(self.provenance, Provenance):
      raise ModelValidationError("measurement provenance is required")
    if not isinstance(self.attributes, Mapping):
      raise ModelValidationError("measurement attributes must be a mapping")
    for key in self.attributes:
      if not isinstance(key, str):
        raise ModelValidationError("measurement attribute keys must be strings")
    def validate_json(item: Any, path: str) -> None:
      if item is None or isinstance(item, (str, bool, int)):
        return
      if isinstance(item, Decimal):
        if not item.is_finite():
          raise ModelValidationError(f"{path} must be finite")
        return
      if isinstance(item, float):
        if not math.isfinite(item):
          raise ModelValidationError(f"{path} must be finite")
        return
      if isinstance(item, Mapping):
        for child_key, child in item.items():
          if not isinstance(child_key, str):
            raise ModelValidationError(f"{path} keys must be strings")
          validate_json(child, f"{path}.{child_key}")
        return
      if isinstance(item, (list, tuple)):
        for index, child in enumerate(item):
          validate_json(child, f"{path}[{index}]")
        return
      raise ModelValidationError(
          f"{path} contains unsupported JSON value {type(item).__name__}"
      )
    validate_json(self.attributes, "measurement attributes")
    object.__setattr__(self, "attributes", frozen_mapping(self.attributes))


@dataclass(frozen=True)
class Coverage:
  analysis_unit_id: str
  component_id: str
  language: str
  path: str
  state: CoverageState
  detail: str | None = None
  definition_version: str | None = None

  def __post_init__(self) -> None:
    if not isinstance(self.analysis_unit_id, str) \
        or not self.analysis_unit_id.strip():
      raise ModelValidationError("coverage analysis_unit_id is required")
    validate_identifier(self.component_id, "component_id")
    validate_identifier(self.language, "language")
    validate_relative_path(self.path)
    if not isinstance(self.state, CoverageState):
      try:
        object.__setattr__(self, "state", CoverageState(self.state))
      except (TypeError, ValueError) as error:
        raise ModelValidationError(f"invalid coverage state: {self.state}") \
            from error
    if self.definition_version is not None \
        and (not isinstance(self.definition_version, str)
             or not self.definition_version.strip()):
      raise ModelValidationError("coverage definition_version cannot be empty")
