import hashlib
import contextlib
import io
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

from shallow_holdout_acceptance import BenchmarkError, frozen_holdout, holdout_relations, main
from shallow_graded_acceptance import compare_pairs


class HoldoutSafeguardTests(unittest.TestCase):
    def fixture(self, directory):
        path = Path(directory) / "manifest.json"
        source = Path(directory) / "Example.java"
        source.write_text("class Example {}")
        spec = {"purpose": "holdout", "status": "independently-reviewed-frozen",
                "selection_blinded_to_scores": True, "frozen_before_first_evaluation": True,
                "cases": [{"id": "high", "language": "java", "target": "Example.java",
                           "group": "high", "expected_range": [65, 100],
                           "files": [{"path": "Example.java", "snapshot": "Example.java",
                                      "sha256": hashlib.sha256(source.read_bytes()).hexdigest()}]}]}
        self.freeze(path, spec)
        return path, source, spec

    def freeze(self, path, spec):
        path.write_text(json.dumps(spec))
        path.with_suffix(".sha256").write_text(hashlib.sha256(path.read_bytes()).hexdigest())

    def test_hashes_reject_changed_expectations_and_source(self):
        with tempfile.TemporaryDirectory() as directory:
            path, source, spec = self.fixture(directory)
            frozen_holdout(path)
            path.write_text(path.read_text().replace("65", "0"))
            with self.assertRaisesRegex(BenchmarkError, "checksum"):
                frozen_holdout(path)
            self.freeze(path, spec)
            source.write_text("class Changed {}")
            with self.assertRaisesRegex(BenchmarkError, "hash mismatch"):
                frozen_holdout(path)

    def test_corpus_without_real_high_cases_is_rejected(self):
        with tempfile.TemporaryDirectory() as directory:
            path, _, spec = self.fixture(directory)
            spec["cases"][0]["group"] = "low"
            self.freeze(path, spec)
            with self.assertRaisesRegex(BenchmarkError, "high-band"):
                frozen_holdout(path)
            # An explicitly separate low/mixed survey can report its coverage
            # gap; the default high-detection gate remains mandatory.
            self.assertEqual(frozen_holdout(path, require_high=False)["cases"][0]["group"], "low")

    def test_source_review_relations_are_actually_checked(self):
        spec = {"relations": [{"lower": "low", "higher": "high", "minimum_delta": 25}]}
        result = compare_pairs({"low": {"raw_max": 30}, "high": {"raw_max": 40}}, holdout_relations(spec))
        self.assertEqual(len(result[0]["checks"]), 1)
        self.assertEqual(result[0]["checks"][0]["min_delta"], 25)
        self.assertFalse(result[0]["checks"][0]["passed"])

    def invoke(self, manifest, binary, output, *extra):
        argv = ["holdout", "--manifest", str(manifest), "--binary", str(binary),
                "--output", str(output), *extra]
        with patch("sys.argv", argv), contextlib.redirect_stdout(io.StringIO()), contextlib.redirect_stderr(io.StringIO()):
            return main()

    def command_fixture(self, directory):
        root = Path(directory)
        frozen = root / "frozen"
        frozen.mkdir()
        manifest, source, spec = self.fixture(frozen)
        witness = frozen / "witness.java"
        witness.write_text("class Caller {}")
        spec["witness_snapshots"] = [{"snapshot": witness.name,
            "sha256": hashlib.sha256(witness.read_bytes()).hexdigest()}]
        self.freeze(manifest, spec)
        binary = root / "failing-cli"
        binary.write_text("#!/bin/sh\necho synthetic-cli-failure >&2\nexit 1\n")
        binary.chmod(0o755)
        return manifest, source, witness, spec, binary, root / "result.json"

    def test_failed_first_evaluation_is_preserved_and_cannot_be_overwritten(self):
        with tempfile.TemporaryDirectory() as directory:
            manifest, source, witness, _, binary, output = self.command_fixture(directory)
            self.assertEqual(self.invoke(manifest, binary, output, "--record-first"), 1)
            first = manifest.parent / "first-evaluation.json"
            checksum = first.with_suffix(".sha256")
            record = json.loads(first.read_text())
            self.assertEqual(record["results"][0]["id"], "high")
            self.assertFalse(record["results"][0]["passed"])
            self.assertIn("synthetic-cli-failure", record["results"][0]["failures"][0])
            self.assertEqual(checksum.read_text().split()[0], hashlib.sha256(first.read_bytes()).hexdigest())
            protected = [first, checksum, manifest, manifest.with_suffix(".sha256"), source, witness]
            before = {path: path.read_bytes() for path in protected}
            with self.assertRaises(SystemExit) as rejected:
                self.invoke(manifest, binary, output, "--record-first")
            self.assertEqual(rejected.exception.code, 2)
            for path in protected:
                with self.subTest(path=path.name), self.assertRaises(SystemExit) as rejected:
                    self.invoke(manifest, binary, path)
                self.assertEqual(rejected.exception.code, 2)
            self.assertEqual({path: path.read_bytes() for path in protected}, before)

    def test_high_detection_cannot_substitute_another_language(self):
        with tempfile.TemporaryDirectory() as directory:
            manifest, _, _, _, binary, output = self.command_fixture(directory)
            self.assertEqual(self.invoke(manifest, binary, output,
                                         "--require-high-language", "rust"), 1)
            result = json.loads(output.read_text())
            self.assertIn("missing predeclared high-band languages: rust", result["failures"][0]["failure"])
            self.assertEqual(result["results"], [])

    def test_required_positive_cannot_pass_as_not_applicable(self):
        with tempfile.TemporaryDirectory() as directory:
            manifest, _, _, spec, binary, output = self.command_fixture(directory)
            spec["cases"][0]["allow_not_applicable"] = True
            self.freeze(manifest, spec)
            row = {"id": "high", "language": "java", "group": "high",
                   "raw_max": None, "passed": True, "numeric": False,
                   "not_applicable": True, "failures": []}
            with patch("shallow_holdout_acceptance.run_case", return_value=row):
                self.assertEqual(self.invoke(manifest, binary, output,
                                             "--require-high-language", "java"), 1)
            result = json.loads(output.read_text())
            self.assertEqual(result["positive_detection"]["by_language"]["java"]["detected_high"], 0)
            self.assertEqual(result["failures"][0]["kind"], "positive_detection")

    def test_case_exception_keeps_prior_and_subsequent_observations(self):
        with tempfile.TemporaryDirectory() as directory:
            manifest, _, _, spec, binary, output = self.command_fixture(directory)
            spec["cases"] = [dict(spec["cases"][0], id=name) for name in ("before", "broken", "after")]
            self.freeze(manifest, spec)
            def evaluate(binary, case, manifest):
                if case["id"] == "broken":
                    raise KeyError("malformed report")
                return {"id": case["id"], "language": "java", "group": "high",
                        "raw_max": 70, "passed": True, "numeric": True,
                        "not_applicable": False, "failures": []}
            with patch("shallow_holdout_acceptance.run_case", side_effect=evaluate):
                self.assertEqual(self.invoke(manifest, binary, output, "--record-first"), 1)
            result = json.loads(output.read_text())
            self.assertEqual([row["id"] for row in result["results"]], ["before", "broken", "after"])
            self.assertEqual(result["results"][0]["raw_max"], 70)
            self.assertEqual(result["results"][2]["raw_max"], 70)
            self.assertEqual(result["failures"][0]["case"], "broken")
            self.assertIn("KeyError", result["failures"][0]["failure"])
            self.assertEqual(output.read_bytes(), (manifest.parent / "first-evaluation.json").read_bytes())
            self.assertEqual(result["positive_detection"]["by_language"]["java"],
                             {"predeclared_real_high_cases": 3, "detected_high": 2})


if __name__ == "__main__":
    unittest.main()
