"""Strict repository-policy discovery and TOML loading."""

from __future__ import annotations

import os
from dataclasses import dataclass
from datetime import date, datetime
from pathlib import Path
from typing import Any, Callable, Mapping

try:
  import tomllib
except ModuleNotFoundError:  # pragma: no cover - Python 3.10 uses tomli
  import tomli as tomllib  # type: ignore[no-redef]


CONFIG_NAME = ".slopscout.toml"
XDG_CONFIG_PATH = Path("slopscout") / "config.toml"


class ConfigLoadError(ValueError):
  """A policy file cannot be decoded safely."""


@dataclass(frozen=True)
class ConfigLocations:
  local: Path
  xdg: Path
  home: Path


def config_locations(
    current_directory: Path,
    *,
    environment: Mapping[str, str] | None = None,
    home: Path | None = None,
) -> ConfigLocations:
  """Return the local and global policy locations in precedence order."""
  environment = os.environ if environment is None else environment
  home = (Path.home() if home is None else home).expanduser().resolve()
  current = current_directory.resolve()
  if current.is_file():
    current = current.parent

  xdg_value = environment.get("XDG_CONFIG_HOME", "")
  xdg_base = Path(xdg_value).expanduser() if xdg_value else home / ".config"
  if not xdg_base.is_absolute():
    xdg_base = home / ".config"
  return ConfigLocations(
      local=(current / CONFIG_NAME).resolve(),
      xdg=(xdg_base / XDG_CONFIG_PATH).resolve(),
      home=(home / CONFIG_NAME).resolve(),
  )


def discover_global_config(
    current_directory: Path,
    *,
    environment: Mapping[str, str] | None = None,
    home: Path | None = None,
    warn: Callable[[str], None] | None = None,
) -> Path | None:
  """Select the XDG policy, falling back to the home policy."""
  locations = config_locations(
      current_directory, environment=environment, home=home,
  )
  if locations.xdg.is_file():
    if locations.home.is_file() and warn is not None:
      warn(
          f"using XDG configuration {locations.xdg}; "
          f"home configuration {locations.home} also exists"
      )
    return locations.xdg
  if locations.home.is_file():
    return locations.home
  return None


def discover_config(
    current_directory: Path,
    *,
    environment: Mapping[str, str] | None = None,
    home: Path | None = None,
    warn: Callable[[str], None] | None = None,
) -> Path | None:
  """Select one policy using current-directory, XDG, then home precedence."""
  locations = config_locations(
      current_directory, environment=environment, home=home,
  )
  if locations.local.is_file():
    return locations.local
  return discover_global_config(
      current_directory, environment=environment, home=home, warn=warn,
  )


def load_policy(path: Path | None) -> Mapping[str, Any]:
  if path is None:
    return {}
  try:
    with path.open("rb") as stream:
      document = tomllib.load(stream)
  except (OSError, tomllib.TOMLDecodeError) as error:
    raise ConfigLoadError(f"cannot load {path}: {error}") from error
  if not isinstance(document, dict):
    raise ConfigLoadError(f"{path} must contain a TOML table")
  return document


def initial_policy() -> str:
  return '''# Slopscout scoring policy. Language tables override common defaults.
schema = 1
extends = "slopscout-balanced-v1"
completeness = "fail_closed"

# [languages.typescript.components.unsafe_type_boundary]
# weight = 12

# [languages.java.components.cognitive_complexity]
# threshold = 15
# weight = 10

# [languages.rust.components.type_complexity]
# enabled = true
# weight = 7
'''


def _toml_key(value: str) -> str:
  if value.replace("_", "a").replace("-", "a").isalnum() and value:
    return value
  import json
  return json.dumps(value)


def _toml_value(value: Any) -> str:
  import json
  if isinstance(value, bool):
    return "true" if value else "false"
  if isinstance(value, (int, float)) and not isinstance(value, bool):
    return str(value).lower()
  if isinstance(value, (date, datetime)):
    return value.isoformat()
  if isinstance(value, str):
    return json.dumps(value, ensure_ascii=False)
  if isinstance(value, list) and all(not isinstance(item, Mapping) for item in value):
    return "[" + ", ".join(_toml_value(item) for item in value) + "]"
  raise ConfigLoadError(f"cannot serialize policy value of type {type(value).__name__}")


def dump_policy(document: Mapping[str, Any]) -> str:
  """Serialize the strict policy subset, including waiver array tables."""
  lines: list[str] = []

  def table(value: Mapping[str, Any], path: tuple[str, ...], *, header: bool) -> None:
    if header:
      if lines and lines[-1] != "":
        lines.append("")
      lines.append("[" + ".".join(_toml_key(item) for item in path) + "]")
    scalars = [(key, item) for key, item in value.items()
               if not isinstance(item, Mapping)
               and not (isinstance(item, list) and item and isinstance(item[0], Mapping))]
    mappings = [(key, item) for key, item in value.items() if isinstance(item, Mapping)]
    arrays = [(key, item) for key, item in value.items()
              if isinstance(item, list) and item and isinstance(item[0], Mapping)]
    for key, item in scalars:
      lines.append(f"{_toml_key(str(key))} = {_toml_value(item)}")
    for key, item in mappings:
      table(item, (*path, str(key)), header=True)
    for key, items in arrays:
      for item in items:
        if lines and lines[-1] != "":
          lines.append("")
        lines.append("[[" + ".".join(_toml_key(part) for part in (*path, str(key))) + "]]" )
        table(item, (*path, str(key)), header=False)

  table(document, (), header=False)
  return "\n".join(lines).rstrip() + "\n"
