"""Lazy installation of the analyzer dependencies declared in the catalogue."""

from __future__ import annotations

import os
import shutil
import subprocess
import sys
import tempfile
from dataclasses import dataclass
from enum import Enum
from pathlib import Path
from typing import Any, Callable, Mapping

from slopslap_core import catalog_document
from slopslap_core.catalog import CATALOG_PATH
from slopslap_runtime.errors import RuntimeError as AnalyzerRuntimeError


class InstallationMethod(str, Enum):
  MAVEN = "maven"
  NPM = "npm"
  CARGO = "cargo"
  GO = "go"
  JDK = "jdk"


class ExecutionMethod(str, Enum):
  PYTHON = "python"
  NODE = "node"
  DIRECT = "direct"


class AnalyzerDependencyError(AnalyzerRuntimeError):
  pass


@dataclass(frozen=True)
class AnalyzerDependency:
  language: str
  backend: str
  extensions: tuple[str, ...]
  dependency: str
  version: str
  installation_method: InstallationMethod
  execution_method: ExecutionMethod
  source: Path
  entrypoint: Path
  ready_path: Path


_FIELDS = {
    "language", "backend", "extensions", "dependency", "version", "installation_method",
    "execution_method", "source", "entrypoint", "ready_path",
}


def _dependency(raw: Any, name: str) -> AnalyzerDependency:
  distribution_root = CATALOG_PATH.parent.parent.resolve()
  if not isinstance(raw, Mapping) or set(raw) != _FIELDS:
    raise AnalyzerDependencyError(f"{name} has invalid fields")
  language = str(raw["language"])
  backend = str(raw["backend"])
  if not backend:
    raise AnalyzerDependencyError(f"{name}.backend is invalid")
  extensions = raw["extensions"]
  if not isinstance(extensions, list) or not extensions \
      or any(not isinstance(item, str) or not item.startswith(".") for item in extensions):
    raise AnalyzerDependencyError(f"{name}.extensions is invalid")
  source = (CATALOG_PATH.parent / str(raw["source"])).resolve()
  try:
    source.relative_to(distribution_root)
  except ValueError as error:
    raise AnalyzerDependencyError(f"{name}.source escapes the installation") from error
  return AnalyzerDependency(
      language=language,
      backend=backend,
      extensions=tuple(extensions),
      dependency=str(raw["dependency"]),
      version=str(raw["version"]),
      installation_method=InstallationMethod(str(raw["installation_method"])),
      execution_method=ExecutionMethod(str(raw["execution_method"])),
      source=source,
      entrypoint=Path(str(raw["entrypoint"])),
      ready_path=Path(str(raw["ready_path"])),
  )


def analyzer_dependencies() -> dict[str, AnalyzerDependency]:
  result: dict[str, AnalyzerDependency] = {}
  for index, raw in enumerate(catalog_document()["analyzers"]):
    dependency = _dependency(raw, f"analyzers[{index}]")
    if dependency.language in result:
      raise AnalyzerDependencyError(
          f"duplicate analyzer dependency for {dependency.language}"
      )
    result[dependency.language] = dependency
  return result


def analyzer_backends() -> dict[tuple[str, str], AnalyzerDependency]:
  result = {
      (item.language, item.backend): item for item in analyzer_dependencies().values()
  }
  for index, raw in enumerate(catalog_document()["analyzer_backends"]):
    dependency = _dependency(raw, f"analyzer_backends[{index}]")
    key = (dependency.language, dependency.backend)
    if key in result:
      raise AnalyzerDependencyError(
          f"duplicate analyzer backend for {dependency.language}={dependency.backend}"
      )
    result[key] = dependency
  return result


def analyzer_dependency(language: str, backend: str | None = None) -> AnalyzerDependency:
  if backend is None:
    try:
      return analyzer_dependencies()[language]
    except KeyError as error:
      raise AnalyzerDependencyError(f"no analyzer dependency for {language}") from error
  try:
    return analyzer_backends()[(language, backend)]
  except KeyError as error:
    raise AnalyzerDependencyError(
        f"no analyzer backend {backend!r} for {language}"
    ) from error


