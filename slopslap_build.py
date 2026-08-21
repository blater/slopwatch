"""Build helpers used while creating a platform Slopslap distribution."""

from __future__ import annotations

import os
import shutil
import stat
import subprocess
import tempfile
from pathlib import Path


class StructuralAnalyzerBuildError(RuntimeError):
  pass


def _rust_binary_name() -> str:
  return "slopslap-structural-rust.exe" if os.name == "nt" else "slopslap-structural-rust"


def build_structural_analyzer(target: Path, *, project: Path | None = None) -> Path:
  """Build the bundled, static structural analyzer for the current platform."""
  go = shutil.which("go")
  if go is None:
    raise StructuralAnalyzerBuildError(
        "Go 1.22+ is required when building Slopslap from source; "
        "release wheels include the structural analyzer"
    )
  project = project or Path(__file__).parent / "analyzers" / "structural"
  target = target.resolve()
  target.parent.mkdir(parents=True, exist_ok=True)
  environment = dict(os.environ)
  environment["CGO_ENABLED"] = "0"
  try:
    with tempfile.TemporaryDirectory(prefix="slopslap-go-build-") as cache:
      environment["GOCACHE"] = cache
      subprocess.run(
          [go, "build", "-o", str(target),
           "./cmd/slopslap-structural"],
          cwd=project, env=environment, stdin=subprocess.DEVNULL, check=True,
      )
  except subprocess.CalledProcessError as error:
    raise StructuralAnalyzerBuildError(
        "failed to build the bundled Slopslap structural analyzer"
    ) from error
  target.chmod(target.stat().st_mode | stat.S_IXUSR | stat.S_IXGRP | stat.S_IXOTH)
  return target


def build_rust_adapter(target: Path, *, project: Path | None = None) -> Path:
  """Build the bundled Rust syntax-to-facts adapter for the current platform."""
  cargo = shutil.which("cargo")
  if cargo is None:
    raise StructuralAnalyzerBuildError(
        "Rust 1.78+ is required when building Slopslap from source; "
        "release wheels include the Rust adapter"
    )
  project = project or Path(__file__).parent / "analyzers" / "structural"
  manifest = project / "adapters" / "rust" / "Cargo.toml"
  target = target.resolve()
  target.parent.mkdir(parents=True, exist_ok=True)
  environment = dict(os.environ)
  try:
    with tempfile.TemporaryDirectory(prefix="slopslap-rust-build-") as cargo_target:
      environment["CARGO_TARGET_DIR"] = cargo_target
      subprocess.run(
          [cargo, "build", "--locked", "--release", "--manifest-path", str(manifest)],
          cwd=project, env=environment, stdin=subprocess.DEVNULL, check=True,
      )
      shutil.copy2(Path(cargo_target) / "release" / _rust_binary_name(), target)
  except subprocess.CalledProcessError as error:
    raise StructuralAnalyzerBuildError(
        "failed to build the bundled Slopslap Rust adapter"
    ) from error
  target.chmod(target.stat().st_mode | stat.S_IXUSR | stat.S_IXGRP | stat.S_IXOTH)
  return target
