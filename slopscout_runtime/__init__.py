"""Analyzer protocol, provider, planning, and process primitives."""

from .errors import ProtocolError, RuntimeError, ValidationError
from .planning import AnalysisContext, AnalysisPlan, AnalysisUnit, KernelPlan, KernelSpec
from .process import AnalyzerProcessAdapter, ProcessResult
from .protocol import AnalyzerRequest, ProtocolReader
from .providers import Capability, LanguageProvider, ProviderRegistry

__all__ = [
    "AnalysisContext", "AnalysisPlan", "AnalysisUnit", "AnalyzerProcessAdapter",
    "AnalyzerRequest", "Capability", "KernelPlan", "KernelSpec", "LanguageProvider",
    "ProcessResult", "ProtocolError", "ProtocolReader", "ProviderRegistry",
    "RuntimeError", "ValidationError",
]
