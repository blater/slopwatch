from __future__ import annotations

import sys
import threading
import unittest
from pathlib import Path

from slopscout_runtime.errors import AnalyzerExecutionError
from slopscout_runtime.process import AnalyzerProcessAdapter
from slopscout_runtime.protocol import AnalyzerRequest, ProtocolUnit, RequestedComponent


class ProcessAdapterTests(unittest.TestCase):
    def setUp(self) -> None:
        self.request = AnalyzerRequest.create(Path.cwd(), [ProtocolUnit("unit_1", "java", ("A.java",))],
                                              [RequestedComponent("npath_complexity", "pmd-v1")])

    def test_runs_explicit_command_and_validates_stdout(self) -> None:
        code = (
            "import json,sys; r=json.loads(sys.stdin.readline()); base={'protocol_version':1,'invocation_id':r['invocation_id']}; "
            "print(json.dumps(dict(base,type='terminal',status='success',message='ok',analyzed_unit_ids=['unit_1'],failed_unit_ids=[],skipped_unit_ids=[])))"
        )
        result = AnalyzerProcessAdapter(timeout_seconds=2).run([sys.executable, "-c", code], self.request)
        self.assertEqual(result.records[-1].payload["status"], "success")

    def test_timeout_and_cancellation_are_structured(self) -> None:
        with self.assertRaises(AnalyzerExecutionError):
            AnalyzerProcessAdapter(timeout_seconds=0.05).run([sys.executable, "-c", "import time; time.sleep(2)"], self.request)
        cancelled = threading.Event()
        cancelled.set()
        with self.assertRaises(AnalyzerExecutionError):
            AnalyzerProcessAdapter(timeout_seconds=2).run([sys.executable, "-c", "import time; time.sleep(2)"],
                                                           self.request, cancellation=cancelled)
