"""Bounded, expiring measurement waivers."""

from __future__ import annotations

import fnmatch
from dataclasses import dataclass
from datetime import date
from decimal import Decimal
from enum import Enum
from functools import lru_cache
from typing import Any, Iterable, Mapping

from .canonical import decimal_value
from .components import ComponentCatalog
from .errors import WaiverError
from .model import validate_identifier


@dataclass(frozen=True)
class Waiver:
  waiver_id: str
  path: str
  component_id: str
  reason: str
  allow_up_to: Decimal
  language: str | None = None
  expires: date | None = None

  def __post_init__(self) -> None:
    try:
      validate_identifier(self.waiver_id, "waiver id")
    except ValueError as error:
      raise WaiverError(str(error)) from error
    _validate_glob(self.path)
    if not isinstance(self.component_id, str) or not self.component_id:
      raise WaiverError("waiver component is required")
    if not isinstance(self.reason, str) or not self.reason.strip():
      raise WaiverError("waiver reason is required")
    if self.language is not None:
      try:
        validate_identifier(self.language, "waiver language")
      except ValueError as error:
        raise WaiverError(str(error)) from error
    try:
      ceiling = decimal_value(self.allow_up_to, "waiver allow_up_to")
    except ValueError as error:
      raise WaiverError(str(error)) from error
    if ceiling < 0:
      raise WaiverError("waiver allow_up_to cannot be negative")
    if self.expires is not None and not isinstance(self.expires, date):
      raise WaiverError("waiver expires must be a date")
    object.__setattr__(self, "allow_up_to", ceiling)
    object.__setattr__(self, "reason", self.reason.strip())

  @property
  def expired(self) -> bool:
    return self.expires is not None and self.expires < date.today()

  def is_expired(self, as_of: date) -> bool:
    return self.expires is not None and self.expires < as_of

  def matches(self, path: str, language: str, component_id: str) -> bool:
    return (self.component_id == component_id
            and (self.language is None or self.language == language)
            and path_glob_matches(path, self.path))


class WaiverStatus(str, Enum):
  APPLIED = "applied"
  REJECTED_CEILING = "rejected_ceiling"
  EXPIRED = "expired"


@dataclass(frozen=True)
class WaiverDisposition:
  waiver_id: str
  status: WaiverStatus
  path: str
  language: str
  component_id: str
  comparison_value: Decimal
  allow_up_to: Decimal
  subject_key: str | None = None
  reason: str = ""
  expires: date | None = None


def _validate_glob(pattern: str) -> None:
  if not isinstance(pattern, str) or not pattern or "\\" in pattern:
    raise WaiverError("waiver path must be a non-empty POSIX glob")
  if pattern.startswith(("/", "./")):
    raise WaiverError("waiver path must be workspace-relative")
  segments = pattern.split("/")
  if any(segment in ("", ".", "..") for segment in segments):
    raise WaiverError(
        f"waiver path escapes or is not canonical: {pattern!r}"
    )
  # Prevent drive-qualified selectors on every platform, even when validation
  # runs on POSIX.
  if ":" in segments[0]:
    raise WaiverError("waiver path cannot be drive-qualified")


@lru_cache(maxsize=512)
def _glob_parts(pattern: str) -> tuple[str, ...]:
  return tuple(pattern.split("/"))


def path_glob_matches(path: str, pattern: str) -> bool:
  """Match POSIX paths while giving ``**`` recursive semantics."""
  path_parts = tuple(path.split("/"))
  pattern_parts = _glob_parts(pattern)

  @lru_cache(maxsize=None)
  def match(path_index: int, pattern_index: int) -> bool:
    if pattern_index == len(pattern_parts):
      return path_index == len(path_parts)
    token = pattern_parts[pattern_index]
    if token == "**":
      return (match(path_index, pattern_index + 1)
              or (path_index < len(path_parts)
                  and match(path_index + 1, pattern_index)))
    return (path_index < len(path_parts)
            and fnmatch.fnmatchcase(path_parts[path_index], token)
            and match(path_index + 1, pattern_index + 1))

  return match(0, 0)


def parse_waivers(raw_waivers: Iterable[Mapping[str, Any]],
                  catalog: ComponentCatalog) -> tuple[Waiver, ...]:
  results: list[Waiver] = []
  seen: set[str] = set()
  allowed = {"id", "path", "language", "component", "allow_up_to",
             "reason", "expires"}
  for index, raw in enumerate(raw_waivers):
    if not isinstance(raw, Mapping):
      raise WaiverError(f"waivers[{index}] must be a table")
    unknown = set(raw).difference(allowed)
    if unknown:
      raise WaiverError(
          f"waivers[{index}] has unknown fields: {sorted(unknown)}"
      )
    waiver_id = raw.get("id")
    try:
      validate_identifier(waiver_id, f"waivers[{index}].id")
    except (ValueError, TypeError) as error:
      raise WaiverError(str(error)) from error
    if waiver_id in seen:
      raise WaiverError(f"duplicate waiver ID: {waiver_id}")
    seen.add(waiver_id)
    path = raw.get("path")
    _validate_glob(path)
    component_id = raw.get("component")
    if not isinstance(component_id, str):
      raise WaiverError(f"waivers[{index}].component is required")
    try:
      descriptor = catalog.get(component_id)
    except ValueError as error:
      raise WaiverError(str(error)) from error
    language = raw.get("language")
    if language is not None:
      if language not in catalog.languages:
        raise WaiverError(f"unknown waiver language: {language}")
      if not descriptor.support_for(language).configurable:
        raise WaiverError(
            f"component {component_id} is not supported for {language}"
        )
    reason = raw.get("reason")
    if not isinstance(reason, str) or not reason.strip():
      raise WaiverError(f"waivers[{index}].reason is required")
    if "allow_up_to" not in raw:
      raise WaiverError(f"waivers[{index}].allow_up_to is required")
    try:
      allow_up_to = decimal_value(raw["allow_up_to"], "waiver allow_up_to")
    except ValueError as error:
      # TOML decimals arrive as floats. Converting through their source-like
      # shortest representation avoids carrying binary approximation forward.
      value = raw["allow_up_to"]
      if isinstance(value, float):
        try:
          allow_up_to = Decimal(str(value))
        except Exception as nested:
          raise WaiverError(str(error)) from nested
      else:
        raise WaiverError(str(error)) from error
    if not allow_up_to.is_finite() or allow_up_to < 0:
      raise WaiverError("waiver allow_up_to must be finite and non-negative")
    expiry = raw.get("expires")
    if isinstance(expiry, str):
      try:
        expiry = date.fromisoformat(expiry)
      except ValueError as error:
        raise WaiverError(f"invalid waiver expiry date: {expiry!r}") from error
    if expiry is not None and not isinstance(expiry, date):
      raise WaiverError("waiver expires must be an ISO date")
    results.append(Waiver(
        waiver_id=waiver_id,
        path=path,
        component_id=component_id,
        reason=reason.strip(),
        allow_up_to=allow_up_to,
        language=language,
        expires=expiry,
    ))
  return tuple(sorted(results, key=lambda item: item.waiver_id))
