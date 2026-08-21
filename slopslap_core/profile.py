"""Profile materialization, hierarchical policy resolution, and hashing."""

from __future__ import annotations

import math
from dataclasses import dataclass, field
from decimal import Decimal
from enum import Enum
from types import MappingProxyType
from typing import Any, Iterable, Mapping

from .canonical import content_hash, decimal_value, frozen_mapping
from .components import (
    ComponentCatalog,
    ComponentDescriptor,
    DefaultComponentPolicy,
    MeasurementKind,
    Severity,
)
from .errors import ProfileError
from .waiver import Waiver, parse_waivers


class CompletenessPolicy(str, Enum):
  FAIL_CLOSED = "fail_closed"
  ALLOW_PARTIAL = "allow_partial"


@dataclass(frozen=True)
class ComponentPolicy:
  enabled: bool
  severity: Severity
  threshold: Decimal | None
  weight: Decimal
  formula: str
  cap: Decimal | None

  def __post_init__(self) -> None:
    if not isinstance(self.enabled, bool):
      raise ProfileError("component enabled must be boolean")
    if not isinstance(self.severity, Severity):
      try:
        object.__setattr__(self, "severity", Severity(self.severity))
      except (TypeError, ValueError) as error:
        raise ProfileError("invalid component severity") from error
    if not isinstance(self.formula, str) or not self.formula:
      raise ProfileError("component formula is required")
    for name in ("threshold", "weight", "cap"):
      value = getattr(self, name)
      if value is None:
        continue
      try:
        normalized = decimal_value(value, f"component {name}")
      except ValueError as error:
        raise ProfileError(str(error)) from error
      if name == "threshold" and normalized <= 0:
        raise ProfileError("component threshold must be positive")
      if name != "threshold" and normalized < 0:
        raise ProfileError(f"component {name} cannot be negative")
      object.__setattr__(self, name, normalized)


@dataclass(frozen=True)
class BuiltInProfile:
  name: str
  components: Mapping[str, Mapping[str, Any]] = field(default_factory=dict)
  completeness: CompletenessPolicy = CompletenessPolicy.FAIL_CLOSED
  calibrated: bool = True

  def __post_init__(self) -> None:
    if not isinstance(self.name, str) or not self.name.strip():
      raise ProfileError("built-in profile name is required")
    if not isinstance(self.completeness, CompletenessPolicy):
      try:
        object.__setattr__(self, "completeness",
                           CompletenessPolicy(self.completeness))
      except (TypeError, ValueError) as error:
        raise ProfileError("invalid built-in completeness policy") from error
    if not isinstance(self.calibrated, bool):
      raise ProfileError("built-in calibrated flag must be boolean")
    object.__setattr__(self, "components", frozen_mapping(self.components))


@dataclass(frozen=True)
class EffectiveLanguageProfile:
  language: str
  components: Mapping[str, ComponentPolicy]
  completeness: CompletenessPolicy
  profile_hash: str

  def __post_init__(self) -> None:
    object.__setattr__(self, "components", MappingProxyType(dict(self.components)))

  @property
  def enabled_components(self) -> tuple[str, ...]:
    return tuple(component_id for component_id, policy
                 in self.components.items() if policy.enabled)


@dataclass(frozen=True)
class ProfileSet:
  built_in_profile: str
  profiles: Mapping[str, EffectiveLanguageProfile]
  waivers: tuple[Waiver, ...]
  repository_policy_hash: str
  profile_set_hash: str
  definition_versions: Mapping[str, str]
  calibrated: bool
  repository_policy: Mapping[str, Any] = field(repr=False)
  cli_overrides: Mapping[str, Any] = field(repr=False)

  def __post_init__(self) -> None:
    object.__setattr__(self, "profiles", MappingProxyType(dict(self.profiles)))
    object.__setattr__(self, "definition_versions",
                       MappingProxyType(dict(self.definition_versions)))
    object.__setattr__(self, "repository_policy",
                       frozen_mapping(self.repository_policy))
    object.__setattr__(self, "cli_overrides", frozen_mapping(self.cli_overrides))

  def for_language(self, language: str) -> EffectiveLanguageProfile:
    try:
      return self.profiles[language]
    except KeyError as error:
      raise ProfileError(f"language is not selected: {language}") from error


_TOP_LEVEL = {"schema", "extends", "component_groups", "components",
              "languages", "waivers", "completeness"}
_LANGUAGE_LEVEL = {"component_groups", "components"}
_GROUP_FIELDS = {"enabled", "severity"}
_COMPONENT_FIELDS = {"enabled", "severity", "threshold", "weight",
                     "formula", "cap"}


