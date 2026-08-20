"""Domain errors raised by Slopscout's language-neutral core."""


class CoreError(Exception):
  """Base class for core-domain failures."""


class ModelValidationError(CoreError, ValueError):
  """A measurement or other domain object violates its contract."""


class CatalogError(CoreError, ValueError):
  """The component catalogue is internally inconsistent."""


class ProfileError(CoreError, ValueError):
  """A profile cannot be resolved into an effective policy."""


class ScoringError(CoreError, ValueError):
  """Measurements cannot be scored under the selected profile."""


class WaiverError(ProfileError):
  """A bounded waiver is malformed or ambiguous."""
