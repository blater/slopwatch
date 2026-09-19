#!/usr/bin/env python3
"""Run passive-carrier positives and behavioral negatives through the packaged CLI."""
import argparse
import hashlib
import json
from pathlib import Path
import subprocess
import tempfile

ROOT = Path(__file__).resolve().parents[1]
BODIES = {
    "java": ["value = input;", "if(input < 0) throw new IllegalArgumentException(); value = input;", "value = input * 2;", "value += input;"],
    "go": ["r.value = input", 'if input < 0 { panic("invalid") }; r.value = input', "r.value = input * 2", "r.value += input"],
    "typescript": ["this.value = input;", 'if(input < 0) throw new Error("invalid"); this.value = input;', "this.value = input * 2;", "this.value += input;"],
    "rust": ["self.value = input;", 'if input < 0 { panic!("invalid") }; self.value = input;', "self.value = input * 2;", "self.value += input;"],
}
SOURCES = {
    "java": ("Payload.java", "public final class Payload { private int value; public int value() { return value; } public void store(int input) { BODY } public void reset() { value = 0; } }"),
    "go": ("payload.go", "package payload\ntype Payload struct { value int }\nfunc (r *Payload) Value() int { return r.value }\nfunc (r *Payload) Store(input int) { BODY }\nfunc (r *Payload) Reset() { r.value = 0 }\n"),
    "typescript": ("payload.ts", "export class Payload { private value: number = 0; current(): number { return this.value; } store(input: number): void { BODY } reset(): void { this.value = 0; } }"),
    "rust": ("lib.rs", "pub struct Payload { value: i32 } impl Payload { pub fn value(&self)->i32 { self.value } pub fn store(&mut self, input:i32) { BODY } pub fn reset(&mut self) { self.value = 0; } }"),
}

def run(binary):
    rows = []
    for language, bodies in BODIES.items():
        for variant, body in zip(("passive", "validation", "transformation", "state_transition"), bodies):
            with tempfile.TemporaryDirectory(prefix="slopwatch-carrier-") as directory:
                root = Path(directory)
                filename, template = SOURCES[language]
                (root / filename).write_text(template.replace("BODY", body))
                if language == "go":
                    (root / "go.mod").write_text("module example.org/payload\n\ngo 1.22\n")
                completed = subprocess.run([str(binary), "-format", "json", "."], cwd=root, capture_output=True, text=True, check=True)
                report = json.loads(completed.stdout)
                file = next(f for f in report["files"] if f["path"] == filename)
                metric = file["components"]["module_shallowness"]
                boundaries = [report["depth"][key] for key in metric.get("depth_boundary_ids", [])]
                proven = any(e.get("kind") == "passive-result-carrier-v1" and e.get("status") == "proven" for b in boundaries for e in b.get("evidence", []))
                score = metric.get("raw_max")
                passed = isinstance(score, (int, float)) and proven == (variant == "passive")
                if variant == "passive":
                    passed &= score == 0 and metric["contribution"] == 0 and metric.get("depth_state") == "measured"
                else:
                    passed &= score > 0
                rows.append(dict(language=language, variant=variant, shallow=score, contribution=metric["contribution"], passive_carrier=proven, passed=passed))
    return rows

if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--binary", type=Path, default=ROOT / "build/slopmark")
    parser.add_argument("--river-report", type=Path)
    parser.add_argument("--output", type=Path, default=ROOT / "build/shallow-carrier-acceptance.json")
    args = parser.parse_args()
    binary = args.binary.resolve()
    rows = run(binary)
    artifacts = [binary, ROOT / "analyzers/structural/slopslap-structural", ROOT / "analyzers/structural/slopslap-structural-java.jar", ROOT / "analyzers/structural/slopslap-structural-rust", ROOT / "build/typescript/dist/src/passive-result-carrier.js"]
    evidence = {"artifacts_sha256": {str(path.relative_to(ROOT)): hashlib.sha256(path.read_bytes()).hexdigest() for path in artifacts}, "cases": rows}
    if args.river_report:
        river = json.loads(args.river_report.read_text())
        file = next(f for f in river["files"] if f["path"].endswith("/DirectoryOperationResult.java"))
        metric = file["components"]["module_shallowness"]
        boundaries = [river["depth"][key] for key in metric["depth_boundary_ids"]]
        assert metric["raw_max"] == 0 and metric["contribution"] == 0
        assert any(e.get("kind") == "passive-result-carrier-v1" and e.get("status") == "proven" for b in boundaries for e in b.get("evidence", []))
        evidence["river"] = {"path": file["path"], "shallow": metric["raw_max"], "contribution": metric["contribution"], "boundaries": boundaries}
    args.output.write_text(json.dumps(evidence, indent=2) + "\n")
    print(json.dumps(rows, indent=2))
    raise SystemExit(0 if all(row["passed"] for row in rows) else 1)
