"""Structured failures at the runtime boundary."""

from __future__ import annotations


class RuntimeError(Exception):
    """Base class for a runtime operation that cannot be safely completed."""


class ValidationError(RuntimeError):
    """A local model violates a stable contract."""


class ProtocolError(RuntimeError):
    """Analyzer stdout did not conform to the versioned wire protocol."""


class AnalyzerExecutionError(RuntimeError):
    """An analyzer process crashed, timed out, or was cancelled."""
