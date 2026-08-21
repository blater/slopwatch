from __future__ import annotations

import os
import sys
from pathlib import Path

from setuptools import setup
from setuptools._distutils.errors import DistutilsExecError
from setuptools.command.build_py import build_py
from wheel.bdist_wheel import bdist_wheel

sys.path.insert(0, str(Path(__file__).parent))
from slopslap_build import (
    StructuralAnalyzerBuildError, build_java_adapter, build_rust_adapter,
    build_structural_analyzer,
)


class BuildPyWithStructuralAnalyzer(build_py):
  """Compile the first-party Go analyzer into the installed Python package."""

  def _binary_name(self) -> str:
    return "slopslap-structural.exe" if os.name == "nt" else "slopslap-structural"

  def _target(self) -> Path:
    if getattr(self, "editable_mode", False):
      return Path(__file__).parent / "analyzers" / "structural" / self._binary_name()
    return (Path(self.build_lib).resolve() / "analyzers" / "structural"
            / self._binary_name())

  def _rust_target(self) -> Path:
    name = "slopslap-structural-rust.exe" if os.name == "nt" \
        else "slopslap-structural-rust"
    return self._target().with_name(name)

  def _java_target(self) -> Path:
    return self._target().with_name("slopslap-structural-java.jar")

  def run(self) -> None:
    try:
      build_structural_analyzer(self._target())
      build_rust_adapter(self._rust_target())
      build_java_adapter(self._java_target())
    except StructuralAnalyzerBuildError as error:
      raise DistutilsExecError(str(error)) from error
    super().run()


class PlatformWheel(bdist_wheel):
  """Tag wheels for the platform that produced the bundled executable."""

  def finalize_options(self) -> None:
    super().finalize_options()
    self.root_is_pure = False

  def get_tag(self) -> tuple[str, str, str]:
    _, _, platform = super().get_tag()
    return "py3", "none", platform


setup(cmdclass={
    "build_py": BuildPyWithStructuralAnalyzer,
    "bdist_wheel": PlatformWheel,
})
