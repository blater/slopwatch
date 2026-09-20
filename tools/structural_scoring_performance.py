#!/usr/bin/env python3
"""Measure structural-scoring runtime without adding a test suite.

The driver keeps source snapshots, reports, and watch output out of the
repository.  A cold run writes the normal CLI cache without reading it.  The
same workspace and cache are then used for unchanged warm and one-file edit
runs.  Reports are consumed as byte streams; large JSON reports are never
retained in the repository.

Typical real-repository run (the source snapshot is reusable for a candidate):

    python3 tools/structural_scoring_performance.py snapshot \
      --source /Users/blater/src/slopwatch \
      --destination /private/tmp/slopwatch-structural-perf/slopwatch-source
    python3 tools/structural_scoring_performance.py measure \
      --binary /private/tmp/slopwatch-baseline-20260920T1420Z/slopmark \
      --workspace /private/tmp/slopwatch-structural-perf/slopwatch-source \
      --output /private/tmp/slopwatch-structural-perf/slopwatch-baseline.json

The same ``measure`` invocation can be repeated with a candidate binary after
the candidate build.  The workspace is never edited in place: edit phases
clone the tree with hard links, then break the link for the changed file.
"""

from __future__ import annotations

import argparse
import collections
import dataclasses
import gzip
import hashlib
import json
import os
import pty
import re
import resource
import select
import shutil
import signal
import stat
import struct
import subprocess
import sys
import tempfile
import termios
import time
from pathlib import Path
from typing import Iterable, Iterator, Sequence


LANG_EXTENSIONS = {".go": "go", ".java": "java", ".ts": "typescript", ".rs": "rust"}
LANGUAGES = ("go", "java", "typescript", "rust")
SKIP_DIRS = {
    ".git",
    ".idea",
    ".gradle",
    ".gocache",
    ".cache",
    "node_modules",
    "build",
    "target",
    "dist",
    "vendor",
    "java-runtime",
}
SHAPES = ("guard", "dispatch", "shared-state", "helper-edge")


@dataclasses.dataclass
class Capture:
    phase: str
    repetition: int
    command: list[str]
    cwd: str
    elapsed_seconds: float
    user_seconds: float
    system_seconds: float
    peak_rss_bytes: int
    returncode: int
    stdout_bytes: int
    stderr_bytes: int
    report_sha256: str | None = None
    evidence_markers: int = 0
    compressed_report_bytes: int = 0
    idle_seconds: float | None = None
    startup_seconds: float | None = None
    error_tail: str = ""
    idle_cpu_seconds: float | None = None
    settled_seconds: float | None = None
    idle_observed: bool = False
    cache_mode: str | None = None
    report_summary: dict[str, object] | None = None
    analyzer_failures: int = 0
    execution_plan_count: int = 0
    execution_invocation_count: int = 0

    def as_dict(self) -> dict[str, object]:
        return dataclasses.asdict(self)


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__)
    sub = parser.add_subparsers(dest="command", required=True)

    snapshot = sub.add_parser("snapshot", help="copy a source tree into an isolated snapshot")
    snapshot.add_argument("--source", required=True, type=Path)
    snapshot.add_argument("--destination", required=True, type=Path)

    generate = sub.add_parser("generate", help="generate the deterministic 30K adjunct workload")
    generate.add_argument("--destination", required=True, type=Path)
    generate.add_argument("--files-per-language", type=int, default=7500)

    measure = sub.add_parser("measure", help="run cold, warm, edit, and idle measurements")
    measure.add_argument("--binary", required=True, type=Path)
    measure.add_argument("--workspace", required=True, type=Path)
    measure.add_argument("--output", required=True, type=Path)
    measure.add_argument("--repetitions", type=int, default=3)
    measure.add_argument("--timeout-seconds", type=float, default=600.0)
    measure.add_argument("--idle-seconds", type=float, default=10.0)
    measure.add_argument("--startup-seconds", type=float, default=30.0)
    measure.add_argument("--target", action="append", default=[])
    measure.add_argument("--languages", default="")
    measure.add_argument("--keep-raw", action="store_true")
    measure.add_argument("--label", default="workspace")
    return parser.parse_args()


def should_skip(directory: Path) -> bool:
    return directory.name in SKIP_DIRS or directory.name.startswith(".")


def iter_files(root: Path) -> Iterator[Path]:
    for directory, dirs, names in os.walk(root):
        dirs[:] = sorted(d for d in dirs if not should_skip(Path(d)))
        for name in sorted(names):
            path = Path(directory) / name
            if path.is_symlink() or not path.is_file():
                continue
            yield path


