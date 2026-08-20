"""PMD-backed Java analyzer bridge."""

from .adapter import BridgeDecodeError, decode_pmd_report

__all__ = ["BridgeDecodeError", "decode_pmd_report"]
