#!/usr/bin/env python3
"""Execute the normative SHALLOW sources through the packaged CLI.

No expected-failure allowlist: missing capabilities and incorrect numbers both
fail acceptance. The JSON report separates numeric coverage from conformance.
"""
import argparse
import collections
import json
from pathlib import Path
import re
import subprocess
import tempfile

ROOT = Path(__file__).resolve().parents[1]
FILENAMES = {"go": "probe.go", "rust": "lib.rs", "typescript": "index.ts"}


def invoke(cli, directory, language, cache=False):
    command = [str(cli), "-format", "json", "-score-profile", "responsibility-v4",
               "-languages", language]
    if cache:
        command.append("-use-cache")
    result = subprocess.run(command + ["."], cwd=directory, capture_output=True,
                            text=True, timeout=120)
    if result.returncode:
        raise RuntimeError(result.stderr.strip() or f"CLI exit {result.returncode}")
    return json.loads(result.stdout)


def check(expected, raw):
    errors = []
    if expected.get("state") == "measured" and raw.get("estimated"):
        errors.append("expected definitive measurement, got provisional recognition estimate")

    def equal(label, actual, wanted):
        if actual != wanted:
            errors.append(f"{label}: expected {wanted!r}, got {actual!r}")

    obligations = raw.get("obligations") or []
    categories = collections.Counter(item["category"] for item in obligations)
    by_id = {item["id"]: item["category"] for item in obligations}
    for key, wanted in expected.items():
        if key in {"state", "H", "shallow"}:
            equal(key, raw.get(key), wanted)
        elif key in {"O", "A", "E", "P", "T", "L", "S"}:
            equal(key, raw.get("burden", {}).get(key), wanted)
        elif key == "families":
            equal(key, raw.get("burden", {}).get("O"), wanted)
        elif key.endswith("_obligations"):
            equal(key, categories[key.removesuffix("_obligations")], wanted)
        elif key == "categories":
            equal(key, sorted(categories), sorted(wanted))
        elif key == "V" and wanted == "conditional":
            alternatives = raw.get("alternatives") or []
            present = [any(by_id.get(item) == "V" for item in route) for route in alternatives]
            equal("conditional validation", bool(present) and any(present) and not all(present), True)
        elif key == "reason":
            codes = [item["code"] for item in raw.get("reasons") or []]
            if wanted not in codes:
                errors.append(f"reason: expected {wanted!r}, got {codes!r}")
        elif key == "cold_equals_warm":
            pass  # Checked with separate CLI invocations below.
        elif key == "creation_H":
            # create:<canonical type> is the adapter contract's family ID,
            # independent of source constructor/factory names.
            creation = [value for family, value in (raw.get("family_hidden") or {}).items()
                        if family.startswith("create:")]
            if not creation:
                errors.append("creation_H: missing creation-family responsibility")
            else:
                for value in creation:
                    equal(key, value, wanted)
        else:
            errors.append(f"unimplemented acceptance assertion: {key}")
    return errors


def execute(cli, case, language, variant, source):
    row = {"case": case["id"], "language": language, "variant": variant,
           "expected": case["expected"], "numeric": False, "estimated": False, "errors": []}
    try:
        with tempfile.TemporaryDirectory(prefix="shallow-acceptance-") as directory:
            filename = FILENAMES.get(language)
            if language == "java":
                match = re.search(r"public\s+(?:(?:final|abstract)\s+)?(?:class|record|interface|enum)\s+(\w+)", source)
                filename = (match.group(1) if match else "Probe") + ".java"
            Path(directory, filename).write_text(source)
            report = invoke(cli, directory, language)
            boundaries = list((report.get("depth") or {}).values())
            row["numeric"] = bool(boundaries) and all(item.get("shallow") is not None for item in boundaries)
            row["actual"] = [{**item.get("raw", {}), **{key: value for key, value in item.items() if key != "raw"}}
                             for item in boundaries]
            row["estimated"] = any(item.get("estimated", False) for item in row["actual"])
            if len(boundaries) != 1:
                row["errors"].append(f"expected one boundary, got {len(boundaries)}")
            else:
                row["errors"].extend(check(case["expected"], row["actual"][0]))
            if case["expected"].get("cold_equals_warm"):
                cold = invoke(cli, directory, language, cache=True)
                warm = invoke(cli, directory, language, cache=True)
                if report.get("depth") != cold.get("depth") or cold.get("depth") != warm.get("depth"):
                    row["errors"].append("cold/warm depth differs")
            if report.get("diagnostics"):
                row["diagnostics"] = report["diagnostics"]
    except (RuntimeError, subprocess.TimeoutExpired, ValueError, OSError) as error:
        row["errors"].append(str(error))
    row["passed"] = not row["errors"]
    return row


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--cli", type=Path, default=ROOT / "build/slopmark")
    parser.add_argument("--languages", default="go,java,rust,typescript")
    parser.add_argument("--cases", help="comma-separated fixture IDs")
    parser.add_argument("--output", type=Path, default=ROOT / "build/shallow-adapter-acceptance.json")
    args = parser.parse_args()
    fixtures = json.loads((ROOT / "docs/evidence/shallow-v4/adapter-cases.json").read_text())
    languages = args.languages.split(",")
    if set(languages) - {"go", "java", "rust", "typescript"}:
        parser.error("unknown language")
    selected = set(args.cases.split(",")) if args.cases else None
    if selected and selected - {case["id"] for case in fixtures["cases"]}:
        parser.error("unknown case")
    rows = []
    for case in fixtures["cases"]:
        if selected and case["id"] not in selected:
            continue
        for language in languages:
            for variant, value in case["languages"][language]["variants"].items():
                row = execute(args.cli.resolve(), case, language, variant, value["source"])
                rows.append(row)
                print(f'{"PASS" if row["passed"] else "FAIL"} {language}/{case["id"]}/{variant}'
                      + (": " + "; ".join(row["errors"]) if row["errors"] else ""), flush=True)
    summary = {}
    for language in languages:
        group = [row for row in rows if row["language"] == language]
        summary[language] = {"total": len(group), "numeric": sum(row["numeric"] for row in group),
                             "estimated": sum(row["estimated"] for row in group),
                             "definitive_numeric": sum(row["numeric"] and not row["estimated"] for row in group),
                             "conforming": sum(row["passed"] for row in group)}
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(json.dumps({"contract_version": fixtures["contract_version"],
                                      "summary": summary, "results": rows}, indent=2) + "\n")
    print(json.dumps(summary, indent=2))
    print(f"Report: {args.output}")
    return 0 if rows and all(row["passed"] for row in rows) else 1


if __name__ == "__main__":
    raise SystemExit(main())