def source_inventory(root: Path) -> dict[str, object]:
    counts: collections.Counter[str] = collections.Counter()
    bytes_by_language: collections.Counter[str] = collections.Counter()
    lines_by_language: collections.Counter[str] = collections.Counter()
    max_bytes: collections.Counter[str] = collections.Counter()
    tree_digest = hashlib.sha256()
    for path in iter_files(root):
        language = LANG_EXTENSIONS.get(path.suffix.lower())
        if not language:
            continue
        data = path.read_bytes()
        counts[language] += 1
        bytes_by_language[language] += len(data)
        lines_by_language[language] += data.count(b"\n") + (1 if data else 0)
        max_bytes[language] = max(max_bytes[language], len(data))
        tree_digest.update(str(path.relative_to(root)).encode("utf-8"))
        tree_digest.update(b"\0")
        tree_digest.update(data)
    return {
        "files": sum(counts.values()),
        "by_language": dict(sorted(counts.items())),
        "bytes_by_language": dict(sorted(bytes_by_language.items())),
        "lines_by_language": dict(sorted(lines_by_language.items())),
        "max_file_bytes_by_language": dict(sorted(max_bytes.items())),
        "source_tree_sha256": tree_digest.hexdigest(),
    }


def clone_tree_with_links(source: Path, destination: Path) -> None:
    """Clone a tree cheaply; the caller must break links before editing."""
    destination.mkdir(parents=True, exist_ok=False)
    for directory, dirs, names in os.walk(source):
        current = Path(directory)
        relative = current.relative_to(source)
        target_dir = destination / relative
        target_dir.mkdir(parents=True, exist_ok=True)
        dirs[:] = sorted(d for d in dirs if not should_skip(Path(d)))
        for name in sorted(names):
            src = current / name
            dst = target_dir / name
            if src.is_symlink():
                dst.symlink_to(os.readlink(src))
            elif src.is_file():
                try:
                    os.link(src, dst)
                except OSError:
                    shutil.copy2(src, dst)


def snapshot_tree(source: Path, destination: Path) -> None:
    source = source.resolve()
    if not source.is_dir():
        raise SystemExit(f"source is not a directory: {source}")
    if destination.exists():
        raise SystemExit(f"destination already exists: {destination}")
    destination.parent.mkdir(parents=True, exist_ok=True)
    # A source snapshot must survive edits to the live checkout.  Hard-link
    # clones are reserved for the short-lived per-repetition workspaces below.
    destination.mkdir(parents=True, exist_ok=False)
    for directory, dirs, names in os.walk(source):
        current = Path(directory)
        relative = current.relative_to(source)
        target_dir = destination / relative
        target_dir.mkdir(parents=True, exist_ok=True)
        dirs[:] = sorted(d for d in dirs if not should_skip(Path(d)))
        for name in sorted(names):
            src = current / name
            dst = target_dir / name
            if src.is_symlink():
                dst.symlink_to(os.readlink(src))
            elif src.is_file():
                shutil.copy2(src, dst)


def write_text(path: Path, text: str) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(text, encoding="utf-8")


def package_name(index: int) -> str:
    return f"p{index // 50:03d}"


def previous_local(index: int) -> int | None:
    return index - 1 if index % 50 else None


def go_source(index: int, shape: str) -> str:
    pkg, typ, prev = package_name(index), f"Service{index:04d}", previous_local(index)
    dependency = ""
    if shape == "helper-edge" and prev is not None:
        dependency = f"\n\tdependency *Service{prev:04d}"
    if shape == "guard":
        body = """\tif value < 0 { return 0, fmt.Errorf(\"negative\") }
\tif value > 100000 { return 0, fmt.Errorf(\"too large\") }
\ts.total += value
\treturn s.total, nil"""
    elif shape == "dispatch":
        body = """\tswitch mode {
\tcase 0:
\t\ts.total += value
\tcase 1:
\t\ts.total -= value
\tcase 2:
\t\tif value%2 == 0 { s.total += value / 2 } else { s.total += value * 2 }
\tdefault:
\t\treturn 0, fmt.Errorf(\"unknown mode\")
\t}
\treturn s.total, nil"""
    elif shape == "shared-state":
        body = """\tif value < 0 { return 0, fmt.Errorf(\"negative\") }
\tif value%2 == 0 {
\t\tif value%3 == 0 {
\t\t\tif value%5 == 0 { s.total += value } else { s.total += value / 2 }
\t\t} else { s.total += value + 1 }
\t} else if value%3 == 0 { s.total -= value } else { s.total += value * 2 }
\treturn s.total, nil"""
    else:
        dependency_value = f"\n\tif s.dependency != nil {{ s.total += s.dependency.Read() % 7 }}" if prev is not None else ""
        body = f"""\tif value < 0 {{ return 0, fmt.Errorf(\"negative\") }}
\tadjusted := value + {index % 11}
\tif adjusted%2 == 0 {{ adjusted /= 2 }} else {{ adjusted = adjusted*2 + 1 }}
\ts.total += adjusted{dependency_value}
\treturn s.total, nil"""
    return f'''package {pkg}

import "fmt"

type {typ} struct {{
\ttotal int{dependency}
}}

func (s *{typ}) Apply(mode, value int) (int, error) {{
{body}
}}

func (s *{typ}) Read() int {{ return s.total }}
'''


