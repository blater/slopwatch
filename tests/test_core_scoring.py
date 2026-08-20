from __future__ import annotations

import unittest
from decimal import Decimal

from slopscout_core import (
    Coverage,
    CoverageState,
    Measurement,
    Provenance,
    Scope,
    ScoringEngine,
    ScoringError,
    SourceLocation,
    Subject,
    balanced_profile,
    resolve_profile_set,
    standard_catalog,
)


class CoreScoringTest(unittest.TestCase):

  def setUp(self) -> None:
    self.catalog = standard_catalog()
    self.engine = ScoringEngine(self.catalog)
    self.profiles = resolve_profile_set(
        self.catalog, balanced_profile(), ["java", "typescript"]
    )
    self.provenance = Provenance("fixtures", "1", "fixture")

  def measurement(self, component: str, value: int | Decimal, line: int,
                  *, language: str = "java", path: str = "src/Main.java",
                  scope: Scope | None = None, symbol: str | None = None,
                  attributes: dict[str, object] | None = None) -> Measurement:
    descriptor = self.catalog.get(component)
    selected_scope = scope or next(iter(descriptor.scopes))
    return Measurement(
        component_id=component,
        definition_version=descriptor.definition_version,
        language=language,
        path=path,
        scope=selected_scope,
        value=value,
        subject=Subject(
            symbol or f"subject-{line}", SourceLocation(line, 0, line, 5),
            symbol,
        ),
        provenance=self.provenance,
        attributes=attributes or {},
    )

  def file_score(self, measurements: list[Measurement], profiles=None):
    result = self.engine.score(measurements, profiles or self.profiles)
    self.assertEqual(len(result.files), 1)
    return result.files[0]

  def test_exact_thresholds_sum_and_max_use_definition_semantics(self) -> None:
    observations = [
        self.measurement("cognitive_complexity", 15, 1),
        self.measurement("cognitive_complexity", 30, 2),
        self.measurement("coupling_between_objects", 20, 3),
        self.measurement("coupling_between_objects", 40, 4),
    ]
    score = self.file_score(observations)
    self.assertEqual(
        score.components["cognitive_complexity"].contribution, Decimal(30)
    )
    self.assertEqual(
        score.components["coupling_between_objects"].contribution, Decimal(20)
    )
    self.assertEqual(score.total_score, Decimal(50))

  def test_count_findings_are_deduplicated_before_formula(self) -> None:
    duplicate = self.measurement("deeply_nested_if", 1, 10)
    score = self.file_score([
        duplicate, duplicate,
        self.measurement("deeply_nested_if", 1, 20),
    ])
    component = score.components["deeply_nested_if"]
    self.assertEqual(component.observations, 3)
    self.assertEqual(component.deduplicated_observations, 2)
    self.assertEqual(component.contribution, Decimal(12))

  def test_caps_apply_after_aggregation(self) -> None:
    profile = resolve_profile_set(
        self.catalog, balanced_profile(), ["java"],
        {"components": {"cognitive_complexity": {"cap": 25}}},
    )
    score = self.file_score([
        self.measurement("cognitive_complexity", 15, 1),
        self.measurement("cognitive_complexity", 30, 2),
    ], profile)
    self.assertEqual(score.total_score, Decimal(25))

  def test_huge_npath_integer_scores_without_float_overflow(self) -> None:
    score = self.file_score([
        self.measurement("npath_complexity", 10 ** 10_000, 1),
    ])
    self.assertTrue(score.total_score.is_finite())
    self.assertGreater(score.total_score, Decimal(100_000))

  def test_conflicting_duplicate_numeric_observations_are_rejected(self) -> None:
    with self.assertRaisesRegex(ScoringError, "conflicting duplicate"):
      self.engine.score([
          self.measurement("cognitive_complexity", 15, 1, symbol="C#m"),
          self.measurement("cognitive_complexity", 20, 1, symbol="C#m"),
      ], self.profiles)

  def test_definition_and_numeric_contracts_are_enforced(self) -> None:
    valid = self.measurement("npath_complexity", 200, 1)
    with self.assertRaisesRegex(ScoringError, "definition mismatch"):
      self.engine.score([
          Measurement(
              component_id=valid.component_id,
              definition_version="other-v1",
              language=valid.language,
              path=valid.path,
              scope=valid.scope,
              value=valid.value,
              subject=valid.subject,
              provenance=valid.provenance,
          )
      ], self.profiles)
    with self.assertRaisesRegex(ScoringError, "integer observations"):
      self.engine.score([
          self.measurement("npath_complexity", Decimal("200.5"), 1)
      ], self.profiles)

  def test_god_class_compound_formula_uses_pmd_conjunction_and_bridge_tcc(self) -> None:
    score = self.file_score([
        self.measurement("god_class", 60, 1, attributes={
            "wmc": 60, "atfd": 6, "tcc": Decimal("0.25"),
        }),
        self.measurement("god_class", 80, 2, attributes={
            "wmc": 80, "atfd": 4, "tcc": Decimal("0.10"),
        }),
    ])
    subjects = score.components["god_class"].subjects
    self.assertGreater(subjects[0].contribution, Decimal(0))
    self.assertEqual(subjects[1].contribution, Decimal(0))
    self.assertEqual(score.total_score, subjects[0].contribution)

  def test_coverage_definition_version_is_validated_when_present(self) -> None:
    observation = self.measurement("cognitive_complexity", 15, 1)
    with self.assertRaisesRegex(ScoringError, "coverage definition mismatch"):
      self.engine.score([observation], self.profiles, [Coverage(
          "unit", "cognitive_complexity", "java", "src/Main.java",
          CoverageState.COMPLETE, definition_version="wrong-v1",
      )])


if __name__ == "__main__":
  unittest.main()
