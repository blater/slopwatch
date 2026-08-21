from __future__ import annotations

import unittest
from dataclasses import FrozenInstanceError
from decimal import Decimal

from slopslap_core import (
    CatalogError,
    ComponentCatalog,
    Measurement,
    ModelValidationError,
    Provenance,
    Scope,
    SourceLocation,
    Subject,
    standard_catalog,
)


class CoreModelTest(unittest.TestCase):

  def setUp(self) -> None:
    self.provenance = Provenance("fixture", "1.0", "fixture-rule")
    self.subject = Subject("method", SourceLocation(2, 1, 4, 2), "C#m")

  def measurement(self, **changes: object) -> Measurement:
    values = {
        "component_id": "cognitive_complexity",
        "definition_version": "pmd-sonar-v1",
        "language": "java",
        "path": "src/Main.java",
        "scope": Scope.FUNCTION,
        "value": 15,
        "subject": self.subject,
        "provenance": self.provenance,
        "attributes": {"facts": ["if", {"nested": True}]},
    }
    values.update(changes)
    return Measurement(**values)  # type: ignore[arg-type]

  def test_measurements_and_nested_attributes_are_immutable(self) -> None:
    measurement = self.measurement()
    with self.assertRaises(FrozenInstanceError):
      measurement.path = "Elsewhere.java"  # type: ignore[misc]
    with self.assertRaises(TypeError):
      measurement.attributes["new"] = 1  # type: ignore[index]
    with self.assertRaises(TypeError):
      measurement.attributes["facts"][1]["nested"] = False  # type: ignore[index]

  def test_rejects_binary_float_nonfinite_and_noncanonical_paths(self) -> None:
    for changes in (
        {"value": 1.5},
        {"value": Decimal("NaN")},
        {"path": "../Main.java"},
        {"path": "/src/Main.java"},
        {"attributes": {"ratio": float("inf")}},
    ):
      with self.subTest(changes=changes), self.assertRaises(
          ModelValidationError
      ):
        self.measurement(**changes)
    self.assertEqual(self.measurement(
        attributes={"reported_ratio": 1.5}
    ).attributes["reported_ratio"], 1.5)

  def test_source_locations_are_stable_and_ordered(self) -> None:
    for location in (
        (0, 0, 1, 0),
        (2, 3, 1, 4),
        (1, -1, 1, 0),
    ):
      with self.subTest(location=location), self.assertRaises(
          ModelValidationError
      ):
        SourceLocation(*location)


class CoreCatalogTest(unittest.TestCase):

  def test_standard_catalogue_has_complete_explicit_support_metadata(self) -> None:
    catalog = standard_catalog()
    for descriptor in catalog.descriptors.values():
      self.assertEqual(set(descriptor.support), set(catalog.languages))
      self.assertTrue(descriptor.taxonomy_path)
      self.assertTrue(descriptor.documentation)
      self.assertTrue(descriptor.evidence_posture.value)

  def test_catalogue_aligns_with_typescript_and_rust_analyzer_contracts(self) -> None:
    catalog = standard_catalog()
    for component_id in (
        "explicit_any", "unsafe_type_assertion", "unsafe_type_propagation",
        "unsafe_type_use", "unsafe_type_boundary",
        "ambiguous_boolean_expression", "non_exhaustive_union",
    ):
      descriptor = catalog.get(component_id)
      self.assertEqual(descriptor.definition_version,
                       "typescript-local-sink-v1")
      self.assertEqual(descriptor.scopes, frozenset({Scope.EXPRESSION}))
      self.assertIn("attributes.normalized_symbol",
                    descriptor.deduplication_key)
    for component_id in (
        "rust_large_function", "rust_deep_nesting", "rust_unsafe_block",
        "rust_panic_macro",
    ):
      descriptor = catalog.get(component_id)
      self.assertEqual(descriptor.definition_version, "rust-v1")
      self.assertFalse(descriptor.default_policy.enabled)
      self.assertEqual(descriptor.required_capability, "rust_structural")

  def test_catalogue_rejects_missing_language_support(self) -> None:
    descriptor = standard_catalog().get("cognitive_complexity")
    with self.assertRaises(CatalogError):
      ComponentCatalog([descriptor], ["java", "typescript", "rust", "go"])


if __name__ == "__main__":
  unittest.main()
