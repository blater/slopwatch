"""Versioned NDJSON protocol for external analyzers.

Only JSON objects are accepted.  Unknown fields and records are rejected so a
new analyzer cannot silently change scoring semantics underneath an older core.
"""

from __future__ import annotations

import json
import uuid
from dataclasses import dataclass, field
from decimal import Decimal
from pathlib import Path
from typing import Any, Iterable, Iterator, Mapping, TextIO

from ._validation import (canonical_relative_path, finite_number, require_exact_keys,
                          require_identifier, require_version)
from .errors import ProtocolError, ValidationError

PROTOCOL_VERSION = 1
MAX_RECORD_BYTES = 1_048_576
MAX_STREAM_BYTES = 32 * MAX_RECORD_BYTES
MAX_SAFE_JSON_INTEGER = 9_007_199_254_740_991
_TERMINAL = frozenset({"success", "failure", "cancelled", "timeout"})
_COVERAGE = frozenset({"complete", "unavailable", "failed", "not_requested"})
_SCOPES = frozenset({"project", "file", "type", "function", "expression"})


def _json_default(value: Any) -> Any:
    if isinstance(value, Decimal):
        return str(value)
    raise TypeError(f"not JSON serializable: {type(value).__name__}")


def _reject_json_constant(value: str) -> None:
    raise ProtocolError(f"non-finite JSON number {value!r} is prohibited")


def _object(line: str, *, line_number: int) -> Mapping[str, Any]:
    try:
        parsed = json.loads(line, parse_constant=_reject_json_constant)
    except (ValueError, json.JSONDecodeError) as exc:
        raise ProtocolError(f"malformed JSON record at line {line_number}") from exc
    if not isinstance(parsed, dict):
        raise ProtocolError(f"record at line {line_number} must be an object")
    return parsed


def encode_number(value: int | Decimal) -> int | str | Mapping[str, str]:
    """Encode huge integers without crossing a JavaScript numeric boundary."""
    if isinstance(value, int) and abs(value) > MAX_SAFE_JSON_INTEGER:
        return {"$integer": str(value)}
    if isinstance(value, Decimal):
        if not value.is_finite():
            raise ValidationError("measurement value must be finite")
        return str(value)
    return value


def decode_number(value: Any, *, context: str = "value") -> int | Decimal:
    if isinstance(value, dict):
        require_exact_keys(value, {"$integer"}, context=context, required={"$integer"})
        text = value["$integer"]
        if not isinstance(text, str) or not text or text.startswith("+"):
            raise ProtocolError(f"{context} has invalid tagged integer")
        try:
            number = int(text, 10)
        except ValueError as exc:
            raise ProtocolError(f"{context} has invalid tagged integer") from exc
        if str(number) != text:
            raise ProtocolError(f"{context} tagged integer is not canonical")
        return number
    try:
        return finite_number(value, context=context)
    except ValidationError as exc:
        raise ProtocolError(str(exc)) from exc


@dataclass(frozen=True)
class RequestedComponent:
    component_id: str
    definition_version: str

    def __post_init__(self) -> None:
        require_identifier(self.component_id, context="component_id")
        require_version(self.definition_version, context="definition_version")

    def to_dict(self) -> dict[str, str]:
        return {"component_id": self.component_id, "definition_version": self.definition_version}


@dataclass(frozen=True)
class ProtocolUnit:
    unit_id: str
    language: str
    source_paths: tuple[str, ...]
    metadata: Mapping[str, Any] = field(default_factory=dict)

    def __post_init__(self) -> None:
        require_identifier(self.unit_id, context="unit_id")
        require_identifier(self.language, context="language")
        if not self.source_paths:
            raise ValidationError("protocol unit must include source paths")
        paths = tuple(canonical_relative_path(path, context="source_paths") for path in self.source_paths)
        if len(paths) != len(set(paths)):
            raise ValidationError("protocol unit has duplicate source paths")
        object.__setattr__(self, "source_paths", paths)
        if not isinstance(self.metadata, Mapping):
            raise ValidationError("unit metadata must be an object")

    def to_dict(self) -> dict[str, Any]:
        return {"unit_id": self.unit_id, "language": self.language,
                "source_paths": list(self.source_paths), "metadata": dict(self.metadata)}