def dependency_root() -> Path:
  configured = os.environ.get("SLOPSLAP_DEPENDENCY_ROOT")
  if configured:
    return Path(configured).expanduser().resolve()
  xdg = os.environ.get("XDG_CACHE_HOME")
  base = Path(xdg).expanduser() if xdg and Path(xdg).expanduser().is_absolute() \
      else Path.home() / ".cache"
  return (base / "slopslap" / "dependencies").resolve()


def _installed_project(dependency: AnalyzerDependency) -> Path:
  return dependency_root() / dependency.language / dependency.backend / dependency.version


def _platform_path(project: Path, relative: Path,
                   execution: ExecutionMethod) -> Path:
  path = project / relative
  if os.name == "nt" and execution is ExecutionMethod.DIRECT:
    path = path.with_suffix(".exe")
  return path


def _ready_project(dependency: AnalyzerDependency) -> Path | None:
  for project in (dependency.source, _installed_project(dependency)):
    ready = _platform_path(project, dependency.ready_path,
                           dependency.execution_method)
    entrypoint = _platform_path(project, dependency.entrypoint,
                                dependency.execution_method)
    if ready.is_file() and entrypoint.is_file():
      return project
  return None


def _required_programs(dependency: AnalyzerDependency) -> tuple[str, ...]:
  if dependency.installation_method is InstallationMethod.JDK:
    return ("go", "javac", "jar")
  if dependency.language == "rust" and dependency.source.name == "structural":
    return ("cargo", "go")
  if dependency.installation_method is InstallationMethod.MAVEN:
    return ("java", "mvn")
  if dependency.installation_method is InstallationMethod.NPM:
    return ("node", "npm")
  if dependency.installation_method is InstallationMethod.CARGO:
    return ("cargo",)
  return ("go",)


def _install_commands(dependency: AnalyzerDependency,
                      project: Path) -> tuple[tuple[str, ...], ...]:
  if dependency.installation_method is InstallationMethod.JDK:
    classes = project / "adapters" / "java" / "build" / "classes"
    helper = project / "slopslap-structural-java.jar"
    sources = tuple(str(path) for path in sorted(
        (project / "adapters" / "java" / "src").rglob("*.java")
    ))
    return (
        ("javac", "--release", "17", "-d", str(classes), *sources),
        ("jar", "--create", "--file", str(helper), "--main-class",
         "dev.slopslap.structural.Main", "-C", str(classes), "."),
        ("go", "build", "-C", str(project), "-o", "slopslap-structural",
         "./cmd/slopslap-structural"),
    )
  if dependency.installation_method is InstallationMethod.MAVEN:
    return (("mvn", "-q", "-f", str(project / "pom.xml"), "package"),)
  if dependency.installation_method is InstallationMethod.NPM:
    return (
        ("npm", "--prefix", str(project), "ci", "--ignore-scripts"),
        ("npm", "--prefix", str(project), "run", "build"),
    )
  if dependency.installation_method is InstallationMethod.CARGO:
    if dependency.language == "rust" and dependency.source.name == "structural":
      return (
          ("cargo", "build", "--locked", "--release", "--manifest-path",
           str(project / "adapters" / "rust" / "Cargo.toml")),
          ("go", "build", "-C", str(project), "-o", "slopslap-structural",
           "./cmd/slopslap-structural"),
      )
    return (("cargo", "build", "--locked", "--release", "--manifest-path",
             str(project / "Cargo.toml")),)
  return (("go", "build", "-C", str(project), "-o", "slopslap-structural",
           "./cmd/slopslap-structural"),)


