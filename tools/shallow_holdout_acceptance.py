#!/usr/bin/env python3
"""Evaluate the frozen, source-selected real holdout without tuning its labels."""
from __future__ import annotations

import argparse
from datetime import datetime, timezone
import hashlib
import json
from pathlib import Path

from shallow_graded_acceptance import (
    BenchmarkError, ROOT, compare_pairs, digest, distribution, load_json,
    number, run_case, safe_relative, selected_cases, source_entries,
)

DEFAULT_MANIFEST = ROOT / "docs/evidence/shallow-v4/calibration-holdout/manifest.json"


def frozen_holdout(path: Path, *, require_high: bool = True) -> dict:
    expected = path.with_suffix(".sha256").read_text().split()[0]
    if digest(path) != expected:
        raise BenchmarkError("holdout manifest checksum mismatch")
    spec = load_json(path)
    if spec.get("purpose") != "holdout" or spec.get("status") != "independently-reviewed-frozen":
        raise BenchmarkError("manifest is not a frozen holdout")
    if spec.get("selection_blinded_to_scores") is not True or spec.get("frozen_before_first_evaluation") is not True:
        raise BenchmarkError("holdout has no pre-evaluation blind-selection declaration")
    cases = selected_cases(spec, None)
    if require_high and not any(c.get("group") == "high" and c["expected_range"][0] >= 65 for c in cases):
        raise BenchmarkError("holdout has no predeclared high-band positive")
    for case in cases:
        if not all(isinstance(f, dict) and f.get("sha256") and f.get("snapshot") for f in case["files"]):
            raise BenchmarkError("holdout sources must have frozen snapshots and hashes")
        source_entries(case, path)
    for witness in spec.get("witness_snapshots", []):
        relative = safe_relative(witness["snapshot"], "witness snapshot")
        source = (path.parent / relative).resolve()
        if path.parent.resolve() not in source.parents or digest(source) != witness["sha256"]:
            raise BenchmarkError("holdout caller-witness checksum/path mismatch")
    return spec


def holdout_relations(spec: dict) -> dict:
    # The source-review manifest uses descriptive relation names. Translate
    # without rewriting the frozen manifest or silently dropping its checks.
    return {"comparisons": [
        {"lower": r["lower"], "higher": r["higher"], "min_delta": r["minimum_delta"]}
        for r in spec.get("relations", [])
    ]}


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--binary", type=Path, default=ROOT / "build/slopmark")
    parser.add_argument("--manifest", type=Path, default=DEFAULT_MANIFEST)
    parser.add_argument("--output", type=Path, default=ROOT / "build/shallow-holdout.json")
    parser.add_argument("--allow-no-high-cases", action="store_true", help="explicitly evaluate a corpus without high-band coverage; does not establish positive detection")
    parser.add_argument("--require-high-language", action="append", default=[], choices=["java", "go", "typescript", "rust"], help="require a frozen high-band example in each named language")
    parser.add_argument("--record-first", action="store_true", help="write first-evaluation.json once; refuse to overwrite")
    args = parser.parse_args()
    manifest, binary = args.manifest.resolve(), args.binary.resolve()
    first = manifest.parent / "first-evaluation.json"
    # The frozen directory contains the manifest, source and caller-witness
    # snapshots, and first-run artifacts. Reject before validation so even a
    # damaged manifest cannot turn a failure report into a destructive write.
    if args.output.resolve().is_relative_to(manifest.parent):
        parser.error("output cannot overwrite frozen holdout or first-evaluation artifacts")
    if args.record_first and (first.exists() or first.with_suffix(".sha256").exists()):
        parser.error("first evaluation already recorded; it cannot be overwritten")
    result = {"manifest": str(manifest), "required_high_languages": sorted(set(args.require_high_language)), "results": [], "failures": []}
    try:
        spec = frozen_holdout(manifest, require_high=not args.allow_no_high_cases)
        declared = {c["language"] for c in selected_cases(spec, None)
                    if c.get("group") == "high" and c["expected_range"][0] >= 65}
        missing = set(args.require_high_language) - declared
        if missing:
            raise BenchmarkError("missing predeclared high-band languages: " + ", ".join(sorted(missing)))
        if not binary.is_file():
            raise BenchmarkError("packaged CLI does not exist")
        result.update({"manifest_sha256": digest(manifest), "binary_sha256": digest(binary),
                       "evaluated_at_utc": datetime.now(timezone.utc).isoformat(),
                       "limitations": spec.get("limitations", [])})
        rows = result["results"]
        for case in selected_cases(spec, None):
            try:
                row = run_case(binary, case, manifest)
            except Exception as exc:
                # A malformed report or evaluator bug is a failed observation,
                # not grounds to discard earlier cases or skip the remainder.
                row = {"id": case["id"], "language": case["language"],
                       "group": case.get("group"), "passed": False,
                       "numeric": False, "not_applicable": False,
                       "failures": [f"case evaluation failed: {type(exc).__name__}: {exc}"]}
            rows.append(row)
            result["failures"].extend({"case": row["id"], "failure": f} for f in row["failures"])
        relations = compare_pairs({r["id"]: r for r in rows}, holdout_relations(spec))
        result["relations"] = relations
        for relation in relations:
            result["failures"].extend(relation["failures"])
        positives = [r for r in rows if r["group"] == "high"]
        result["positive_detection"] = {
            "predeclared_real_high_cases": len(positives),
            "high_band_coverage": bool(positives),
            "detected_high": sum(number(r.get("raw_max")) and 65 <= r["raw_max"] <= 100 for r in positives),
            "cases": [{"id": r["id"], "value": r.get("raw_max"), "passed": r["passed"]} for r in positives],
            "by_language": {
                language: {
                    "predeclared_real_high_cases": sum(r["language"] == language for r in positives),
                    "detected_high": sum(r["language"] == language and number(r.get("raw_max")) and 65 <= r["raw_max"] <= 100 for r in positives),
                }
                for language in sorted({r["language"] for r in rows} | set(args.require_high_language))
            },
        }
        for language in sorted(set(args.require_high_language)):
            if result["positive_detection"]["by_language"][language]["detected_high"] == 0:
                result["failures"].append({"kind": "positive_detection", "language": language,
                    "failure": "required language has no numeric high-band detection"})
        result["languages"] = distribution(rows)
    except (BenchmarkError, OSError, KeyError, TypeError, ValueError) as exc:
        result["failures"].append({"kind": "suite", "failure": str(exc)})
    encoded = json.dumps(result, indent=2) + "\n"
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(encoded)
    if args.record_first:
        with first.open("x") as handle:
            handle.write(encoded)
        with first.with_suffix(".sha256").open("x") as handle:
            handle.write(hashlib.sha256(encoded.encode()).hexdigest() + "  " + first.name + "\n")
    for row in result["results"]:
        print(f"{'PASS' if row['passed'] else 'FAIL'} {row['id']}: {row.get('raw_max')}")
    print(json.dumps({"positive_detection": result.get("positive_detection"), "failures": result["failures"]}, indent=2))
    return int(bool(result["failures"]))


if __name__ == "__main__":
    raise SystemExit(main())