def java_source(index: int, shape: str) -> str:
    pkg, typ, prev = package_name(index), f"Service{index:04d}", previous_local(index)
    dependency = f"\n    private Service{prev:04d} dependency;" if shape == "helper-edge" and prev is not None else ""
    if shape == "guard":
        body = """        if (value < 0) throw new IllegalArgumentException("negative");
        if (value > 100000) throw new IllegalArgumentException("too large");
        total += value;
        return total;"""
    elif shape == "dispatch":
        body = """        switch (mode) {
            case 0 -> total += value;
            case 1 -> total -= value;
            case 2 -> total += value % 2 == 0 ? value / 2 : value * 2;
            default -> throw new IllegalArgumentException("unknown mode");
        }
        return total;"""
    elif shape == "shared-state":
        body = """        if (value < 0) throw new IllegalArgumentException("negative");
        if (value % 2 == 0) {
            if (value % 3 == 0) {
                if (value % 5 == 0) total += value; else total += value / 2;
            } else total += value + 1;
        } else if (value % 3 == 0) total -= value; else total += value * 2;
        return total;"""
    else:
        dependency_value = f"\n        if (dependency != null) total += dependency.read() % 7;" if prev is not None else ""
        body = f"""        if (value < 0) throw new IllegalArgumentException("negative");
        int adjusted = value + {index % 11};
        adjusted = adjusted % 2 == 0 ? adjusted / 2 : adjusted * 2 + 1;
        total += adjusted;{dependency_value}
        return total;"""
    return f'''package {pkg};

final class {typ} {{
    private int total;{dependency}

    int apply(int mode, int value) {{
{body}
    }}

    int read() {{ return total; }}
}}
'''


def typescript_source(index: int, shape: str) -> str:
    pkg, typ, prev = package_name(index), f"Service{index:04d}", previous_local(index)
    import_line = f'import type {{ Service{prev:04d} }} from "./service{prev:04d}.js";\n\n' if shape == "helper-edge" and prev is not None else ""
    dependency = f"\n  private dependency?: Service{prev:04d};" if shape == "helper-edge" and prev is not None else ""
    if shape == "guard":
        body = """    if (value < 0) throw new Error("negative");
    if (value > 100000) throw new Error("too large");
    this.total += value;
    return this.total;"""
    elif shape == "dispatch":
        body = """    switch (mode) {
      case 0: this.total += value; break;
      case 1: this.total -= value; break;
      case 2: this.total += value % 2 === 0 ? value / 2 : value * 2; break;
      default: throw new Error("unknown mode");
    }
    return this.total;"""
    elif shape == "shared-state":
        body = """    if (value < 0) throw new Error("negative");
    if (value % 2 === 0) {
      if (value % 3 === 0) {
        if (value % 5 === 0) this.total += value; else this.total += value / 2;
      } else this.total += value + 1;
    } else if (value % 3 === 0) this.total -= value; else this.total += value * 2;
    return this.total;"""
    else:
        dependency_value = "\n    if (this.dependency) this.total += this.dependency.read() % 7;" if prev is not None else ""
        body = f"""    if (value < 0) throw new Error("negative");
    let adjusted = value + {index % 11};
    adjusted = adjusted % 2 === 0 ? adjusted / 2 : adjusted * 2 + 1;
    this.total += adjusted;{dependency_value}
    return this.total;"""
    return f'''{import_line}export class {typ} {{
  private total = 0;{dependency}

  apply(mode: number, value: number): number {{
{body}
  }}

  read(): number {{ return this.total; }}
}}
'''


def rust_source(index: int, shape: str) -> str:
    pkg, typ, prev = package_name(index), f"Service{index:04d}", previous_local(index)
    import_line = f"use crate::{pkg}::service{prev:04d}::Service{prev:04d};\n\n" if shape == "helper-edge" and prev is not None else ""
    dependency = f"\n    dependency: Option<Service{prev:04d}>," if shape == "helper-edge" and prev is not None else ""
    if shape == "guard":
        body = """        if value < 0 || value > 100000 { return Err("invalid"); }
        self.total += value;
        Ok(self.total)"""
    elif shape == "dispatch":
        body = """        match mode {
            0 => self.total += value,
            1 => self.total -= value,
            2 if value % 2 == 0 => self.total += value / 2,
            2 => self.total += value * 2,
            _ => return Err("unknown mode"),
        }
        Ok(self.total)"""
    elif shape == "shared-state":
        body = """        if value < 0 { return Err("negative"); }
        if value % 2 == 0 {
            if value % 3 == 0 {
                if value % 5 == 0 { self.total += value; } else { self.total += value / 2; }
            } else { self.total += value + 1; }
        } else if value % 3 == 0 { self.total -= value; } else { self.total += value * 2; }
        Ok(self.total)"""
    else:
        dependency_value = f"\n        if let Some(previous) = &self.dependency {{ self.total += previous.read() % 7; }}" if prev is not None else ""
        body = f"""        if value < 0 {{ return Err("negative"); }}
        let mut adjusted = value + {index % 11};
        adjusted = if adjusted % 2 == 0 {{ adjusted / 2 }} else {{ adjusted * 2 + 1 }};
        self.total += adjusted;{dependency_value}
        Ok(self.total)"""
    return f'''{import_line}pub struct {typ} {{
    total: i32,{dependency}
}}

impl {typ} {{
    pub fn apply(&mut self, mode: i32, value: i32) -> Result<i32, &'static str> {{
{body}
    }}

    pub fn read(&self) -> i32 {{ self.total }}
}}
'''