def _install(dependency: AnalyzerDependency) -> Path:
  missing = [program for program in _required_programs(dependency)
             if shutil.which(program) is None]
  if missing:
    raise AnalyzerDependencyError(
        f"cannot install {dependency.dependency}: missing {', '.join(missing)}"
    )
  if not dependency.source.is_dir():
    raise AnalyzerDependencyError(
        f"installed Slopslap is missing analyzer source: {dependency.source}"
    )
  root = dependency_root()
  root.mkdir(parents=True, exist_ok=True)
  stage = Path(tempfile.mkdtemp(prefix=f"{dependency.language}-", dir=root))
  project = stage / "project"
  try:
    shutil.copytree(
        dependency.source, project,
        ignore=shutil.ignore_patterns("__pycache__", "dist", "node_modules", "target"),
    )
    if dependency.installation_method is InstallationMethod.JDK:
      (project / "adapters" / "java" / "build" / "classes").mkdir(
          parents=True, exist_ok=True,
      )
    for command in _install_commands(dependency, project):
      completed = subprocess.run(command, stdin=subprocess.DEVNULL, check=False)
      if completed.returncode != 0:
        raise AnalyzerDependencyError(
            f"{dependency.installation_method.value} failed while installing "
            f"{dependency.dependency}@{dependency.version}"
        )
    if dependency.language == "rust" and dependency.source.name == "structural":
      suffix = ".exe" if os.name == "nt" else ""
      built = project / "adapters" / "rust" / "target" / "release" \
          / f"slopslap-structural-rust{suffix}"
      shutil.copy2(built, _platform_path(
          project, dependency.ready_path, dependency.execution_method,
      ))
    ready = _platform_path(project, dependency.ready_path,
                           dependency.execution_method)
    entrypoint = _platform_path(project, dependency.entrypoint,
                                dependency.execution_method)
    if not ready.is_file() or not entrypoint.is_file():
      raise AnalyzerDependencyError(
          f"installation did not create {dependency.ready_path} and {dependency.entrypoint}"
      )
    destination = _installed_project(dependency)
    destination.parent.mkdir(parents=True, exist_ok=True)
    if destination.exists():
      shutil.rmtree(destination)
    os.replace(project, destination)
    return destination
  finally:
    shutil.rmtree(stage, ignore_errors=True)


def _command(dependency: AnalyzerDependency, project: Path) -> tuple[str, ...]:
  entrypoint = _platform_path(project, dependency.entrypoint,
                              dependency.execution_method)
  if dependency.execution_method is ExecutionMethod.PYTHON:
    return (sys.executable, str(entrypoint))
  if dependency.execution_method is ExecutionMethod.NODE:
    node = shutil.which("node")
    if node is None:
      raise AnalyzerDependencyError("node is required to run the TypeScript analyzer")
    return (node, str(entrypoint))
  return (str(entrypoint),)


def ensure_analyzer(
    language: str,
    *,
    backend: str | None = None,
    input_stream: Any | None = None,
    prompt: Callable[[str], str] | None = None,
) -> tuple[str, ...]:
  input_stream = sys.stdin if input_stream is None else input_stream
  prompt = input if prompt is None else prompt
  dependency = analyzer_dependency(language, backend)
  project = _ready_project(dependency)
  if project is not None:
    return _command(dependency, project)
  if not input_stream.isatty():
    raise AnalyzerDependencyError(
        f"{dependency.dependency}@{dependency.version} is required for "
        f"{', '.join(dependency.extensions)} files; run Slopslap interactively once to install it"
    )
  answer = prompt(
      f"{language} source detected. Install {dependency.dependency}@{dependency.version} "
      f"using {dependency.installation_method.value}? [Y/n] "
  ).strip().lower()
  if answer not in ("", "y", "yes"):
    raise AnalyzerDependencyError(f"installation declined for {dependency.dependency}")
  return _command(dependency, _install(dependency))


def install_analyzer(language: str, *, backend: str | None = None) -> bool:
  """Install one analyzer, returning False when it was already ready."""
  dependency = analyzer_dependency(language, backend)
  if _ready_project(dependency) is not None:
    return False
  _install(dependency)
  return True


def dependency_statuses() -> tuple[dict[str, Any], ...]:
  return tuple({
      "language": dependency.language,
      "backend": dependency.backend,
      "ready": _ready_project(dependency) is not None,
      "dependency": dependency.dependency,
      "version": dependency.version,
      "installation_method": dependency.installation_method.value,
  } for dependency in analyzer_dependencies().values())
