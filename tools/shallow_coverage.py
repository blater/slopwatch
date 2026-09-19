#!/usr/bin/env python3
"""Report product coverage separately from complete primary measurements.

Usage: python3 tools/shallow_coverage.py build/report.json
Estimates never count towards the primary >75% acceptance threshold.
"""
import collections
import json
import math
import sys


def coverage(document):
    languages = {}
    for file in document.get("files", []):
        language = file.get("language", "unknown")
        row = languages.setdefault(language, {
            "files": 0, "numeric": 0, "primary": 0, "estimated": 0,
            "not_applicable": 0, "missing": [], "distribution": collections.Counter(),
        })
        row["files"] += 1
        metric = file.get("components", {}).get("module_shallowness", {})
        if metric.get("depth_state") == "not_applicable":
            row["not_applicable"] += 1
            continue
        value = metric.get("raw_max")
        if not isinstance(value, (int, float)) or not math.isfinite(value) or not 0 <= value <= 100:
            row["missing"].append(file["path"])
            continue
        row["numeric"] += 1
        boundaries = [document.get("depth", {}).get(key, {}) for key in metric.get("depth_boundary_ids", [])]
        estimated = metric.get("depth_estimated", False) or any(b.get("estimated", False) for b in boundaries)
        complete = metric.get("depth_state") == "measured" and not estimated
        row["primary" if complete else "estimated"] += 1
        row["distribution"]["100" if value == 100 else f"{int(value // 10)*10:02d}–{int(value // 10)*10+9:02d}"] += 1
    for row in languages.values():
        applicable = row["files"] - row["not_applicable"]
        row["applicable"] = applicable
        row["numeric_percent"] = round(100 * row["numeric"] / applicable, 2) if applicable else None
        row["primary_percent"] = round(100 * row["primary"] / applicable, 2) if applicable else None
        row["primary_target_met"] = applicable > 0 and row["primary"] / applicable > 0.75
        row["distribution"] = dict(sorted(row["distribution"].items()))
    return languages


if __name__ == "__main__":
    with open(sys.argv[1]) as source:
        print(json.dumps(coverage(json.load(source)), indent=2, sort_keys=True))
