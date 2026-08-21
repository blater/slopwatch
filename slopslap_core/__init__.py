"""Language-neutral Slopslap domain and policy engine."""

from .catalog import LANGUAGES, balanced_profile, catalog_document, standard_catalog
from .components import (
    Aggregator,
    ComponentCatalog,
    ComponentDescriptor,
    DefaultComponentPolicy,
    EvidencePosture,
    MeasurementKind,
    NumericConstraint,
    NumericType,
    Severity,
    SupportLevel,
)
from .errors import (
    CatalogError,
    CoreError,
    ModelValidationError,
    ProfileError,
    ScoringError,
    WaiverError,
)
from .formulas import FormulaRegistry
from .model import (
    Coverage,
    CoverageState,
    Measurement,
    Provenance,
    Scope,
    SourceLocation,
    Subject,
)
from .profile import (
    BuiltInProfile,
    CompletenessPolicy,
    ComponentPolicy,
    EffectiveLanguageProfile,
    ProfileSet,
    resolve_profile_set,
)
from .scoring import (
    ComponentScore,
    FileScore,
    ScoreResult,
    ScoringEngine,
    SubjectContribution,
)
from .waiver import Waiver, WaiverDisposition, WaiverStatus

__all__ = [
    "Aggregator", "BuiltInProfile", "CatalogError", "CompletenessPolicy",
    "ComponentCatalog", "ComponentDescriptor", "ComponentPolicy",
    "ComponentScore", "CoreError", "Coverage", "CoverageState",
    "DefaultComponentPolicy", "EffectiveLanguageProfile", "EvidencePosture",
    "FileScore", "FormulaRegistry", "LANGUAGES",
    "Measurement", "MeasurementKind", "ModelValidationError",
    "NumericConstraint", "NumericType", "ProfileError", "ProfileSet",
    "Provenance", "Scope", "ScoreResult", "ScoringEngine", "ScoringError",
    "Severity", "SourceLocation", "Subject", "SubjectContribution",
    "SupportLevel", "Waiver", "WaiverDisposition", "WaiverError",
    "WaiverStatus", "balanced_profile", "catalog_document", "resolve_profile_set",
    "standard_catalog",
]