@dataclass(frozen=True)
class AnalyzerRequest:
    """The sole supported request shape sent to an analyzer stdin."""

    invocation_id: str
    workspace: str
    units: tuple[ProtocolUnit, ...]
    components: tuple[RequestedComponent, ...]
    options: Mapping[str, Any] = field(default_factory=dict)
    limits: Mapping[str, int] = field(default_factory=dict)
    protocol_version: int = PROTOCOL_VERSION

    def __post_init__(self) -> None:
        if self.protocol_version != PROTOCOL_VERSION:
            raise ValidationError(f"unsupported protocol version {self.protocol_version}")
        try:
            uuid.UUID(self.invocation_id)
        except (ValueError, AttributeError) as exc:
            raise ValidationError("invocation_id must be a UUID") from exc
        if not isinstance(self.workspace, str) or not Path(self.workspace).is_absolute():
            raise ValidationError("workspace must be an absolute path")
        if not self.units or not self.components:
            raise ValidationError("request requires units and components")
        if any(not isinstance(unit, ProtocolUnit) for unit in self.units):
            raise ValidationError("request units must be protocol units")
        if any(not isinstance(component, RequestedComponent) for component in self.components):
            raise ValidationError("request components must be requested components")
        if len({unit.unit_id for unit in self.units}) != len(self.units):
            raise ValidationError("request has duplicate unit IDs")
        pairs = [(item.component_id, item.definition_version) for item in self.components]
        if len(set(pairs)) != len(pairs):
            raise ValidationError("request has duplicate components")
        if not isinstance(self.options, Mapping) or not isinstance(self.limits, Mapping):
            raise ValidationError("request options and limits must be objects")
        for key, value in self.limits.items():
            if not isinstance(key, str) or isinstance(value, bool) or not isinstance(value, int) or value <= 0:
                raise ValidationError("resource limits must be positive integer values")

    @classmethod
    def create(cls, workspace: Path, units: Iterable[ProtocolUnit],
               components: Iterable[RequestedComponent], **kwargs: Any) -> "AnalyzerRequest":
        return cls(str(uuid.uuid4()), str(Path(workspace).resolve()), tuple(units), tuple(components), **kwargs)

    def to_record(self) -> dict[str, Any]:
        return {"type": "request", "protocol_version": self.protocol_version,
                "invocation_id": self.invocation_id, "workspace": self.workspace,
                "units": [unit.to_dict() for unit in self.units],
                "components": [component.to_dict() for component in self.components],
                "options": dict(self.options), "limits": dict(self.limits)}

    def to_ndjson(self) -> bytes:
        return (json.dumps(self.to_record(), sort_keys=True, separators=(",", ":"), default=_json_default)
                + "\n").encode("utf-8")


@dataclass(frozen=True)
class ProtocolRecord:
    record_type: str
    payload: Mapping[str, Any]