def generate_workload(destination: Path, files_per_language: int) -> dict[str, object]:
    if files_per_language < 1:
        raise SystemExit("--files-per-language must be positive")
    if destination.exists():
        raise SystemExit(f"destination already exists: {destination}")
    destination.mkdir(parents=True)
    write_text(destination / "go.mod", "module example.com/structural-perf\n\ngo 1.23\n")
    write_text(destination / "tsconfig.json", json.dumps({"compilerOptions": {"target": "ES2022", "module": "NodeNext", "moduleResolution": "NodeNext", "strict": True}}) + "\n")
    write_text(destination / "Cargo.toml", "[package]\nname = \"structural_perf\"\nversion = \"0.1.0\"\nedition = \"2021\"\n")
    shape_counts = {language: collections.Counter() for language in LANGUAGES}
    for language, extension, renderer in (("go", ".go", go_source), ("java", ".java", java_source), ("typescript", ".ts", typescript_source), ("rust", ".rs", rust_source)):
        for index in range(files_per_language):
            shape = SHAPES[index % len(SHAPES)]
            path = destination / language / package_name(index) / f"service{index:04d}{extension}"
            write_text(path, renderer(index, shape))
            shape_counts[language][shape] += 1
    inventory = source_inventory(destination)
    inventory["shape_counts"] = {language: dict(counts) for language, counts in shape_counts.items()}
    return inventory


def process_tree_cpu_seconds(root_pid: int) -> float | None:
    """Return summed CPU time for a process and its current descendants."""
    try:
        completed = subprocess.run(
            ["ps", "-axo", "pid=,ppid=,time="],
            check=False,
            stdout=subprocess.PIPE,
            stderr=subprocess.DEVNULL,
            text=True,
        )
    except OSError:
        return None
    rows: dict[int, tuple[int, str]] = {}
    for line in completed.stdout.splitlines():
        fields = line.split()
        if len(fields) != 3:
            continue
        try:
            pid, ppid = int(fields[0]), int(fields[1])
        except ValueError:
            continue
        rows[pid] = (ppid, fields[2])
    pids = {root_pid}
    changed = True
    while changed:
        changed = False
        for pid, (ppid, _) in rows.items():
            if ppid in pids and pid not in pids:
                pids.add(pid)
                changed = True
    total = 0.0
    for pid in pids:
        value = rows.get(pid, (0, ""))[1]
        parts = value.split("-")
        days = int(parts[0]) if len(parts) == 2 and parts[0].isdigit() else 0
        clock = parts[-1].split(":")
        try:
            if len(clock) == 3:
                hours, minutes, seconds = int(clock[0]), int(clock[1]), float(clock[2])
            elif len(clock) == 2:
                hours, minutes, seconds = 0, int(clock[0]), float(clock[1])
            else:
                continue
            total += days * 86400 + hours * 3600 + minutes * 60 + seconds
        except ValueError:
            continue
    return total if root_pid in rows else None


def stream_metrics(path: Path) -> tuple[int, str, int, int, dict[str, object] | None, int, int, int]:
    digest = hashlib.sha256()
    size = markers = 0
    compressed = 0
    analyzer_failures = 0
    execution_plan_count = 0
    execution_invocation_count = 0
    tail = bytearray()
    marker_suffix = b""
    marker_tokens = (b'"evidence"', b"analyzer_failed", b'"type":"execution_plan"', b'"invocation_id"')
    marker_counts = [0] * len(marker_tokens)
    with path.open("rb") as source, tempfile.NamedTemporaryFile(prefix="slopwatch-report-", suffix=".json.gz", delete=False) as compressed_file:
        compressed_path = Path(compressed_file.name)
        with gzip.GzipFile(fileobj=compressed_file, mode="wb", compresslevel=1) as encoder:
            while chunk := source.read(1024 * 1024):
                digest.update(chunk)
                size += len(chunk)
                joined = marker_suffix + chunk.lower()
                for index, token in enumerate(marker_tokens):
                    marker_counts[index] += joined.count(token) - marker_suffix.count(token)
                marker_suffix = joined[-(max(map(len, marker_tokens)) - 1):]
                tail.extend(chunk)
                if len(tail) > 4 * 1024 * 1024:
                    del tail[:-4 * 1024 * 1024]
                encoder.write(chunk)
        compressed = compressed_path.stat().st_size
    compressed_path.unlink(missing_ok=True)
    markers, analyzer_failures, execution_plan_count, execution_invocation_count = marker_counts
    summary: dict[str, object] | None = None
    marker = b'"summary"'
    index = tail.rfind(marker)
    if index >= 0:
        try:
            start = tail.index(b":", index) + 1
            summary_value, _ = json.JSONDecoder().raw_decode(bytes(tail[start:]).decode("utf-8"))
            if isinstance(summary_value, dict):
                summary = summary_value
        except (ValueError, UnicodeDecodeError):
            summary = None
    return size, digest.hexdigest(), compressed, markers, summary, analyzer_failures, execution_plan_count, execution_invocation_count


