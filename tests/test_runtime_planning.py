from __future__ import annotations

import unittest
from pathlib import Path

from slopslap_runtime.errors import ValidationError
from slopslap_runtime.planning import AnalysisContext, AnalysisPlan, AnalysisUnit, KernelPlan, KernelSpec
from slopslap_runtime.protocol import RequestedComponent
from slopslap_runtime.providers import Capability, LanguageProvider, ProviderRegistry


class PlanningTests(unittest.TestCase):
    def test_registry_and_plan_are_capability_checked(self) -> None:
        component = RequestedComponent("npath_complexity", "pmd-v1")
        provider = LanguageProvider("java", (".java",),
                                    (Capability("npath_complexity", "pmd-v1", "conformant", "oracle_aligned", "syntax"),),
                                    "dev.slopslap:slopslap-pmd-bridge", "0.1.0", "maven")
        registry = ProviderRegistry([provider])
        unit = AnalysisUnit("unit_1", "java", Path.cwd(), ("src/A.java",), (component,))
        plan = AnalysisPlan.build(Path.cwd(), [unit], registry)
        self.assertEqual(plan.components_by_language()["java"], (component,))
        with self.assertRaises(ValidationError):
            AnalysisPlan(Path.cwd(), (unit, unit))

    def test_kernel_order_and_context_memoization(self) -> None:
        plan = KernelPlan([KernelSpec("metrics", ("syntax",)), KernelSpec("syntax")])
        self.assertEqual([kernel.name for kernel in plan.ordered(["metrics"])], ["syntax", "metrics"])
        context = AnalysisContext(["src/A.java"])
        calls = 0
        def compute() -> int:
            nonlocal calls
            calls += 1
            return 42
        self.assertEqual(context.fact("syntax", "src/A.java", compute), 42)
        self.assertEqual(context.fact("syntax", "src/A.java", compute), 42)
        self.assertEqual(calls, 1)
