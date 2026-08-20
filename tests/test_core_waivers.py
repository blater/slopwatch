from __future__ import annotations

import unittest
from datetime import date
from decimal import Decimal

from slopscout_core import (
    Coverage,
    CoverageState,
    Measurement,
    ProfileError,
    Provenance,
    Scope,
    ScoringEngine,
    SourceLocation,
    Subject,
    WaiverError,
    WaiverStatus,
    balanced_profile,
    resolve_profile_set,
    standard_catalog,
)


class CoreWaiverTest(unittest.TestCase):

  def setUp(self) -> None:
    self.catalog = standard_catalog()
    self.engine = ScoringEngine(self.catalog)
    self.provenance = Provenance("fixtures", "1", "fixture")

  def measurement(self, component: str, value: int, line: int,
                  *, attributes: dict[str, object] | None = None) -> Measurement:
    descriptor = self.catalog.get(component)
    return Measurement(
        component_id=component,
        definition_version=descriptor.definition_version,
        language="java",
        path="src/Main.java",
        scope=next(iter(descriptor.scopes)),
        value=value,
        subject=Subject(
            f"subject-{line}", SourceLocation(line, 0, line, 4), f"C#{line}"
        ),
        provenance=self.provenance,
        attributes=attributes or {},
    )

  def profile(self, waiver: dict[str, object], **component_override: object):
    repository: dict[str, object] = {"waivers": [waiver]}
    if component_override:
      repository["components"] = {
          waiver["component"]: component_override,
      }
    return resolve_profile_set(
        self.catalog, balanced_profile(), ["java"], repository
    )

  def waiver(self, component: str, ceiling: int, **changes: object):
    value: dict[str, object] = {
        "id": f"waive-{component}",
        "path": "src/**",
        "language": "java",
        "component": component,
        "allow_up_to": ceiling,
        "reason": "Reviewed exception.",
    }
    value.update(changes)
    return value

  def test_waiver_recomputes_capped_sum_and_retains_observed_score(self) -> None:
    profiles = self.profile(
        self.waiver("cognitive_complexity", 15), cap=25
    )
    score = self.engine.score([
        self.measurement("cognitive_complexity", 15, 1),
        self.measurement("cognitive_complexity", 30, 2),
    ], profiles).files[0]
    component = score.components["cognitive_complexity"]
    self.assertEqual(score.total_score, Decimal(20))
    self.assertEqual(score.observed_score, Decimal(25))
    self.assertEqual(component.contribution, Decimal(20))
    self.assertEqual(component.observed_contribution, Decimal(25))
    self.assertEqual(
        {item.status for item in component.waivers},
        {WaiverStatus.APPLIED, WaiverStatus.REJECTED_CEILING},
    )

  def test_max_aggregator_is_recomputed_from_surviving_subjects(self) -> None:
    profiles = self.profile(self.waiver("coupling_between_objects", 30))
    score = self.engine.score([
        self.measurement("coupling_between_objects", 40, 1),
        self.measurement("coupling_between_objects", 30, 2),
    ], profiles).files[0]
    self.assertEqual(score.total_score, Decimal(20))
    self.assertEqual(score.observed_score, Decimal(20))
    dispositions = score.components["coupling_between_objects"].waivers
    self.assertEqual({item.status for item in dispositions}, {
        WaiverStatus.APPLIED, WaiverStatus.REJECTED_CEILING,
    })

  def test_count_ceiling_uses_deduplicated_count(self) -> None:
    profiles = self.profile(self.waiver("deeply_nested_if", 2))
    first = self.measurement("deeply_nested_if", 1, 1)
    score = self.engine.score([
        first, first, self.measurement("deeply_nested_if", 1, 2)
    ], profiles).files[0]
    component = score.components["deeply_nested_if"]
    self.assertEqual(component.deduplicated_observations, 2)
    self.assertEqual(score.total_score, Decimal(0))
    self.assertEqual(score.observed_score, Decimal(12))
    self.assertIs(component.waivers[0].status, WaiverStatus.APPLIED)
    self.assertEqual(component.waivers[0].comparison_value, Decimal(2))

  def test_expired_waiver_has_no_score_effect(self) -> None:
    profiles = self.profile(self.waiver(
        "cognitive_complexity", 100, expires="2024-01-01"
    ))
    score = self.engine.score([
        self.measurement("cognitive_complexity", 30, 1)
    ], profiles, as_of=date(2025, 1, 1)).files[0]
    self.assertEqual(score.total_score, score.observed_score)
    self.assertIs(
        score.components["cognitive_complexity"].waivers[0].status,
        WaiverStatus.EXPIRED,
    )

  def test_compound_component_is_recomputed_per_definition_subject(self) -> None:
    profiles = self.profile(self.waiver("god_class", 90))
    observations = [
        self.measurement("god_class", 90, 1, attributes={
            "wmc": 90, "atfd": 6, "tcc_percent": Decimal("30"),
        }),
        self.measurement("god_class", 100, 2, attributes={
            "wmc": 100, "atfd": 10, "tcc_percent": Decimal("20"),
        }),
    ]
    score = self.engine.score(observations, profiles).files[0]
    self.assertGreater(score.observed_score, score.total_score)
    self.assertGreater(score.total_score, Decimal(0))

  def test_incomplete_coverage_fails_closed_and_cannot_be_waived(self) -> None:
    profiles = self.profile(self.waiver("cognitive_complexity", 100))
    observation = self.measurement("cognitive_complexity", 30, 1)
    coverage = [Coverage(
        "unit-java", "cognitive_complexity", "java", "src/Main.java",
        CoverageState.UNAVAILABLE, "analyzer unavailable",
    )]
    score = self.engine.score([observation], profiles, coverage).files[0]
    self.assertFalse(score.complete)
    self.assertEqual(score.components["cognitive_complexity"].contribution,
                     Decimal(0))
    self.assertFalse(score.components["cognitive_complexity"].waivers)

  def test_allow_partial_is_explicit_and_retains_incomplete_provenance(self) -> None:
    profiles = resolve_profile_set(
        self.catalog, balanced_profile(), ["java"],
        {"completeness": "allow_partial"},
    )
    observation = self.measurement("cognitive_complexity", 15, 1)
    coverage = [Coverage(
        "unit-java", "cognitive_complexity", "java", "src/Main.java",
        CoverageState.UNAVAILABLE,
    )]
    score = self.engine.score([observation], profiles, coverage).files[0]
    self.assertFalse(score.complete)
    self.assertEqual(score.total_score, Decimal(10))

  def test_malformed_waivers_are_configuration_errors(self) -> None:
    invalid = (
        self.waiver("cognitive_complexity", -1),
        self.waiver("cognitive_complexity", 10, path="../src/**"),
        self.waiver("cognitive_complexity", 10, reason=""),
        self.waiver("cognitive_complexity", 10, expires="not-a-date"),
        self.waiver("coupling_between_objects", 10, language="typescript"),
    )
    for waiver in invalid:
      with self.subTest(waiver=waiver), self.assertRaises(WaiverError):
        resolve_profile_set(
            self.catalog, balanced_profile(), ["java"], {"waivers": [waiver]}
        )
    duplicate = self.waiver("cognitive_complexity", 10)
    with self.assertRaisesRegex(WaiverError, "duplicate"):
      resolve_profile_set(
          self.catalog, balanced_profile(), ["java"],
          {"waivers": [duplicate, duplicate]},
      )

  def test_common_component_waiver_requires_language_in_mixed_run(self) -> None:
    waiver = self.waiver("cognitive_complexity", 20)
    del waiver["language"]
    with self.assertRaisesRegex(ProfileError, "must declare language"):
      resolve_profile_set(
          self.catalog, balanced_profile(), ["java", "typescript"],
          {"waivers": [waiver]},
      )


if __name__ == "__main__":
  unittest.main()