def private_child_env(cache_home: Path) -> dict[str, str]:
    # Keep the caller's normal Slopwatch cache and user identity.  The source
    # snapshot path is unique per measurement, so cache view keys cannot
    # collide with an earlier workspace.  Only terminal rendering is fixed.
    env = os.environ.copy()
    env["TERM"] = "dumb"
    return env


def usage_metrics(usage: resource.struct_rusage | None) -> tuple[float, float, int]:
    if usage is None:
        return 0.0, 0.0, 0
    rss = int(usage.ru_maxrss)
    if sys.platform != "darwin":
        rss *= 1024
    return float(usage.ru_utime), float(usage.ru_stime), rss


def execute_json(binary: Path, workspace: Path, cache_home: Path, phase: str, repetition: int, targets: list[str], languages: str, timeout: float, raw_dir: Path, use_cache: bool) -> Capture:
    raw_dir.mkdir(parents=True, exist_ok=True)
    stdout_path = raw_dir / f"{phase}-{repetition}.json"
    stderr_path = raw_dir / f"{phase}-{repetition}.stderr"
    command = [str(binary), "--format", "json", "--limit", "0", "--config", str(cache_home / "preferences.toml")]
    if languages:
        command.extend(["--languages", languages])
    if use_cache:
        command.append("--use-cache")
    command.extend(targets or ["."])
    start = time.monotonic()
    returncode = -1
    error_tail = ""
    usage: resource.struct_rusage | None = None
    try:
        with stdout_path.open("wb") as stdout, stderr_path.open("wb") as stderr:
            process = subprocess.Popen(command, cwd=workspace, env=private_child_env(cache_home), stdout=stdout, stderr=stderr, start_new_session=True)
            deadline = time.monotonic() + timeout
            while True:
                try:
                    waited, status, usage = os.wait4(process.pid, os.WNOHANG)
                except ChildProcessError:
                    returncode = 125
                    break
                if waited == process.pid:
                    returncode = os.waitstatus_to_exitcode(status)
                    break
                if time.monotonic() >= deadline:
                    try:
                        os.killpg(process.pid, signal.SIGTERM)
                    except ProcessLookupError:
                        process.send_signal(signal.SIGTERM)
                    grace_deadline = time.monotonic() + 10.0
                    while time.monotonic() < grace_deadline:
                        try:
                            waited, status, usage = os.wait4(process.pid, os.WNOHANG)
                        except ChildProcessError:
                            break
                        if waited == process.pid:
                            break
                        time.sleep(0.05)
                    else:
                        try:
                            os.killpg(process.pid, signal.SIGKILL)
                        except ProcessLookupError:
                            pass
                        try:
                            _, status, usage = os.wait4(process.pid, 0)
                        except ChildProcessError:
                            status = 0
                    returncode = 124
                    break
                time.sleep(0.05)
        error_tail = stderr_path.read_text(encoding="utf-8", errors="replace")[-4096:]
    finally:
        elapsed = time.monotonic() - start
    timed_user, timed_system, timed_rss = usage_metrics(usage)
    report_bytes = stdout_path.stat().st_size if stdout_path.exists() else 0
    stdout_hash = None
    compressed_bytes = markers = 0
    report_summary = None
    analyzer_failures = 0
    execution_plan_count = 0
    execution_invocation_count = 0
    if report_bytes:
        report_bytes, stdout_hash, compressed_bytes, markers, report_summary, analyzer_failures, execution_plan_count, execution_invocation_count = stream_metrics(stdout_path)
    stderr_bytes = stderr_path.stat().st_size if stderr_path.exists() else 0
    if returncode == 0 and report_bytes == 0:
        error_tail = error_tail or "empty JSON report"
        returncode = 2
    if analyzer_failures:
        error_tail = error_tail or f"report contains {analyzer_failures} analyzer_failed diagnostic markers"
        returncode = 2
    capture = Capture(phase, repetition, command, str(workspace), elapsed, timed_user, timed_system, timed_rss, returncode, report_bytes, stderr_bytes, stdout_hash, markers, compressed_bytes, error_tail=error_tail, cache_mode=("read" if use_cache else "write-only"), report_summary=report_summary, analyzer_failures=analyzer_failures, execution_plan_count=execution_plan_count, execution_invocation_count=execution_invocation_count)
    return capture


ANSI_ESCAPE = re.compile(r"\x1b(?:\[[0-?]*[ -/]*[@-~]|\][^\x07]*(?:\x07|\x1b\\))")


