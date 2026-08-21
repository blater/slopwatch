"""Small, intentionally strict JSON validation helpers."""

from __future__ import annotations

import math
import os
import re
from decimal import Decimal, InvalidOperation
from pathlib import Path, PurePosixPath
from typing import Any, Iterable, Mapping

from .errors import ValidationError

_IDENTIFIER = re.compile(r"^[a-z][a-z0-9_]*(?:[.-][a-z0-9_]+)*$")
_VERSION = re.compile(r"^[A-Za-z0-9][A-Za-z0-9._+-]*$")


def require_exact_keys(value: Mapping[str, Any], allowed: Iterable[str], *, context: str,
                       required: Iterable[str] = ()) -> None:
    allowed_set, required_set = set(allowed), set(required)
    unknown = set(value) - allowed_set
    missing = required_set - set(value)
    if unknown or missing:
        detail = []
        if unknown:
            detail.append("unknown=" + ", ".join(sorted(unknown)))
        if missing:
            detail.append("missing=" + ", ".join(sorted(missing)))
        raise ValidationError(f"{context} has invalid keys ({'; '.join(detail)})")


def require_identifier(value: Any, *, context: str) -> str:
    if not isinstance(value, str) or not _IDENTIFIER.fullmatch(value):
        raise ValidationError(f"{context} must be a lowercase identifier")
    return value


def require_version(value: Any, *, context: str) -> str:
    if not isinstance(value, str) or not _VERSION.fullmatch(value):
        raise ValidationError(f"{context} must be a non-empty version token")
    return value


def canonical_relative_path(value: Any, *, context: str = "path") -> str:
    if not isinstance(value, str) or not value or "\\" in value or "\x00" in value:
        raise ValidationError(f"{context} must be a non-empty POSIX relative path")
    path = PurePosixPath(value)
    if path.is_absolute() or ".." in path.parts or value in {".", ""}:
        raise ValidationError(f"{context} escapes its workspace")
    normal = path.as_posix()
    if normal != value:
        raise ValidationError(f"{context} is not canonical: {value!r}")
    return normal


def resolved_within(path: Path, root: Path, *, context: str = "path") -> Path:
    """Resolve a path and reject symlinks/outside-root locations.

    The explicit symlink check makes the policy deterministic even if a link
    currently resolves inside the workspace.
    """
    root = root.resolve(strict=True)
    path = Path(path)
    cursor = path
    while cursor != cursor.parent:
        if cursor.exists() and cursor.is_symlink():
            raise ValidationError(f"{context} contains a symlink: {path}")
        if cursor == root:
            break
        cursor = cursor.parent
    resolved = path.resolve(strict=False)
    try:
        resolved.relative_to(root)
    except ValueError as exc:
        raise ValidationError(f"{context} is outside workspace: {path}") from exc
    return resolved


def finite_number(value: Any, *, context: str = "value") -> int | Decimal:
    """Return an exact JSON-compatible number; binary floats are rejected."""
    if isinstance(value, bool):
        raise ValidationError(f"{context} must not be boolean")
    if isinstance(value, int):
        return value
    if isinstance(value, str):
        try:
            parsed = Decimal(value)
        except (InvalidOperation, ValueError) as exc:
            raise ValidationError(f"{context} is not a decimal") from exc
        if not parsed.is_finite():
            raise ValidationError(f"{context} must be finite")
        return parsed
    if isinstance(value, float):
        if not math.isfinite(value):
            raise ValidationError(f"{context} must be finite")
        raise ValidationError(f"{context} must not use binary floating point")
    raise ValidationError(f"{context} must be an integer or decimal string")


def platform_token(value: Any, *, context: str) -> str:
    if not isinstance(value, str) or not re.fullmatch(r"[A-Za-z0-9_-]+", value):
        raise ValidationError(f"{context} must be an OS/architecture token")
    return value


def executable_relative_path(value: Any, *, context: str) -> str:
    path = canonical_relative_path(value, context=context)
    if path.startswith("/") or os.path.isabs(path):
        raise ValidationError(f"{context} must be relative")
    return path
