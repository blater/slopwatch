"""Safe external-process adapter for analyzers."""

from __future__ import annotations

import os
import shutil
import subprocess
import tempfile
import threading
import time
from dataclasses import dataclass
from pathlib import Path
from typing import Mapping, Sequence

from .errors import AnalyzerExecutionError, ProtocolError, ValidationError
from .protocol import AnalyzerRequest, ProtocolReader, ProtocolRecord

_SAFE_ENV = ("LANG", "LC_ALL", "LC_CTYPE", "SYSTEMROOT", "WINDIR")


@dataclass(frozen=True)
class ProcessResult:
    records: tuple[ProtocolRecord, ...]
    stderr: str
    returncode: int
    elapsed_seconds: float


class AnalyzerProcessAdapter:
    """Run an explicit analyzer command without shell expansion or repo writes."""

    def __init__(self, *, timeout_seconds: float = 60, kill_grace_seconds: float = 1,
                 max_stderr_bytes: int = 65_536) -> None:
        if timeout_seconds <= 0 or kill_grace_seconds < 0 or max_stderr_bytes <= 0:
            raise ValidationError("invalid process adapter limits")
        self.timeout_seconds = timeout_seconds
        self.kill_grace_seconds = kill_grace_seconds
        self.max_stderr_bytes = max_stderr_bytes

    def run(self, command: Sequence[str | os.PathLike[str]], request: AnalyzerRequest, *,
            cancellation: threading.Event | None = None,
            environment: Mapping[str, str] | None = None) -> ProcessResult:
        if not command or any(not isinstance(item, (str, os.PathLike)) or not str(item) for item in command):
            raise ValidationError("analyzer command must be a non-empty explicit argv")
        argv = [os.fspath(item) for item in command]
        env = self._sanitized_environment(environment)
        started = time.monotonic()
        workdir = Path(tempfile.mkdtemp(prefix="slopscout-analyzer-"))
        (workdir / "cache").mkdir()
        env["SLOPSCOUT_WORK_DIR"] = str(workdir)
        env["SLOPSCOUT_CACHE_DIR"] = str(workdir / "cache")
        process: subprocess.Popen[bytes] | None = None
        try:
            process = subprocess.Popen(argv, stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                                       cwd=workdir, env=env, shell=False)
            assert process.stdin is not None
            process.stdin.write(request.to_ndjson())
            process.stdin.close()
            stdout_chunks: list[bytes] = []
            stderr_chunks: list[bytes] = []
            output_too_large = threading.Event()
            stdout_thread = threading.Thread(target=self._collect, args=(process.stdout, stdout_chunks,
                                                                          32 * 1_048_576, output_too_large), daemon=True)
            stderr_thread = threading.Thread(target=self._collect, args=(process.stderr, stderr_chunks,
                                                                          self.max_stderr_bytes, None), daemon=True)
            stdout_thread.start()
            stderr_thread.start()
            while process.poll() is None:
                if output_too_large.is_set():
                    self._stop(process)
                    raise AnalyzerExecutionError("analyzer protocol output exceeds configured size limit")
                if cancellation is not None and cancellation.is_set():
                    self._stop(process)
                    raise AnalyzerExecutionError("analyzer execution cancelled")
                if time.monotonic() - started > self.timeout_seconds:
                    self._stop(process)
                    raise AnalyzerExecutionError(f"analyzer timed out after {self.timeout_seconds}s")
                time.sleep(0.01)
            stdout_thread.join(timeout=1)
            stderr_thread.join(timeout=1)
            stdout, stderr = b"".join(stdout_chunks), b"".join(stderr_chunks)
            elapsed = time.monotonic() - started
            stderr_text = stderr[:self.max_stderr_bytes].decode("utf-8", errors="replace")
            if output_too_large.is_set():
                raise AnalyzerExecutionError("analyzer protocol output exceeds configured size limit")
            if process.returncode != 0:
                raise AnalyzerExecutionError(f"analyzer exited with status {process.returncode}: {stderr_text}")
            try:
                records = tuple(ProtocolReader(request).parse_text(stdout.decode("utf-8", errors="strict")))
            except (UnicodeError, ProtocolError) as exc:
                raise AnalyzerExecutionError(f"invalid analyzer protocol: {exc}") from exc
            return ProcessResult(records, stderr_text, process.returncode, elapsed)
        finally:
            if process is not None:
                for stream in (process.stdin, process.stdout, process.stderr):
                    if stream is not None:
                        stream.close()
            shutil.rmtree(workdir, ignore_errors=True)

    @staticmethod
    def _collect(stream: object, chunks: list[bytes], limit: int, overflow: threading.Event | None) -> None:
        if stream is None:
            return
        total = 0
        while True:
            data = stream.read(65_536)  # type: ignore[union-attr]
            if not data:
                return
            total += len(data)
            if total > limit:
                if overflow is not None:
                    overflow.set()
                return
            chunks.append(data)

    def _stop(self, process: subprocess.Popen[bytes]) -> None:
        if process.poll() is not None:
            return
        process.terminate()
        try:
            process.wait(timeout=self.kill_grace_seconds)
        except subprocess.TimeoutExpired:
            process.kill()
            process.wait()

    @staticmethod
    def _sanitized_environment(extra: Mapping[str, str] | None) -> dict[str, str]:
        env = {key: os.environ[key] for key in _SAFE_ENV if key in os.environ}
        # An analyzer gets an explicit minimal PATH.
        env["PATH"] = os.defpath
        if extra:
            for key, value in extra.items():
                if not isinstance(key, str) or not isinstance(value, str) or key in {"PWD", "HOME", "PYTHONPATH"}:
                    raise ValidationError("unsafe analyzer environment override")
                env[key] = value
        return env