def terminal_screen(data: bytes, rows: int = 40, columns: int = 160) -> list[str]:
    """Apply the small cursor subset used by the follow renderer."""
    screen = [[" "] * columns for _ in range(rows)]
    row = column = 0
    text = data.decode("utf-8", errors="replace")
    index = 0
    while index < len(text):
        value = text[index]
        if value == "\x1b":
            match = ANSI_ESCAPE.match(text, index)
            if match is None:
                index += 1
                continue
            sequence = match.group(0)
            if sequence.startswith("\x1b[") and sequence.endswith(("H", "f")):
                values = sequence[2:-1].split(";")
                try:
                    row = max(0, int(values[0] or "1") - 1)
                    column = max(0, int(values[1] or "1") - 1) if len(values) > 1 else 0
                except ValueError:
                    pass
            elif sequence.startswith("\x1b[") and sequence.endswith("J") and sequence[2:-1] in ("2", "3"):
                screen = [[" "] * columns for _ in range(rows)]
                row = column = 0
            elif sequence.startswith("\x1b[") and sequence.endswith("K"):
                for position in range(column, columns):
                    screen[min(row, rows - 1)][position] = " "
            elif sequence.startswith("\x1b[") and sequence.endswith(("A", "B", "C", "D")):
                try:
                    amount = int(sequence[2:-1] or "1")
                except ValueError:
                    amount = 1
                if sequence.endswith("A"):
                    row -= amount
                elif sequence.endswith("B"):
                    row += amount
                elif sequence.endswith("C"):
                    column += amount
                else:
                    column -= amount
                row = max(0, min(rows - 1, row))
                column = max(0, min(columns - 1, column))
            index = match.end()
            continue
        if value == "\r":
            column = 0
        elif value == "\n":
            row = min(rows - 1, row + 1)
        elif value == "\b":
            column = max(0, column - 1)
        elif value.isprintable() and row < rows and column < columns:
            screen[row][column] = value
            column += 1
        index += 1
    return ["".join(line).rstrip() for line in screen]


def dashboard_frame_complete(path: Path, expected_files: int | None) -> bool:
    if expected_files is None or not path.exists():
        return False
    plain = "\n".join(terminal_screen(path.read_bytes())).upper()
    if f"FILES: {expected_files:,}" not in plain:
        return False
    header = "\n".join(plain.splitlines()[:4])
    return not any(token in header for token in ("SCANNING", "VERIFY", "REFRESH", "CACHED", "PROV"))


def execute_idle(binary: Path, workspace: Path, cache_home: Path, repetition: int, targets: list[str], languages: str, startup_wait: float, idle_seconds: float, timeout: float, raw_dir: Path, expected_files: int | None) -> Capture:
    raw_dir.mkdir(parents=True, exist_ok=True)
    ansi_path = raw_dir / f"idle-{repetition}.ansi"
    command = [str(binary), "--follow", "--limit", "0", "--config", str(cache_home / "preferences.toml")]
    if languages:
        command.extend(["--languages", languages])
    command.extend(targets or ["."])
    start = time.monotonic()
    pid, fd = pty.fork()
    if pid == 0:
        os.chdir(workspace)
        os.execve(str(binary), command, private_child_env(cache_home))
    termios.tcsetattr(fd, termios.TCSANOW, termios.tcgetattr(fd))
    # Keep the same 160x40 terminal used by the existing capture_ui.py.
    import fcntl
    fcntl.ioctl(fd, termios.TIOCSWINSZ, struct.pack("HHHH", 40, 160, 0, 0))
    output_bytes = 0
    settled_at: float | None = None
    idle_cpu_seconds: float | None = None
    last_output = start
    startup_deadline = start + startup_wait
    hard_deadline = start + timeout
    with ansi_path.open("wb") as log:
        while time.monotonic() < startup_deadline and time.monotonic() < hard_deadline:
            ready, _, _ = select.select([fd], [], [], 0.1)
            if ready:
                try:
                    data = os.read(fd, 1024 * 1024)
                except OSError:
                    break
                if not data:
                    break
                output_bytes += len(data)
                log.write(data)
                log.flush()
                last_output = time.monotonic()
            elif time.monotonic() - last_output >= 2.0 and dashboard_frame_complete(ansi_path, expected_files):
                settled_at = time.monotonic()
                break
        if settled_at is not None:
            idle_cpu_before = process_tree_cpu_seconds(pid)
            idle_deadline = min(settled_at + idle_seconds, hard_deadline)
            while time.monotonic() < idle_deadline:
                ready, _, _ = select.select([fd], [], [], 0.1)
                if not ready:
                    continue
                try:
                    data = os.read(fd, 1024 * 1024)
                except OSError:
                    break
                if not data:
                    break
                output_bytes += len(data)
                log.write(data)
                log.flush()
            idle_cpu_after = process_tree_cpu_seconds(pid)
            if idle_cpu_before is not None and idle_cpu_after is not None:
                idle_cpu_seconds = max(0.0, idle_cpu_after - idle_cpu_before)
            else:
                idle_cpu_seconds = None
        else:
            # The initial analysis did not settle before startup timeout; this
            # is a bounded failure rather than a fabricated idle result.
            idle_cpu_seconds = None
        try:
            os.write(fd, b"q")
        except OSError:
            pass
    # Drain the PTY before wait4 so the dashboard cannot block on a full pipe.
    drain_deadline = time.monotonic() + 10.0
    while time.monotonic() < drain_deadline:
        ready, _, _ = select.select([fd], [], [], 0.1)
        if not ready:
            continue
        try:
            data = os.read(fd, 1024 * 1024)
        except OSError:
            break
        if not data:
            break
        output_bytes += len(data)
        with ansi_path.open("ab") as log:
            log.write(data)
    os.close(fd)
    status = None
    usage: resource.struct_rusage | None = None
    reaped = False
    wait_deadline = time.monotonic() + 10.0
    while time.monotonic() < wait_deadline:
        try:
            waited, status, usage = os.wait4(pid, os.WNOHANG)
        except ChildProcessError:
            status = 1
            break
        if waited == pid:
            reaped = True
            break
        time.sleep(0.05)
    if not reaped:
        try:
            try:
                os.killpg(os.getpgid(pid), signal.SIGTERM)
            except ProcessLookupError:
                os.kill(pid, signal.SIGTERM)
            grace_deadline = time.monotonic() + 10.0
            while time.monotonic() < grace_deadline:
                waited, status, usage = os.wait4(pid, os.WNOHANG)
                if waited == pid:
                    break
                time.sleep(0.05)
            else:
                try:
                    os.killpg(os.getpgid(pid), signal.SIGKILL)
                except ProcessLookupError:
                    os.kill(pid, signal.SIGKILL)
                _, status, usage = os.wait4(pid, 0)
        except (ChildProcessError, ProcessLookupError):
            status = 1
    try:
        returncode = os.waitstatus_to_exitcode(status)
    except (TypeError, ValueError):
        returncode = 1
    elapsed = time.monotonic() - start
    timed_user, timed_system, timed_rss = usage_metrics(usage)
    idle_observed = settled_at is not None and idle_cpu_seconds is not None
    idle_error = "" if idle_observed else "idle interval unavailable (current row-count frame, settled output, or CPU sampling missing)"
    return Capture("idle", repetition, command, str(workspace), elapsed, timed_user, timed_system, timed_rss, returncode, output_bytes, 0, None, 0, 0, idle_seconds=idle_seconds, startup_seconds=startup_wait, idle_cpu_seconds=idle_cpu_seconds, settled_seconds=(settled_at - start if settled_at is not None else None), idle_observed=idle_observed, error_tail=idle_error, cache_mode="read")