def _config_decimal(value: Any, name: str) -> Decimal:
  if isinstance(value, float):
    if not math.isfinite(value):
      raise ProfileError(f"{name} must be finite")
    value = Decimal(str(value))
  try:
    result = decimal_value(value, name)
  except ValueError as error:
    raise ProfileError(str(error)) from error
  if result != 0 and abs(result.adjusted()) > 1000:
    raise ProfileError(f"{name} exceeds the supported numeric range")
  return result


def _severity(value: Any, name: str) -> Severity:
  try:
    return value if isinstance(value, Severity) else Severity(value)
  except (TypeError, ValueError) as error:
    raise ProfileError(f"{name} must be error, warning, or info") from error


def _mapping(value: Any, name: str) -> Mapping[str, Any]:
  if value is None:
    return {}
  if not isinstance(value, Mapping):
    raise ProfileError(f"{name} must be a table")
  return value


def _validate_keys(table: Mapping[str, Any], allowed: set[str], name: str) -> None:
  unknown = set(table).difference(allowed)
  if unknown:
    raise ProfileError(f"{name} has unknown fields: {sorted(unknown)}")


def _normalize_for_hash(value: Any) -> Any:
  if isinstance(value, bool):
    return value
  if isinstance(value, int):
    return Decimal(value)
  if isinstance(value, float):
    if not math.isfinite(value):
      raise ProfileError("configuration contains a non-finite number")
    return Decimal(str(value))
  if isinstance(value, Mapping):
    return {str(key): _normalize_for_hash(child)
            for key, child in value.items()}
  if isinstance(value, (tuple, list)):
    return tuple(_normalize_for_hash(child) for child in value)
  return value


def _base_policy(default: DefaultComponentPolicy) -> ComponentPolicy:
  return ComponentPolicy(
      enabled=default.enabled,
      severity=default.severity,
      threshold=default.threshold,
      weight=default.weight,
      formula=default.formula,
      cap=default.cap,
  )


def _apply_component_override(policy: ComponentPolicy,
                              override: Mapping[str, Any],
                              descriptor: ComponentDescriptor,
                              name: str) -> ComponentPolicy:
  _validate_keys(override, _COMPONENT_FIELDS, name)
  if any(value is None for value in override.values()):
    raise ProfileError(f"{name} cannot use null/reset values")
  enabled = override.get("enabled", policy.enabled)
  if not isinstance(enabled, bool):
    raise ProfileError(f"{name}.enabled must be boolean")
  severity = (_severity(override["severity"], f"{name}.severity")
              if "severity" in override else policy.severity)
  threshold = (_config_decimal(override["threshold"], f"{name}.threshold")
               if "threshold" in override else policy.threshold)
  weight = (_config_decimal(override["weight"], f"{name}.weight")
            if "weight" in override else policy.weight)
  formula = override.get("formula", policy.formula)
  cap = (_config_decimal(override["cap"], f"{name}.cap")
         if "cap" in override else policy.cap)
  if descriptor.measurement_kind is not MeasurementKind.CONTINUOUS \
      and "threshold" in override:
    raise ProfileError(f"{name}.threshold is not legal for this component")
  if not isinstance(formula, str) or formula not in descriptor.allowed_formulas:
    raise ProfileError(
        f"{name}.formula {formula!r} is not allowed; expected one of "
        f"{sorted(descriptor.allowed_formulas)}"
    )
  if weight < 0 or (threshold is not None and threshold <= 0) \
      or (cap is not None and cap < 0):
    raise ProfileError(
        f"{name} requires non-negative weight/cap and positive threshold"
    )
  if descriptor.measurement_kind is MeasurementKind.CONTINUOUS \
      and threshold is None:
    raise ProfileError(f"{name} requires a threshold")
  if threshold is not None and not descriptor.threshold_constraint.accepts(
      threshold
  ):
    raise ProfileError(f"{name}.threshold violates the component constraint")
  return ComponentPolicy(enabled, severity, threshold, weight, formula, cap)


