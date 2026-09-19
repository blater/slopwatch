#!/usr/bin/env python3
"""Compare frozen holdout isolation, retained witnesses and live workspace context."""
from __future__ import annotations
import argparse
from datetime import datetime, timezone
import hashlib
import json
from pathlib import Path
import subprocess

from shallow_graded_acceptance import (BenchmarkError, ROOT, compare_pairs, digest, distribution,
                                      evaluate_report, number, run_case, selected_cases)
from shallow_holdout_acceptance import DEFAULT_MANIFEST, frozen_holdout, holdout_relations


def repository(origin: Path) -> Path:
    process = subprocess.run(["git", "-C", str(origin.parent), "rev-parse", "--show-toplevel"],
                             capture_output=True, text=True, timeout=10)
    if process.returncode:
        raise BenchmarkError(f"cannot locate workspace for {origin}: {process.stderr.strip()}")
    return Path(process.stdout.strip()).resolve()


def context_case(case: dict, spec: dict, manifest: Path, root: Path) -> dict:
    # Only frozen caller witnesses from this repository are added. Retaining
    # original relative paths preserves package/module topology and provenance.
    files = []
    seen = {}
    for entry in [*case["files"], *spec.get("witness_snapshots", [])]:
        origin = Path(entry["origin"]).resolve()
        if not origin.is_relative_to(root):
            continue
        relative = origin.relative_to(root).as_posix()
        if relative in seen:
            if seen[relative] != entry["sha256"]:
                raise BenchmarkError(f"conflicting frozen bytes for context path: {relative}")
            continue
        seen[relative] = entry["sha256"]
        files.append({"path": relative, "snapshot": entry["snapshot"], "sha256": entry["sha256"]})
    target = Path(case["files"][0]["origin"]).resolve().relative_to(root).as_posix()
    return {**case, "target": target, "files": files}


