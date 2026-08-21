"""Load the standard component catalogue packaged beside the core code."""

from __future__ import annotations

import json
from decimal import Decimal
from pathlib import Path
from typing import Any, Mapping

from .components import (
    ComponentCatalog, ComponentDescriptor, DefaultComponentPolicy,
    NumericConstraint,
)
from .errors import CatalogError
from .model import Scope


CATALOG_PATH = Path(__file__).with_name("component-catalog.json")


def _mapping(value: Any, name: str) -> Mapping[str, Any]:
  if not isinstance(value, Mapping):
    raise CatalogError(f"{name} must be an object")
  return value


def _decimal(value: Any, name: str) -> Decimal | None:
  if value is None:
    return None
  try:
    return Decimal(str(value))
  except Exception as error:
    raise CatalogError(f"{name} must be a decimal or null") from error


def _constraint(value: Any, name: str) -> NumericConstraint:
  document = _mapping(value, name)
  if set(document) != {"minimum", "maximum"}:
    raise CatalogError(f"{name} must contain minimum and maximum")
  minimum = _decimal(document["minimum"], f"{name}.minimum")
  if minimum is None:
    raise CatalogError(f"{name}.minimum cannot be null")
  return NumericConstraint(minimum, _decimal(document["maximum"], f"{name}.maximum"))


def _descriptor(raw: Any, index: int) -> ComponentDescriptor:
  item = _mapping(raw, f"components[{index}]")
  expected = {
      "component_id", "definition_version", "axis", "taxonomy", "kind",
      "numeric_type", "scopes", "deduplication_key", "aggregator",
      "required_capability", "evidence_posture", "documentation",
      "allowed_formulas", "value_constraint", "threshold_constraint",
      "waiver_value", "support", "defaults",
  }
  if set(item) != expected:
    raise CatalogError(
        f"components[{index}] fields differ: "
        f"missing={sorted(expected.difference(item))}, "
        f"extra={sorted(set(item).difference(expected))}"
    )
  defaults = _mapping(item["defaults"], f"components[{index}].defaults")
  default_fields = {"enabled", "severity", "threshold", "weight", "formula", "cap"}
  if set(defaults) != default_fields:
    raise CatalogError(f"components[{index}].defaults has invalid fields")
  threshold = _decimal(defaults["threshold"], f"components[{index}].defaults.threshold")
  weight = _decimal(defaults["weight"], f"components[{index}].defaults.weight")
  if weight is None:
    raise CatalogError(f"components[{index}].defaults.weight cannot be null")
  return ComponentDescriptor(
      component_id=str(item["component_id"]),
      definition_version=str(item["definition_version"]),
      axis=str(item["axis"]),
      taxonomy_path=str(item["taxonomy"]),
      measurement_kind=str(item["kind"]),
      numeric_type=str(item["numeric_type"]),
      scopes=frozenset(Scope(str(value)) for value in item["scopes"]),
      deduplication_key=tuple(str(value) for value in item["deduplication_key"]),
      aggregator=str(item["aggregator"]),
      default_policy=DefaultComponentPolicy(
          defaults["enabled"], str(defaults["severity"]), weight,
          str(defaults["formula"]), threshold,
          _decimal(defaults["cap"], f"components[{index}].defaults.cap"),
      ),
      allowed_formulas=frozenset(str(value) for value in item["allowed_formulas"]),
      value_constraint=_constraint(item["value_constraint"],
                                   f"components[{index}].value_constraint"),
      support={str(language): str(level)
               for language, level in _mapping(item["support"],
                                                f"components[{index}].support").items()},
      required_capability=str(item["required_capability"]),
      evidence_posture=str(item["evidence_posture"]),
      documentation=str(item["documentation"]),
      waiver_value=str(item["waiver_value"]),
      threshold_constraint=_constraint(
          item["threshold_constraint"], f"components[{index}].threshold_constraint",
      ),
  )


def _load() -> tuple[dict[str, Any], ComponentCatalog]:
  try:
    document = json.loads(CATALOG_PATH.read_text(encoding="utf-8"))
  except (OSError, json.JSONDecodeError) as error:
    raise CatalogError(f"cannot load component catalogue {CATALOG_PATH}: {error}") from error
  root = _mapping(document, "component catalogue")
  if set(root) != {"schema", "profile", "languages", "analyzers", "components"}:
    raise CatalogError("component catalogue has invalid top-level fields")
  if root["schema"] != 1:
    raise CatalogError(f"unsupported component catalogue schema: {root['schema']!r}")
  if not isinstance(root["profile"], str) or not root["profile"].strip():
    raise CatalogError("component catalogue profile must be a non-empty string")
  if not isinstance(root["languages"], list) or not isinstance(root["analyzers"], list) \
      or not isinstance(root["components"], list):
    raise CatalogError("component catalogue languages, analyzers, and components must be arrays")
  languages = tuple(str(language) for language in root["languages"])
  descriptors = tuple(_descriptor(item, index)
                      for index, item in enumerate(root["components"]))
  return dict(document), ComponentCatalog(descriptors, languages)


def catalog_document() -> dict[str, Any]:
  """Return the validated JSON document used by the UI and CLI."""
  document, _ = _load()
  return document


def standard_catalog() -> ComponentCatalog:
  """Return the typed catalogue used by validation and scoring."""
  _, catalog = _load()
  return catalog


LANGUAGES = frozenset(str(value) for value in catalog_document()["languages"])


def balanced_profile() -> "BuiltInProfile":
  from .profile import BuiltInProfile
  return BuiltInProfile(str(catalog_document()["profile"]))
