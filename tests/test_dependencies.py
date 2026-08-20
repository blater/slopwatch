from __future__ import annotations

import unittest
from pathlib import Path
from unittest.mock import Mock, patch

from slopscout_app.dependencies import (
    AnalyzerDependencyError, InstallationMethod, analyzer_dependencies,
    ensure_analyzer, install_analyzer,
)


class _Input:
  def __init__(self, interactive: bool) -> None:
    self.interactive = interactive

  def isatty(self) -> bool:
    return self.interactive


class AnalyzerDependencyTests(unittest.TestCase):
  def test_catalog_declares_extensions_versions_and_installation_methods(self) -> None:
    dependencies = analyzer_dependencies()
    self.assertEqual(set(dependencies), {"java", "typescript", "rust"})
    self.assertEqual(dependencies["java"].extensions, (".java",))
    self.assertEqual(
        dependencies["typescript"].extensions, (".ts", ".tsx", ".mts", ".cts"),
    )
    self.assertEqual(dependencies["rust"].extensions, (".rs",))
    self.assertEqual(dependencies["java"].installation_method,
                     InstallationMethod.MAVEN)
    self.assertEqual(dependencies["typescript"].installation_method,
                     InstallationMethod.NPM)
    self.assertEqual(dependencies["rust"].installation_method,
                     InstallationMethod.CARGO)
    self.assertTrue(all(item.version == "0.1.0" for item in dependencies.values()))

  def test_missing_dependency_does_not_prompt_noninteractive_process(self) -> None:
    prompt = Mock(return_value="yes")
    with patch("slopscout_app.dependencies._ready_project", return_value=None), \
         self.assertRaises(AnalyzerDependencyError):
      ensure_analyzer("typescript", input_stream=_Input(False), prompt=prompt)
    prompt.assert_not_called()

  def test_interactive_first_use_prompts_and_installs(self) -> None:
    project = Path("/installed/typescript")
    prompt = Mock(return_value="yes")
    with patch("slopscout_app.dependencies._ready_project", return_value=None), \
         patch("slopscout_app.dependencies._install", return_value=project) as install, \
         patch("slopscout_app.dependencies._command", return_value=("node", "cli.js")):
      command = ensure_analyzer("typescript", input_stream=_Input(True), prompt=prompt)
    self.assertEqual(command, ("node", "cli.js"))
    prompt.assert_called_once()
    install.assert_called_once()

  def test_explicit_install_is_idempotent(self) -> None:
    with patch("slopscout_app.dependencies._ready_project", return_value=Path("/ready")), \
         patch("slopscout_app.dependencies._install") as install:
      self.assertFalse(install_analyzer("rust"))
    install.assert_not_called()

  def test_explicit_install_installs_when_missing(self) -> None:
    with patch("slopscout_app.dependencies._ready_project", return_value=None), \
         patch("slopscout_app.dependencies._install") as install:
      self.assertTrue(install_analyzer("java"))
    install.assert_called_once()


if __name__ == "__main__":
  unittest.main()