def summarize(rows: list[dict], spec: dict) -> dict:
    relations = compare_pairs({r["id"]: r for r in rows}, holdout_relations(spec))
    failures = [{"case": r["id"], "failure": f} for r in rows for f in r["failures"]]
    failures.extend(f for relation in relations for f in relation["failures"])
    positives = [r for r in rows if r.get("group") == "high"]
    by_language = {
        language: {"expected_high": sum(r.get("language") == language for r in positives),
                   "detected_high": sum(r.get("language") == language and number(r.get("raw_max")) and 65 <= r["raw_max"] <= 100 for r in positives)}
        for language in sorted({r["language"] for r in positives})
    }
    for language, counts in by_language.items():
        if counts["detected_high"] == 0:
            failures.append({"kind": "positive_detection", "language": language,
                             "failure": "declared high-band language has no numeric high detection"})
    return {"results": rows, "relations": relations, "failures": failures,
            "languages": distribution(rows), "passed": not failures,
            "positive_detection": {"expected_high": len(positives),
                                   "detected_high": sum(counts["detected_high"] for counts in by_language.values()),
                                   "by_language": by_language}}


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--manifest", type=Path, default=DEFAULT_MANIFEST)
    parser.add_argument("--binary", type=Path, default=ROOT / "build/slopmark")
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--frozen-only", action="store_true", help="evaluate frozen targets and witnesses without requiring live repositories")
    parser.add_argument("--context", type=Path, default=DEFAULT_MANIFEST.parent.parent / "calibration-context.json")
    args = parser.parse_args()
    manifest, binary = args.manifest.resolve(), args.binary.resolve()
    if args.output.resolve().is_relative_to(manifest.parent) or args.output.resolve() in {binary, args.context.resolve()}:
        parser.error("output must be outside the frozen holdout directory")
    spec = frozen_holdout(manifest)
    artifact = {"manifest_sha256": digest(manifest), "binary_sha256": digest(binary),
                "evaluated_at_utc": datetime.now(timezone.utc).isoformat(),
                "note": "Context evaluation of previously observed cases, not a fresh blind holdout. Frozen labels unchanged.",
                "workspaces": {}, "modes": {}}
    context = json.loads(args.context.read_text())
    artifact["context_sha256"] = digest(args.context)
    cases = selected_cases(spec, None)
    rows = {"isolated": [], "witness_context": [], "workspace": []}
    if args.frozen_only:
        del rows["workspace"]
    reports: dict[Path, dict] = {}
    for case in cases:
        rows["isolated"].append(run_case(binary, case, manifest))
        try:
            root = Path(context["repositories"][context["case_repositories"][case["id"]]]).resolve()
            if not args.frozen_only and repository(Path(case["files"][0]["origin"])) != root:
                raise BenchmarkError("live repository root disagrees with declared context")
            contextual = context_case(case, spec, manifest, root)
            rows["witness_context"].append(run_case(binary, contextual, manifest))
            if args.frozen_only:
                continue
            # Refuse to compare different target/witness bytes as contextual
            # effects. Other workspace source/configuration remains live and
            # its analyzed-file inventory hashes are recorded below.
            for entry in [*case["files"], *spec.get("witness_snapshots", [])]:
                origin = Path(entry["origin"]).resolve()
                if origin.is_relative_to(root) and digest(origin) != entry["sha256"]:
                    raise BenchmarkError(f"live source differs from frozen source: {origin}")
            if root not in reports:
                command = [str(binary), "-format", "json", "-languages", case["language"], "."]
                process = subprocess.run(command, cwd=root, capture_output=True, text=True, timeout=600)
                if process.returncode:
                    raise BenchmarkError(process.stderr.strip() or f"CLI exit {process.returncode}")
                reports[root] = json.loads(process.stdout)
                inventory = [{"path": f["path"], "sha256": digest(root / f["path"])}
                             for f in reports[root].get("files", [])]
                files = reports[root].get("files", [])
                metrics = [f.get("components", {}).get("module_shallowness", {}) for f in files]
                revision = subprocess.run(["git", "-C", str(root), "rev-parse", "HEAD"], capture_output=True, text=True, timeout=10)
                artifact["workspaces"][str(root)] = {"command": command,
                    "revision": revision.stdout.strip() if revision.returncode == 0 else None,
                    "context_limitations": "Live workspace; analyzed source bytes are hashed, external dependencies and unreported configuration are not frozen.",
                    "coverage": {"analyzed": len(files), "numeric": sum(number(m.get("raw_max")) for m in metrics),
                                 "not_applicable": sum(m.get("depth_state") == "not_applicable" for m in metrics),
                                 "estimated": sum(bool(m.get("depth_estimated")) for m in metrics),
                                 "missing_applicable": sum(not number(m.get("raw_max")) and m.get("depth_state") != "not_applicable" for m in metrics)},
                    "ratings": [{"path": f["path"], "value": m.get("raw_max"), "state": m.get("depth_state")} for f,m in zip(files,metrics)],
                    "analyzed_source_hashes": inventory, "file_count": len(inventory),
                    "report_sha256": hashlib.sha256(process.stdout.encode()).hexdigest()}
            rows["workspace"].append(evaluate_report(contextual, reports[root]))
        except (BenchmarkError, OSError, ValueError, KeyError, TypeError, subprocess.TimeoutExpired) as exc:
            failed = {"id": case["id"], "language": case["language"], "group": case["group"],
                      "numeric": False, "not_applicable": False, "passed": False, "failures": [str(exc)]}
            if not any(r["id"] == case["id"] for r in rows["witness_context"]):
                rows["witness_context"].append(dict(failed))
            if "workspace" in rows:
                rows["workspace"].append(failed)
    artifact["modes"] = {mode: summarize(values, spec) for mode, values in rows.items()}
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(json.dumps(artifact, indent=2) + "\n")
    for mode, result in artifact["modes"].items():
        print(mode, [(r["id"], r.get("raw_max")) for r in result["results"]], result["positive_detection"])
    return int(any(not r["passed"] for r in artifact["modes"].values()))


if __name__ == "__main__":
    raise SystemExit(main())
