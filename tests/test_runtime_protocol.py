from __future__ import annotations

import json
import unittest
from pathlib import Path

from slopscout_runtime.errors import ProtocolError, ValidationError
from slopscout_runtime.protocol import AnalyzerRequest, ProtocolReader, ProtocolUnit, RequestedComponent, decode_number, encode_number


class ProtocolTests(unittest.TestCase):
    def setUp(self) -> None:
        self.component = RequestedComponent("cognitive_complexity", "pmd-sonar-v1")
        self.request = AnalyzerRequest.create(
            Path.cwd(), [ProtocolUnit("unit_1", "typescript", ("src/a.ts",))], [self.component]
        )

    def _record(self, record_type: str, **values: object) -> dict[str, object]:
        return {"type": record_type, "protocol_version": 1, "invocation_id": self.request.invocation_id, **values}

    def test_valid_stream_and_tagged_huge_integer(self) -> None:
        measurement = self._record(
            "measurement", unit_id="unit_1", component_id="cognitive_complexity", definition_version="pmd-sonar-v1",
            path="src/a.ts", scope="function", value=encode_number(10**30), subject={"name": "f"}, attributes={}, provenance={}
        )
        terminal = self._record("terminal", status="success", message="ok", analyzed_unit_ids=["unit_1"],
                                failed_unit_ids=[], skipped_unit_ids=[])
        records = ProtocolReader(self.request).parse_text(json.dumps(measurement) + "\n" + json.dumps(terminal) + "\n")
        self.assertEqual([record.record_type for record in records], ["measurement", "terminal"])
        self.assertEqual(decode_number(measurement["value"]), 10**30)

    def test_rejects_duplicate_and_outside_measurement(self) -> None:
        record = self._record(
            "measurement", unit_id="unit_1", component_id="cognitive_complexity", definition_version="pmd-sonar-v1",
            path="src/a.ts", scope="function", value=1, subject={"name": "f"}, attributes={}, provenance={}
        )
        terminal = self._record("terminal", status="success", message="ok", analyzed_unit_ids=["unit_1"],
                                failed_unit_ids=[], skipped_unit_ids=[])
        with self.assertRaises(ProtocolError):
            ProtocolReader(self.request).parse_text("\n".join(map(json.dumps, [record, record, terminal])) + "\n")
        record["path"] = "outside.ts"
        with self.assertRaises(ProtocolError):
            ProtocolReader(self.request).parse_text(json.dumps(record) + "\n" + json.dumps(terminal) + "\n")

    def test_requires_terminal_and_rejects_unknown_schema(self) -> None:
        with self.assertRaises(ProtocolError):
            ProtocolReader(self.request).parse_text(json.dumps(self._record("metadata")) + "\n")
        malformed = self._record("terminal", status="success", message="ok", analyzed_unit_ids=[], failed_unit_ids=[],
                                 skipped_unit_ids=[], surprise=True)
        with self.assertRaises(ValidationError):
            ProtocolReader(self.request).parse_text(json.dumps(malformed) + "\n")
