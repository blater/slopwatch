"""Capability-checked batch analysis plan and analyzer-local kernel primitives."""

from __future__ import annotations

import threading
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any, Callable, Iterable, Mapping

from ._validation import canonical_relative_path, require_identifier
from .errors import ValidationError
from .protocol import RequestedComponent
from .providers import ProviderRegistry


@dataclass(frozen=True)
class AnalysisUnit:
    unit_id: str
    language: str
    root: Path
    source_paths: tuple[str, ...]
    components: tuple[RequestedComponent, ...]
    parser_modes: tuple[str, ...] = ("syntax",)

    def __post_init__(self) -> None:
        require_identifier(self.unit_id, context="analysis unit ID")
        require_identifier(self.language, context="analysis unit language")
        if not Path(self.root).is_absolute():
            raise ValidationError("analysis unit root must be absolute")
        paths = tuple(canonical_relative_path(item, context="analysis source") for item in self.source_paths)
        if not paths or len(paths) != len(set(paths)):
            raise ValidationError("analysis unit needs unique source paths")
        object.__setattr__(self, "source_paths", paths)
        if not self.components:
            raise ValidationError("analysis unit needs requested components")
        if not self.parser_modes or len(set(self.parser_modes)) != len(self.parser_modes):
            raise ValidationError("analysis unit parser modes must be non-empty and unique")


@dataclass(frozen=True)
class AnalysisPlan:
    workspace: Path
    units: tuple[AnalysisUnit, ...]

    def __post_init__(self) -> None:
        if not Path(self.workspace).is_absolute():
            raise ValidationError("workspace must be absolute")
        if not self.units or len({unit.unit_id for unit in self.units}) != len(self.units):
            raise ValidationError("analysis plan needs uniquely identified units")
        owners: set[tuple[str, str]] = set()
        for unit in self.units:
            for path in unit.source_paths:
                key = (unit.language, path)
                if key in owners:
                    raise ValidationError(f"duplicate authoritative source ownership: {unit.language}:{path}")
                owners.add(key)

    @classmethod
    def build(cls, workspace: Path, units: Iterable[AnalysisUnit], registry: ProviderRegistry) -> "AnalysisPlan":
        units_tuple = tuple(units)
        for unit in units_tuple:
            for component in unit.components:
                if not registry.supports(unit.language, component):
                    raise ValidationError(f"{unit.language} cannot provide {component.component_id}/{component.definition_version}")
        return cls(Path(workspace).resolve(), units_tuple)

    def components_by_language(self) -> Mapping[str, tuple[RequestedComponent, ...]]:
        result: dict[str, set[RequestedComponent]] = {}
        for unit in self.units:
            result.setdefault(unit.language, set()).update(unit.components)
        return {language: tuple(sorted(values, key=lambda item: (item.component_id, item.definition_version)))
                for language, values in sorted(result.items())}


@dataclass(frozen=True)
class KernelSpec:
    name: str
    requires: tuple[str, ...] = ()
    parser_mode: str = "syntax"

    def __post_init__(self) -> None:
        require_identifier(self.name, context="kernel name")
        if any(not isinstance(item, str) for item in self.requires):
            raise ValidationError("kernel requirements must be strings")


class KernelPlan:
    """Acyclic kernel dependency graph; callers execute returned order once."""

    def __init__(self, kernels: Iterable[KernelSpec]) -> None:
        kernel_tuple = tuple(kernels)
        self._kernels = {kernel.name: kernel for kernel in kernel_tuple}
        if not self._kernels:
            raise ValidationError("kernel plan cannot be empty")
        if len(self._kernels) != len(kernel_tuple):
            raise ValidationError("duplicate kernel names")
        for kernel in self._kernels.values():
            unknown = set(kernel.requires) - set(self._kernels)
            if unknown:
                raise ValidationError(f"kernel {kernel.name} needs unknown dependency {sorted(unknown)!r}")

    def ordered(self, requested: Iterable[str] | None = None) -> tuple[KernelSpec, ...]:
        wanted = set(self._kernels if requested is None else requested)
        if unknown := wanted - set(self._kernels):
            raise ValidationError(f"unknown requested kernels {sorted(unknown)!r}")
        order: list[KernelSpec] = []
        visiting, done = set(), set()

        def visit(name: str) -> None:
            if name in done:
                return
            if name in visiting:
                raise ValidationError(f"kernel dependency cycle at {name}")
            visiting.add(name)
            for dependency in self._kernels[name].requires:
                visit(dependency)
            visiting.remove(name)
            done.add(name)
            order.append(self._kernels[name])

        for name in sorted(wanted):
            visit(name)
        return tuple(order)


class AnalysisContext:
    """Invocation-scoped cache used only inside a language analyzer."""

    def __init__(self, source_paths: Iterable[str], *, metadata: Mapping[str, Any] | None = None) -> None:
        self.source_paths = tuple(canonical_relative_path(path, context="context source") for path in source_paths)
        if len(self.source_paths) != len(set(self.source_paths)):
            raise ValidationError("context source paths must be unique")
        self.metadata = dict(metadata or {})
        self.diagnostics: list[Mapping[str, Any]] = []
        self._facts: dict[tuple[str, str], Any] = {}
        self._lock = threading.RLock()

    def fact(self, name: str, source_path: str, compute: Callable[[], Any]) -> Any:
        """Memoize an expensive fact once per source and named parser/index mode."""
        key = (require_identifier(name, context="fact name"), canonical_relative_path(source_path))
        if key[1] not in self.source_paths:
            raise ValidationError("fact source is not in analysis context")
        with self._lock:
            if key not in self._facts:
                self._facts[key] = compute()
            return self._facts[key]

    def add_diagnostic(self, diagnostic: Mapping[str, Any]) -> None:
        with self._lock:
            self.diagnostics.append(dict(diagnostic))