def _parse_groups(raw: Any, catalog: ComponentCatalog,
                  name: str) -> Mapping[str, Mapping[str, Any]]:
  groups = _mapping(raw, name)
  parsed: dict[str, Mapping[str, Any]] = {}
  for taxonomy, value in groups.items():
    group = _mapping(value, f"{name}.{taxonomy}")
    _validate_keys(group, _GROUP_FIELDS, f"{name}.{taxonomy}")
    if not any(descriptor.taxonomy_path == taxonomy
               or descriptor.taxonomy_path.startswith(taxonomy + ".")
               for descriptor in catalog.descriptors.values()):
      raise ProfileError(f"unknown component taxonomy group: {taxonomy}")
    if "enabled" in group and not isinstance(group["enabled"], bool):
      raise ProfileError(f"{name}.{taxonomy}.enabled must be boolean")
    if "severity" in group:
      _severity(group["severity"], f"{name}.{taxonomy}.severity")
    parsed[taxonomy] = group
  return parsed


def _apply_groups(policy: ComponentPolicy, descriptor: ComponentDescriptor,
                  groups: Mapping[str, Mapping[str, Any]],
                  name: str) -> ComponentPolicy:
  matching = [
      (taxonomy, group) for taxonomy, group in groups.items()
      if descriptor.taxonomy_path == taxonomy
      or descriptor.taxonomy_path.startswith(taxonomy + ".")
  ]
  matching.sort(key=lambda item: (item[0].count("."), item[0]))
  enabled, severity = policy.enabled, policy.severity
  for taxonomy, group in matching:
    if "enabled" in group:
      enabled = group["enabled"]
    if "severity" in group:
      severity = _severity(group["severity"],
                           f"{name}.{taxonomy}.severity")
  return ComponentPolicy(enabled, severity, policy.threshold, policy.weight,
                         policy.formula, policy.cap)


def _component_tables(raw: Any, catalog: ComponentCatalog,
                      name: str) -> Mapping[str, Mapping[str, Any]]:
  table = _mapping(raw, name)
  result: dict[str, Mapping[str, Any]] = {}
  for component_id, value in table.items():
    try:
      catalog.get(component_id)
    except ValueError as error:
      raise ProfileError(str(error)) from error
    result[component_id] = _mapping(value, f"{name}.{component_id}")
  return result


def _validate_document(raw: Mapping[str, Any], catalog: ComponentCatalog,
                       *, cli: bool = False) -> None:
  allowed = ({"components", "languages"} if cli else _TOP_LEVEL)
  _validate_keys(raw, allowed, "CLI overrides" if cli else "repository policy")
  if not cli:
    if raw.get("schema", 1) != 1:
      raise ProfileError("only configuration schema 1 is supported")
    if "waivers" in raw and not isinstance(raw["waivers"], (list, tuple)):
      raise ProfileError("waivers must be an array of tables")
  _parse_groups(raw.get("component_groups"), catalog, "component_groups")
  _component_tables(raw.get("components"), catalog, "components")
  languages = _mapping(raw.get("languages"), "languages")
  for language, language_raw in languages.items():
    if language not in catalog.languages:
      raise ProfileError(f"unknown configured language: {language}")
    language_table = _mapping(language_raw, f"languages.{language}")
    _validate_keys(language_table, _LANGUAGE_LEVEL,
                   f"languages.{language}")
    _parse_groups(language_table.get("component_groups"), catalog,
                  f"languages.{language}.component_groups")
    components = _component_tables(
        language_table.get("components"), catalog,
        f"languages.{language}.components"
    )
    for component_id in components:
      if not catalog.get(component_id).support_for(language).configurable:
        raise ProfileError(
            f"component {component_id} is not supported for {language}"
        )