class ProtocolReader:
    """Stateful validator for analyzer stdout.

    It validates stream framing and request binding while retaining no scoring
    policy.  Callers may preserve partial records for diagnostics only.
    """

    def __init__(self, request: AnalyzerRequest, *, max_record_bytes: int = MAX_RECORD_BYTES,
                 max_stream_bytes: int = MAX_STREAM_BYTES) -> None:
        if max_record_bytes <= 0 or max_stream_bytes < max_record_bytes:
            raise ValidationError("invalid protocol size limits")
        self.request = request
        self.max_record_bytes = max_record_bytes
        self.max_stream_bytes = max_stream_bytes
        self._terminal_seen = False
        self._seen_measurements: set[tuple[Any, ...]] = set()
        self._seen_coverage: set[tuple[str, str, str, str]] = set()
        self._requested_paths = {path for unit in request.units for path in unit.source_paths}
        self._unit_ids = {unit.unit_id for unit in request.units}
        self._unit_paths = {unit.unit_id: set(unit.source_paths) for unit in request.units}
        self._unit_languages = {unit.unit_id: unit.language for unit in request.units}
        self._components = {(item.component_id, item.definition_version) for item in request.components}

    def read(self, stream: TextIO) -> Iterator[ProtocolRecord]:
        total = 0
        for line_number, raw in enumerate(stream, 1):
            raw_bytes = raw.encode("utf-8")
            total += len(raw_bytes)
            if len(raw_bytes) > self.max_record_bytes or total > self.max_stream_bytes:
                raise ProtocolError("analyzer protocol output exceeds configured size limit")
            if not raw.endswith("\n"):
                raise ProtocolError("truncated analyzer record")
            if self._terminal_seen:
                raise ProtocolError("record received after terminal status")
            payload = _object(raw, line_number=line_number)
            yield self._validate(payload)
        if not self._terminal_seen:
            raise ProtocolError("analyzer protocol stream ended without terminal status")

    def parse_text(self, text: str) -> list[ProtocolRecord]:
        from io import StringIO
        return list(self.read(StringIO(text)))

    def _common(self, payload: Mapping[str, Any], allowed: set[str], *, record_type: str,
                required: set[str] | None = None) -> None:
        require_exact_keys(payload, allowed, context=f"{record_type} record",
                           required={"type", "protocol_version", "invocation_id"} | (required or set()))
        if payload["type"] != record_type or payload["protocol_version"] != PROTOCOL_VERSION:
            raise ProtocolError(f"invalid {record_type} protocol header")
        if payload["invocation_id"] != self.request.invocation_id:
            raise ProtocolError("analyzer record has mismatched invocation ID")

    def _validate(self, payload: Mapping[str, Any]) -> ProtocolRecord:
        record_type = payload.get("type")
        if record_type == "measurement":
            self._common(payload, {"type", "protocol_version", "invocation_id", "unit_id", "component_id",
                                   "definition_version", "path", "scope", "value", "subject", "attributes",
                                   "provenance", "language"}, record_type="measurement",
                         required={"unit_id", "component_id", "definition_version", "path", "scope", "value",
                                   "subject", "attributes", "provenance"})
            self._validate_measurement(payload)
        elif record_type == "diagnostic":
            self._common(payload, {"type", "protocol_version", "invocation_id", "severity", "code", "message",
                                   "unit_id", "path"}, record_type="diagnostic",
                         required={"severity", "code", "message", "unit_id", "path"})
            if payload["severity"] not in {"error", "warning", "info"}:
                raise ProtocolError("diagnostic severity is invalid")
            if not isinstance(payload["code"], str) or not isinstance(payload["message"], str):
                raise ProtocolError("diagnostic code and message must be strings")
            self._validate_optional_location(payload)
        elif record_type == "coverage":
            self._common(payload, {"type", "protocol_version", "invocation_id", "unit_id", "component_id",
                                   "definition_version", "path", "state", "reason"}, record_type="coverage",
                         required={"unit_id", "component_id", "definition_version", "path", "state", "reason"})
            if (payload["unit_id"] not in self._unit_ids
                    or payload["path"] not in self._unit_paths.get(payload["unit_id"], set())):
                raise ProtocolError("coverage targets an unrequested unit or path")
            if (payload["component_id"], payload["definition_version"]) not in self._components:
                raise ProtocolError("coverage targets an unrequested component")
            if payload["state"] not in _COVERAGE or not isinstance(payload["reason"], str):
                raise ProtocolError("coverage state or reason is invalid")
            coverage_key = (payload["unit_id"], payload["component_id"], payload["definition_version"], payload["path"])
            if coverage_key in self._seen_coverage:
                raise ProtocolError("duplicate component coverage")
            self._seen_coverage.add(coverage_key)
        elif record_type == "execution_plan":
            self._common(payload, {"type", "protocol_version", "invocation_id", "unit_id", "parser_modes",
                                   "kernels", "discovered_source_count", "parsed_source_count"},
                         record_type="execution_plan", required={"unit_id", "parser_modes", "kernels",
                                                                    "discovered_source_count", "parsed_source_count"})
            if payload["unit_id"] not in self._unit_ids:
                raise ProtocolError("execution plan targets an unrequested unit")
            if (not isinstance(payload["parser_modes"], list) or not isinstance(payload["kernels"], list)
                    or any(not isinstance(item, str) for item in payload["parser_modes"] + payload["kernels"])
                    or any(isinstance(payload[item], bool) or not isinstance(payload[item], int) or payload[item] < 0
                           for item in ("discovered_source_count", "parsed_source_count"))):
                raise ProtocolError("execution plan is invalid")
        elif record_type == "terminal":
            self._common(payload, {"type", "protocol_version", "invocation_id", "status", "message",
                                   "analyzed_unit_ids", "failed_unit_ids", "skipped_unit_ids"}, record_type="terminal",
                         required={"status", "message", "analyzed_unit_ids", "failed_unit_ids", "skipped_unit_ids"})
            if payload["status"] not in _TERMINAL or not isinstance(payload["message"], str):
                raise ProtocolError("invalid terminal status")
            for field_name in ("analyzed_unit_ids", "failed_unit_ids", "skipped_unit_ids"):
                values = payload[field_name]
                if not isinstance(values, list) or any(item not in self._unit_ids for item in values):
                    raise ProtocolError(f"terminal {field_name} is invalid")
                if len(values) != len(set(values)):
                    raise ProtocolError(f"terminal {field_name} contains duplicates")
            dispositions = [set(payload[name]) for name in ("analyzed_unit_ids", "failed_unit_ids", "skipped_unit_ids")]
            if any(dispositions[left] & dispositions[right] for left in range(3) for right in range(left + 1, 3)):
                raise ProtocolError("terminal unit dispositions overlap")
            self._terminal_seen = True
        else:
            raise ProtocolError(f"unknown analyzer record type {record_type!r}")
        return ProtocolRecord(str(record_type), payload)

    def _validate_optional_location(self, payload: Mapping[str, Any]) -> None:
        if payload["unit_id"] is not None and payload["unit_id"] not in self._unit_ids:
            raise ProtocolError("diagnostic targets an unrequested unit")
        if payload["path"] is not None and payload["path"] not in self._requested_paths:
            raise ProtocolError("diagnostic targets a path outside requested sources")

    def _validate_measurement(self, payload: Mapping[str, Any]) -> None:
        if payload["unit_id"] not in self._unit_ids or payload["path"] not in self._unit_paths.get(payload["unit_id"], set()):
            raise ProtocolError("measurement targets an unrequested unit or path")
        if (payload["component_id"], payload["definition_version"]) not in self._components:
            raise ProtocolError("measurement targets an unrequested component")
        if payload["scope"] not in _SCOPES:
            raise ProtocolError("measurement scope is invalid")
        if "language" in payload and payload["language"] != self._unit_languages[payload["unit_id"]]:
            raise ProtocolError("measurement language does not match its unit")
        decode_number(payload["value"], context="measurement value")
        for name in ("subject", "attributes", "provenance"):
            if not isinstance(payload[name], dict):
                raise ProtocolError(f"measurement {name} must be an object")
        subject = payload["subject"]
        key = (payload["unit_id"], payload["component_id"], payload["definition_version"],
               payload["path"], payload["scope"], json.dumps(subject, sort_keys=True, separators=(",", ":")))
        if key in self._seen_measurements:
            raise ProtocolError("duplicate measurement observation")
        self._seen_measurements.add(key)