def prepare_preferences(cache_home: Path) -> None:
    path = cache_home / "preferences.toml"
    write_text(path, "version = 1\n\n[files]\nhonor_gitignore = false\n\n[appearance]\ntheme = 'dark'\n")


def choose_edit(workspace: Path) -> tuple[Path, bytes, bytes, str]:
    """Select a deterministic parseable code mutation for paired runs."""
    slopwatch_path = workspace / "go/internal/scoring/catalog.go"
    slopwatch_before, slopwatch_after = b"return 1", b"return 2"
    if slopwatch_path.is_file() and slopwatch_before in slopwatch_path.read_bytes():
        return slopwatch_path, slopwatch_before, slopwatch_after, "go/internal/scoring/catalog.go: return 1 -> return 2"
    kafka_path = workspace / "metadata/src/main/java/org/apache/kafka/controller/BrokerHeartbeatManager.java"
    kafka_before = b"return controlledShutdownOffset >= 0;"
    kafka_after = b"return controlledShutdownOffset > 0;"
    if kafka_path.is_file() and kafka_before in kafka_path.read_bytes():
        return kafka_path, kafka_before, kafka_after, "metadata/src/main/java/org/apache/kafka/controller/BrokerHeartbeatManager.java: >= 0 -> > 0"
    candidates = [p for p in iter_files(workspace) if p.suffix.lower() in LANG_EXTENSIONS]
    for candidate in candidates:
        data = candidate.read_bytes()
        if candidate.suffix.lower() == ".go" and b"if value < 0 {" in data:
            return candidate, b"if value < 0 {", b"if value <= 0 {", f"{candidate.relative_to(workspace)}: value < 0 -> value <= 0"
    raise SystemExit(f"no deterministic code edit target under {workspace}")


