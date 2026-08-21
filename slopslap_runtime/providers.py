"""First-party language provider metadata and capability registry."""

from __future__ import annotations

from dataclasses import dataclass, field
from typing import Iterable, Mapping

from ._validation import require_identifier, require_version
from .errors import ValidationError
from .protocol import RequestedComponent

SUPPORT_LEVELS = frozenset({"conformant", "supported", "best_effort", "not_applicable", "unsupported"})
EVIDENCE_POSTURES = frozenset({"oracle_aligned", "established", "supported", "experimental"})


@dataclass(frozen=True)
class Capability:
    component_id: str
    definition_version: str
    support_level: str
    evidence_posture: str
    required_capability: str

    def __post_init__(self) -> None:
        require_identifier(self.component_id, context="component_id")
        require_version(self.definition_version, context="definition_version")
        if self.support_level not in SUPPORT_LEVELS:
            raise ValidationError("invalid language support level")
        if self.evidence_posture not in EVIDENCE_POSTURES:
            raise ValidationError("invalid evidence posture")
        require_identifier(self.required_capability, context="required capability")

    @property
    def component(self) -> RequestedComponent:
        return RequestedComponent(self.component_id, self.definition_version)


@dataclass(frozen=True)
class LanguageProvider:
    language: str
    source_extensions: tuple[str, ...]
    capabilities: tuple[Capability, ...]
    dependency: str
    dependency_version: str
    installation_method: str

    def __post_init__(self) -> None:
        require_identifier(self.language, context="language")
        require_version(self.dependency_version, context="analyzer dependency version")
        if not self.dependency:
            raise ValidationError("analyzer dependency name is required")
        if self.installation_method not in {"maven", "npm", "cargo", "go", "jdk"}:
            raise ValidationError("invalid analyzer installation method")
        if not self.source_extensions or any(not item.startswith(".") or item != item.lower() for item in self.source_extensions):
            raise ValidationError("provider extensions must be lowercase dotted extensions")
        if len(set(self.source_extensions)) != len(self.source_extensions):
            raise ValidationError("provider has duplicate extensions")
        keys = [(item.component_id, item.definition_version) for item in self.capabilities]
        if len(set(keys)) != len(keys):
            raise ValidationError("provider has duplicate capabilities")

    def capability_for(self, component: RequestedComponent) -> Capability | None:
        return next((item for item in self.capabilities if item.component == component), None)


class ProviderRegistry:
    """Registry with deterministic exact-file dispatch and no language branching."""

    def __init__(self, providers: Iterable[LanguageProvider] = ()) -> None:
        self._providers: dict[str, LanguageProvider] = {}
        self._extensions: dict[str, str] = {}
        for provider in providers:
            self.register(provider)

    def register(self, provider: LanguageProvider) -> None:
        if provider.language in self._providers:
            raise ValidationError(f"duplicate provider language {provider.language}")
        for extension in provider.source_extensions:
            owner = self._extensions.get(extension)
            if owner is not None:
                raise ValidationError(f"ambiguous extension claim {extension}: {owner}, {provider.language}")
        self._providers[provider.language] = provider
        self._extensions.update({extension: provider.language for extension in provider.source_extensions})

    def provider(self, language: str) -> LanguageProvider:
        try:
            return self._providers[language]
        except KeyError as exc:
            raise ValidationError(f"unknown language provider {language}") from exc

    def language_for_path(self, path: str) -> str:
        suffix = path[path.rfind("."):].lower() if "." in path else ""
        try:
            return self._extensions[suffix]
        except KeyError as exc:
            raise ValidationError(f"unsupported source extension for {path}") from exc

    def capabilities(self, language: str) -> tuple[Capability, ...]:
        return self.provider(language).capabilities

    def supports(self, language: str, component: RequestedComponent) -> bool:
        capability = self.provider(language).capability_for(component)
        return capability is not None and capability.support_level not in {"not_applicable", "unsupported"}

    def all_providers(self) -> tuple[LanguageProvider, ...]:
        return tuple(self._providers[key] for key in sorted(self._providers))
