from __future__ import annotations

import unittest
from decimal import Decimal

from slopscout_core import (
    ProfileError,
    Severity,
    balanced_profile,
    resolve_profile_set,
    standard_catalog,
)


class CoreProfileResolutionTest(unittest.TestCase):

  def setUp(self) -> None:
    self.catalog = standard_catalog()
    self.built_in = balanced_profile()

  def test_resolution_order_and_language_isolation(self) -> None:
    policy = {
        "schema": 1,
        "extends": "slopscout-balanced-v1",
        "component_groups": {
            "structural": {"enabled": False, "severity": "error"},
            "structural.complexity": {"severity": "warning"},
        },
        "components": {
            "cognitive_complexity": {"enabled": True, "weight": 11},
        },
        "languages": {
            "java": {
                "component_groups": {
                    "structural.complexity": {"severity": "info"},
                },
                "components": {
                    "cognitive_complexity": {
                        "threshold": 16,
                        "severity": "error",
                    },
                },
            },
            "typescript": {
                "component_groups": {
                    "typescript.type_safety": {"severity": "error"},
                },
                "components": {
                    "explicit_any": {"weight": 9},
                },
            },
        },
    }
    profiles = resolve_profile_set(
        self.catalog, self.built_in, ["java", "typescript"], policy
    )
    java = profiles.for_language("java")
    typescript = profiles.for_language("typescript")
    cognitive = java.components["cognitive_complexity"]
    self.assertTrue(cognitive.enabled)
    self.assertEqual(cognitive.weight, Decimal(11))
    self.assertEqual(cognitive.threshold, Decimal(16))
    self.assertIs(cognitive.severity, Severity.ERROR)
    self.assertFalse(java.components["npath_complexity"].enabled)
    self.assertIs(typescript.components["npath_complexity"].severity,
                  Severity.WARNING)
    self.assertIs(typescript.components["explicit_any"].severity,
                  Severity.ERROR)
    self.assertEqual(typescript.components["explicit_any"].weight, Decimal(9))

  def test_cli_language_override_wins_over_common_component(self) -> None:
    repository = {
        "languages": {
            "java": {
                "components": {"cognitive_complexity": {"weight": 12}},
            },
        },
    }
    cli = {
        "components": {"cognitive_complexity": {"weight": 13}},
        "languages": {
            "java": {
                "components": {"cognitive_complexity": {"weight": 14}},
            },
        },
    }
    profile = resolve_profile_set(
        self.catalog, self.built_in, ["java"], repository, cli
    ).for_language("java")
    self.assertEqual(profile.components["cognitive_complexity"].weight,
                     Decimal(14))

  def test_hashing_is_stable_across_key_order_and_numeric_spelling(self) -> None:
    first = {
        "schema": 1,
        "components": {
            "cognitive_complexity": {"weight": 10, "threshold": 15},
        },
    }
    second = {
        "components": {
            "cognitive_complexity": {
                "threshold": Decimal("15.0"),
                "weight": Decimal("10.00"),
            },
        },
        "schema": 1,
    }
    a = resolve_profile_set(self.catalog, self.built_in, ["java"], first)
    b = resolve_profile_set(self.catalog, self.built_in, ["java"], second)
    self.assertEqual(a.repository_policy_hash, b.repository_policy_hash)
    self.assertEqual(a.profile_set_hash, b.profile_set_hash)
    self.assertEqual(a.for_language("java").profile_hash,
                     b.for_language("java").profile_hash)

  def test_gate_configuration_is_not_part_of_the_policy(self) -> None:
    with self.assertRaisesRegex(ProfileError, "unknown fields"):
      resolve_profile_set(
          self.catalog, self.built_in, ["java"],
          {"gates": {"max_score": 100}},
      )

  def test_hashing_normalizes_waiver_order_and_date_representation(self) -> None:
    def waiver(waiver_id: str, expires: object) -> dict[str, object]:
      return {
          "id": waiver_id,
          "path": "src/**",
          "language": "java",
          "component": "cognitive_complexity",
          "allow_up_to": 20,
          "reason": "Reviewed.",
          "expires": expires,
      }
    from datetime import date
    first = {"waivers": [
        waiver("second", "2027-01-01"),
        waiver("first", date(2027, 2, 1)),
    ]}
    second = {"waivers": [
        waiver("first", "2027-02-01"),
        waiver("second", date(2027, 1, 1)),
    ]}
    a = resolve_profile_set(self.catalog, self.built_in, ["java"], first)
    b = resolve_profile_set(self.catalog, self.built_in, ["java"], second)
    self.assertEqual(a.repository_policy_hash, b.repository_policy_hash)
    self.assertEqual(a.profile_set_hash, b.profile_set_hash)

  def test_only_enablement_and_severity_are_legal_on_groups(self) -> None:
    with self.assertRaisesRegex(ProfileError, "unknown fields"):
      resolve_profile_set(
          self.catalog, self.built_in, ["java"],
          {"component_groups": {"structural": {"weight": 2}}},
      )

  def test_explicit_unsupported_language_component_is_an_error(self) -> None:
    with self.assertRaisesRegex(ProfileError, "not supported"):
      resolve_profile_set(
          self.catalog, self.built_in, ["typescript"],
          {"languages": {"typescript": {"components": {
              "coupling_between_objects": {"enabled": True},
          }}}},
      )

  def test_invalid_formula_ranges_and_null_reset_are_errors(self) -> None:
    overrides = (
        {"formula": "count"},
        {"threshold": 0},
        {"weight": -1},
        {"cap": float("inf")},
        {"weight": 10 ** 1002},
        {"enabled": None},
    )
    for override in overrides:
      with self.subTest(override=override), self.assertRaises(ProfileError):
        resolve_profile_set(
            self.catalog, self.built_in, ["java"],
            {"components": {"cognitive_complexity": override}},
        )


if __name__ == "__main__":
  unittest.main()