def resolve_profile_set(
    catalog: ComponentCatalog,
    built_in: BuiltInProfile,
    languages: Iterable[str],
    repository_policy: Mapping[str, Any] | None = None,
    cli_overrides: Mapping[str, Any] | None = None,
) -> ProfileSet:
  """Resolve one immutable effective profile per selected language."""
  selected = tuple(dict.fromkeys(languages))
  if not selected:
    raise ProfileError("at least one language must be selected")
  for language in selected:
    if language not in catalog.languages:
      raise ProfileError(f"unknown selected language: {language}")
  repository = dict(repository_policy or {})
  cli = dict(cli_overrides or {})
  _validate_document(repository, catalog)
  _validate_document(cli, catalog, cli=True)
  if "extends" in repository and repository["extends"] != built_in.name:
    raise ProfileError(
        f"configuration extends {repository['extends']!r}, but resolver was "
        f"given {built_in.name!r}"
    )
  for component_id, override in built_in.components.items():
    descriptor = catalog.get(component_id)
    _apply_component_override(
        _base_policy(descriptor.default_policy), override, descriptor,
        f"built-in.{component_id}"
    )

  repo_groups = _parse_groups(repository.get("component_groups"), catalog,
                              "component_groups")
  repo_components = _component_tables(repository.get("components"), catalog,
                                      "components")
  cli_components = _component_tables(cli.get("components"), catalog,
                                     "CLI.components")
  language_tables = _mapping(repository.get("languages"), "languages")
  cli_languages = _mapping(cli.get("languages"), "CLI.languages")
  completeness_raw = repository.get("completeness", built_in.completeness.value)
  try:
    completeness = (completeness_raw if isinstance(completeness_raw,
                                                    CompletenessPolicy)
                    else CompletenessPolicy(completeness_raw))
  except (TypeError, ValueError) as error:
    raise ProfileError(
        "completeness must be fail_closed or allow_partial"
    ) from error
  waivers = parse_waivers(repository.get("waivers", ()), catalog)
  for waiver in waivers:
    if waiver.language is not None:
      continue
    candidates = [
        language for language in selected
        if catalog.get(waiver.component_id).support_for(language).configurable
    ]
    if len(candidates) > 1:
      raise ProfileError(
          f"waiver {waiver.waiver_id} must declare language because component "
          f"{waiver.component_id} is active in {', '.join(candidates)}"
      )

  effective: dict[str, EffectiveLanguageProfile] = {}
  for language in selected:
    repo_language = _mapping(language_tables.get(language),
                             f"languages.{language}")
    cli_language = _mapping(cli_languages.get(language),
                            f"CLI.languages.{language}")
    language_groups = _parse_groups(
        repo_language.get("component_groups"), catalog,
        f"languages.{language}.component_groups"
    )
    language_components = _component_tables(
        repo_language.get("components"), catalog,
        f"languages.{language}.components"
    )
    cli_language_components = _component_tables(
        cli_language.get("components"), catalog,
        f"CLI.languages.{language}.components"
    )
    policies: dict[str, ComponentPolicy] = {}
    for descriptor in catalog.for_language(language):
      component_id = descriptor.component_id
      policy = _base_policy(descriptor.default_policy)
      if component_id in built_in.components:
        policy = _apply_component_override(
            policy, built_in.components[component_id], descriptor,
            f"built-in.{component_id}"
        )
      policy = _apply_groups(policy, descriptor, repo_groups,
                             "component_groups")
      if component_id in repo_components:
        policy = _apply_component_override(
            policy, repo_components[component_id], descriptor,
            f"components.{component_id}"
        )
      policy = _apply_groups(policy, descriptor, language_groups,
                             f"languages.{language}.component_groups")
      if component_id in language_components:
        policy = _apply_component_override(
            policy, language_components[component_id], descriptor,
            f"languages.{language}.components.{component_id}"
        )
      if component_id in cli_components:
        policy = _apply_component_override(
            policy, cli_components[component_id], descriptor,
            f"CLI.components.{component_id}"
        )
      if component_id in cli_language_components:
        policy = _apply_component_override(
            policy, cli_language_components[component_id], descriptor,
            f"CLI.languages.{language}.components.{component_id}"
        )
      policies[component_id] = policy
    language_payload = {
        "language": language,
        "components": policies,
        "completeness": completeness,
        "definitions": {
            item.component_id: item.definition_version
            for item in catalog.for_language(language)
        },
        "waivers": tuple(waiver for waiver in waivers
                          if waiver.language in (None, language)),
    }
    effective[language] = EffectiveLanguageProfile(
        language=language,
        components=policies,
        completeness=completeness,
        profile_hash=content_hash(language_payload),
    )

  normalized_repository = _normalize_for_hash(repository)
  if waivers:
    normalized_repository["waivers"] = waivers
  normalized_cli = _normalize_for_hash(cli)
  repository_hash = content_hash(normalized_repository)
  definitions = catalog.definition_versions()
  customized_repository = {
      key: value for key, value in repository.items()
      if key not in {"schema", "extends"}
  }
  calibrated = built_in.calibrated and not customized_repository and not cli
  set_payload = {
      "built_in_profile": built_in.name,
      "profiles": effective,
      "waivers": waivers,
      "definitions": definitions,
      "repository_policy": normalized_repository,
      "cli_overrides": normalized_cli,
      "calibrated": calibrated,
  }
  return ProfileSet(
      built_in_profile=built_in.name,
      profiles=effective,
      waivers=waivers,
      repository_policy_hash=repository_hash,
      profile_set_hash=content_hash(set_payload),
      definition_versions=definitions,
      calibrated=calibrated,
      repository_policy=normalized_repository,
      cli_overrides=normalized_cli,
  )
