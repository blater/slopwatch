"""Canonical serialization and exact numeric helpers."""

from __future__ import annotations

import dataclasses
import hashlib
import json
import math
from datetime import date
from decimal import Decimal, InvalidOperation
from enum import Enum
from types import MappingProxyType
from typing import Any, Mapping


def decimal_value(value: object, field_name: str) -> Decimal:
  """Return a finite Decimal without accepting binary floating point."""
  if isinstance(value, bool) or isinstance(value, float):
    raise ValueError(f"{field_name} must be an integer or Decimal, not float")
  if isinstance(value, int):
    result = Decimal(value)
  elif isinstance(value, Decimal):
    result = value
  elif isinstance(value, str):
    try:
      result = Decimal(value)
    except InvalidOperation as error:
      raise ValueError(f"{field_name} is not a decimal number") from error
  else:
    raise ValueError(f"{field_name} must be an integer or Decimal")
  if not result.is_finite():
    raise ValueError(f"{field_name} must be finite")
  return result


def canonical_decimal(value: Decimal) -> str:
  """Represent equal Decimal values identically for provenance hashing."""
  if not value.is_finite():
    raise ValueError("non-finite decimals cannot be canonicalized")
  if value == 0:
    return "0"
  normalized = value.normalize()
  text = format(normalized, "f")
  if "." in text:
    text = text.rstrip("0").rstrip(".")
  return text


def canonical_data(value: Any) -> Any:
  """Convert domain data to a JSON-safe, type-stable canonical structure."""
  if dataclasses.is_dataclass(value):
    return {
        field.name: canonical_data(getattr(value, field.name))
        for field in dataclasses.fields(value)
        if not field.name.startswith("_")
    }
  if isinstance(value, Enum):
    return value.value
  if isinstance(value, Decimal):
    return {"$decimal": canonical_decimal(value)}
  if isinstance(value, float):
    if not math.isfinite(value):
      raise ValueError("non-finite floats cannot be canonicalized")
    return {"$decimal": canonical_decimal(Decimal(str(value)))}
  if isinstance(value, date):
    return value.isoformat()
  if isinstance(value, (Mapping, MappingProxyType)):
    return {
        str(key): canonical_data(item)
        for key, item in sorted(value.items(), key=lambda pair: str(pair[0]))
    }
  if isinstance(value, (tuple, list)):
    return [canonical_data(item) for item in value]
  if isinstance(value, (set, frozenset)):
    items = [canonical_data(item) for item in value]
    return sorted(items, key=lambda item: json.dumps(
        item, sort_keys=True, separators=(",", ":"), ensure_ascii=False
    ))
  if value is None or isinstance(value, (str, int, bool)):
    return value
  raise TypeError(f"cannot canonicalize {type(value).__name__}")


def canonical_json(value: Any) -> str:
  return json.dumps(
      canonical_data(value),
      ensure_ascii=False,
      sort_keys=True,
      separators=(",", ":"),
  )


def content_hash(value: Any) -> str:
  return hashlib.sha256(canonical_json(value).encode("utf-8")).hexdigest()


def frozen_mapping(value: Mapping[str, Any] | None) -> Mapping[str, Any]:
  """Recursively freeze JSON-like mappings used by immutable domain models."""
  def freeze(item: Any) -> Any:
    if isinstance(item, Mapping):
      return MappingProxyType({str(key): freeze(child)
                               for key, child in item.items()})
    if isinstance(item, list):
      return tuple(freeze(child) for child in item)
    if isinstance(item, tuple):
      return tuple(freeze(child) for child in item)
    return item

  return freeze(value or {})
