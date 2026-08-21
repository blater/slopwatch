"""First-party provider registration derived from the canonical catalogue."""

from __future__ import annotations

from slopslap_core import catalog_document, standard_catalog
from slopslap_runtime import Capability, LanguageProvider, ProviderRegistry


def standard_provider_registry() -> ProviderRegistry:
  """Build provider metadata from the same definitions used for scoring."""
  catalog = standard_catalog()
  dependencies = {item["language"]: item
                  for item in catalog_document()["analyzers"]}
  providers = []
  for language in sorted(catalog.languages):
    dependency = dependencies[language]
    capabilities = tuple(
        Capability(
            descriptor.component_id,
            descriptor.definition_version,
            descriptor.support_for(language).value,
            descriptor.evidence_posture.value,
            descriptor.required_capability,
        )
        for descriptor in catalog.for_language(language)
    )
    providers.append(LanguageProvider(
        language=language,
        source_extensions=tuple(dependency["extensions"]),
        capabilities=capabilities,
        dependency=dependency["dependency"],
        dependency_version=dependency["version"],
        installation_method=dependency["installation_method"],
    ))
  return ProviderRegistry(providers)
