"""Machine-readable descriptions shared by CLI and configuration UI."""

from __future__ import annotations

from pathlib import Path
from typing import Any, Callable

from slopscout_core import (
    balanced_profile, catalog_document, resolve_profile_set, standard_catalog,
)

from .config import config_locations, discover_config
from .dependencies import dependency_statuses


def _number(value: object) -> str | None:
  return None if value is None else str(value)


def schema_document() -> dict[str, Any]:
  return catalog_document()


def configuration_document(
    current_directory: Path,
    *,
    warn: Callable[[str], None] | None = None,
) -> dict[str, Any]:
  """Return configuration locations and the complete analyzer catalogue."""
  locations = config_locations(current_directory)
  selected = discover_config(current_directory, warn=warn)
  location_rows = [
      {"scope": scope, "path": str(path), "exists": path.is_file(),
       "selected": path == selected}
      for scope, path in (
          ("local", locations.local),
          ("global_xdg", locations.xdg),
          ("global_home", locations.home),
      )
  ]
  statuses = {item["language"]: item for item in dependency_statuses()}
  document = catalog_document()
  document["configuration"] = {
      "selected": None if selected is None else str(selected),
      "locations": location_rows,
  }
  document["analyzers"] = [
      {**analyzer, "installed": statuses[analyzer["language"]]["ready"]}
      for analyzer in document["analyzers"]
  ]
  return document


def effective_profile_document(policy: dict[str, Any], languages: list[str]) -> dict[str, Any]:
  resolved = resolve_profile_set(
      standard_catalog(), balanced_profile(), languages, policy,
  )
  return {
      "profile_set_hash": resolved.profile_set_hash,
      "repository_policy_hash": resolved.repository_policy_hash,
      "calibrated": resolved.calibrated,
      "languages": {
          language: {
              "profile_hash": profile.profile_hash,
              "completeness": profile.completeness.value,
              "components": {
                  component_id: {
                      "enabled": component.enabled,
                      "severity": component.severity.value,
                      "threshold": _number(component.threshold),
                      "weight": _number(component.weight),
                      "formula": component.formula,
                      "cap": _number(component.cap),
                  }
                  for component_id, component in sorted(profile.components.items())
              },
          }
          for language, profile in sorted(resolved.profiles.items())
      },
  }
