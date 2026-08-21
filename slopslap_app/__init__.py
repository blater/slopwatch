"""Application services for multi-language Slopslap analysis."""

from .config import CONFIG_NAME, discover_config, load_policy
from .registry import standard_provider_registry
from .report import report_document

__all__ = [
    "CONFIG_NAME", "discover_config", "load_policy", "report_document",
    "standard_provider_registry",
]