def measure(args: argparse.Namespace) -> None:
    binary = args.binary.resolve()
    workspace = args.workspace.resolve()
    output = args.output.resolve()
    if not binary.is_file() or not os.access(binary, os.X_OK):
        raise SystemExit(f"binary is not executable: {binary}")
    if not workspace.is_dir():
        raise SystemExit(f"workspace is not a directory: {workspace}")
    if args.repetitions < 1:
        raise SystemExit("--repetitions must be positive")
    output.parent.mkdir(parents=True, exist_ok=True)
    inventory = source_inventory(workspace)
    if not inventory["files"]:
        raise SystemExit("workspace has no supported-language files")
    languages = args.languages
    targets = args.target
    run_root = Path(tempfile.mkdtemp(prefix="slopwatch-structural-perf-", dir="/private/tmp"))
    captures: list[Capture] = []
    edit_source, edit_before, edit_after, edit_description = choose_edit(workspace)
    source_bytes = edit_source.read_bytes()
    try:
        for repetition in range(1, args.repetitions + 1):
            rep_root = run_root / f"rep-{repetition}"
            rep_root.mkdir()
            measured_workspace = rep_root / "workspace"
            clone_tree_with_links(workspace, measured_workspace)
            cache_home = rep_root / "home"
            cache_home.mkdir()
            prepare_preferences(cache_home)
            cold = execute_json(binary, measured_workspace, cache_home, "cold", repetition, targets, languages, args.timeout_seconds, rep_root / "raw", False)
            captures.append(cold)
            warm = execute_json(binary, measured_workspace, cache_home, "warm", repetition, targets, languages, args.timeout_seconds, rep_root / "raw", True)
            captures.append(warm)
            changed = measured_workspace / edit_source.relative_to(workspace)
            if not changed.exists():
                raise SystemExit(f"edit path missing from clone: {changed}")
            backup = changed.read_bytes()
            if edit_before not in backup:
                raise SystemExit(f"edit marker missing from clone: {changed}")
            edited = changed.with_name(changed.name + ".performance-edit")
            edited.write_bytes(backup.replace(edit_before, edit_after, 1))
            os.replace(edited, changed)
            edit = execute_json(binary, measured_workspace, cache_home, "edit", repetition, targets, languages, args.timeout_seconds, rep_root / "raw", True)
            captures.append(edit)
            restored = changed.with_name(changed.name + ".performance-restore")
            restored.write_bytes(backup)
            os.replace(restored, changed)
            expected_files = None
            if edit.report_summary is not None:
                value = edit.report_summary.get("discovered_source_count")
                if isinstance(value, int):
                    expected_files = value
            idle = execute_idle(binary, measured_workspace, cache_home, repetition, targets, languages, args.startup_seconds, args.idle_seconds, args.timeout_seconds, rep_root / "raw", expected_files)
            captures.append(idle)
            if not args.keep_raw:
                shutil.rmtree(rep_root / "raw", ignore_errors=True)
    finally:
        if not args.keep_raw:
            shutil.rmtree(run_root, ignore_errors=True)
        else:
            print(f"raw captures retained at {run_root}", file=sys.stderr)
    if edit_source.read_bytes() != source_bytes:
        raise SystemExit(f"measurement changed source snapshot: {edit_source}")
    result = {
        "schema_version": 1,
        "driver": "tools/structural_scoring_performance.py",
        "label": args.label,
        "binary": str(binary),
        "binary_sha256": hashlib.sha256(binary.read_bytes()).hexdigest(),
        "workspace": str(workspace),
        "workspace_inventory": inventory,
        "targets": targets or ["."],
        "languages": languages or "auto",
        "repetitions": args.repetitions,
        "contract": {
            "cold_p95_seconds": 120,
            "warm_seconds": 2,
            "implementation_edit_seconds": 5,
            "contract_edit_seconds": 10,
            "peak_rss_bytes": 2 * 1024**3,
            "idle_cpu_delta_seconds": 0.1,
            "candidate_overhead_fraction": 0.10,
        },
        "edit_path": str(edit_source.relative_to(workspace)),
        "edit_source_sha256": hashlib.sha256(source_bytes).hexdigest(),
        "edit_mutation": {
            "description": edit_description,
            "before": edit_before.decode("utf-8"),
            "after": edit_after.decode("utf-8"),
            "before_sha256": hashlib.sha256(edit_before).hexdigest(),
            "after_sha256": hashlib.sha256(edit_after).hexdigest(),
        },
        "captures": [capture.as_dict() for capture in captures],
        "notes": [
            "Synthetic 30K evidence is a capacity adjunct; real repository results are the primary structural-scoring comparison.",
            "Idle capture requires a current row-count frame with no SCANNING, VERIFY, REFRESH, CACHED, or PROV status, then waits for two quiet seconds before recording the requested interval CPU delta; idle_observed=false means completion evidence was unavailable.",
            "User/system CPU and peak RSS come from os.wait4 for each direct CLI child (including waited child accounting); output bytes include the complete JSON report for CLI phases.",
        ],
    }
    output.write_text(json.dumps(result, indent=2, sort_keys=True) + "\n", encoding="utf-8")


def main() -> None:
    args = parse_args()
    if args.command == "snapshot":
        snapshot_tree(args.source, args.destination)
        print(json.dumps({"destination": str(args.destination.resolve()), "inventory": source_inventory(args.destination.resolve())}, indent=2, sort_keys=True))
    elif args.command == "generate":
        print(json.dumps({"destination": str(args.destination.resolve()), "inventory": generate_workload(args.destination.resolve(), args.files_per_language)}, indent=2, sort_keys=True))
    else:
        measure(args)


if __name__ == "__main__":
    main()
