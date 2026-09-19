#!/usr/bin/env python3
"""Check supported penalties through the freshly built packaged CLI."""
import json
from pathlib import Path
import subprocess
import tempfile

ROOT = Path(__file__).resolve().parents[1]
CASES = {
    "go": ("go", "package sample\nfunc Run(x int) int { return BODY }"),
    "java": ("java", "public class Service { public int run(int x) { return BODY; } }"),
    "typescript": ("ts", "export function run(x:number) { return BODY; }"),
    "rust": ("rs", "pub fn run(x:i32)->i32 { BODY }"),
}
rows = {}
for language, (extension, template) in CASES.items():
    results = {}
    for variant, body in {"known": "x*2", "unfamiliar": "missing(x)", "identity": "x"}.items():
        with tempfile.TemporaryDirectory(prefix="sw-uncertainty-") as directory:
            workspace = Path(directory)
            filename = "Service." + extension
            (workspace / filename).write_text(template.replace("BODY", body))
            if language == "go":
                (workspace / "go.mod").write_text("module sample\n\ngo 1.24\n")
            report = json.loads(subprocess.check_output([str(ROOT / "build/slopmark"), "-format", "json", "."], cwd=workspace))
            metric = next(f for f in report["files"] if f["path"] == filename)["components"]["module_shallowness"]
            results[variant] = {key: metric.get(key) for key in ("raw_max", "contribution", "depth_estimated")}
    assert results["identity"]["raw_max"] == 0, (language, results)
    assert results["unfamiliar"]["raw_max"] == 0, (language, results)
    assert results["unfamiliar"]["contribution"] == 0 and results["unfamiliar"]["depth_estimated"], (language, results)
    rows[language] = results
findings = {}
for variant, body in {"discarded": "return value*2", "used": "return value*extra*other"}.items():
    with tempfile.TemporaryDirectory(prefix="sw-finding-") as directory:
        workspace = Path(directory)
        (workspace / "go.mod").write_text("module sample\n\ngo 1.24\n")
        (workspace / "service.go").write_text("package sample\nfunc calculate(value,extra,other int)int{" + body + "}")
        (workspace / "caller.go").write_text("package sample\nfunc Run(x int)int{return calculate(x,x*x,x+1)}")
        report = json.loads(subprocess.check_output([str(ROOT / "build/slopmark"), "-format", "json", "."], cwd=workspace))
        metric = next(f for f in report["files"] if f["path"] == "service.go")["components"]["module_shallowness"]
        findings[variant] = {key: metric.get(key) for key in ("raw_max", "contribution", "depth_estimated")}
        assert (metric["raw_max"] > 0) == (variant == "discarded"), (variant, metric)
        assert (metric["contribution"] > 0) == (variant == "discarded"), (variant, metric)
rows["caller_cost"] = findings
surfaces = {
    "go": ("Service.go", "package sample\nfunc Run(x,unused,spare int)int{return BODY}"),
    "java": ("Service.java", "public class Service { public int run(int x,int unused,int spare){return BODY;} }"),
    "typescript": ("Service.ts", "export function run(x:number,unused:number,spare:number){return BODY;}"),
    "rust": ("Service.rs", "pub fn run(x:i32,unused:i32,spare:i32)->i32{BODY}"),
}
for language, (filename, source) in surfaces.items():
    results = {}
    for variant, body in {"unused": "x*2", "used": "x*unused*spare"}.items():
        with tempfile.TemporaryDirectory(prefix="sw-surface-") as directory:
            workspace = Path(directory)
            (workspace / filename).write_text(source.replace("BODY", body))
            if language == "go":
                (workspace / "go.mod").write_text("module sample\n\ngo 1.24\n")
            report = json.loads(subprocess.check_output([str(ROOT / "build/slopmark"), "-format", "json", "."], cwd=workspace))
            metric = next(f for f in report["files"] if f["path"] == filename)["components"]["module_shallowness"]
            results[variant] = {key: metric.get(key) for key in ("raw_max", "contribution", "depth_estimated")}
            assert (metric["raw_max"] > 0) == (variant == "unused"), (language, variant, metric)
            assert (metric["contribution"] > 0) == (variant == "unused"), (language, variant, metric)
    rows[language]["required_surface"] = results
(ROOT / "build/finding-acceptance-r35.json").write_text(json.dumps(rows, indent=2) + "\n")
print(json.dumps(rows, indent=2))
