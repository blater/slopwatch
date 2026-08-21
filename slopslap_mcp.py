#!/usr/bin/env python3
"""MCP interface for targeted slopslap analysis."""

from __future__ import annotations

import sys
from pathlib import Path
from typing import Any

from slopslap_app.config import discover_config, load_policy
from slopslap_app.orchestrator import analyze as analyze_languages
from slopslap_app.report import report_document
from slopslap_app.schema import configuration_document


SERVER_INSTRUCTIONS = (
    "Use rank_files to find high-scoring Java, TypeScript, or Rust files worth simplifying. "
    "Use score_files with every supported source file changed by feature work. "
    "Use get_config to inspect configuration, analyzers, and scoring components. "
    "Use score_files with a cleanup target before and after refactoring. Lower scores are "
    "better; work toward scores below 100. The tools are read-only and "
    "stateless, so they neither modify code nor maintain a baseline."
)


def _workspace_for(paths: list[Path]) -> Path:
  if not paths:
    return Path.cwd().resolve()
  resolved = [path.resolve() for path in paths]
  common = Path(__import__("os").path.commonpath(
      [str(path if path.is_dir() else path.parent) for path in resolved]
  ))
  for candidate in (common, *common.parents):
    if (candidate / ".git").exists():
      return candidate
  return common


def rank_files(
    directories: list[str] | None = None,
    languages: list[str] | None = None,
    limit: int = 10,
) -> dict[str, Any]:
  """Rank supported source files with the standard report format."""
  if limit < 0:
    raise ValueError("limit must be non-negative")
  targets = [Path(item) for item in directories or []]
  workspace = _workspace_for(targets)
  config = discover_config(
      Path.cwd(), warn=lambda message: print(f"warning: {message}", file=sys.stderr),
  )
  selected = None if not languages or languages == ["auto"] else languages
  outcome = analyze_languages(workspace, targets=targets, policy=load_policy(config), languages=selected)
  document = report_document(outcome.scores, calibrated=outcome.profiles.calibrated,
                             diagnostics=outcome.diagnostics,
                             execution_plans=outcome.execution_plans,
                             configuration=None if config is None else str(config),
                             limit=limit)
  return document


def score_files(files: list[str]) -> dict[str, Any]:
  """Score exactly the supplied supported source files."""
  if not files:
    raise ValueError("files must contain at least one source path")
  paths = [Path(item) for item in files]
  return rank_files([str(item) for item in paths], limit=0)


def get_config() -> dict[str, Any]:
  """Return configuration locations, analyzers, and scoring components."""
  return configuration_document(
      Path.cwd(), warn=lambda message: print(f"warning: {message}", file=sys.stderr),
  )


def create_server() -> Any:
  """Create the MCP server."""
  try:
    from mcp.server.mcpserver import MCPServer
    from mcp_types import ToolAnnotations
  except ModuleNotFoundError as error:
    raise RuntimeError(
        "the Slopslap installation is incomplete: its MCP dependency is missing"
    ) from error

  server = MCPServer(
      name="slopslap",
      title="slopslap multi-language design-debt scorer",
      description=(
          "Read-only tools for analysis and configuration introspection."
      ),
      instructions=SERVER_INSTRUCTIONS,
      version="0.1.0",
  )
  annotations = ToolAnnotations(
      read_only_hint=True,
      destructive_hint=False,
      idempotent_hint=True,
      open_world_hint=False,
  )
  server.tool(
      name="rank_files",
      title="Rank source files",
      description=(
          "Rank Java, TypeScript, and safe structural Rust sources. Omit "
          "directories and use languages=['auto'] to inspect the workspace."
      ),
      annotations=annotations,
      structured_output=True,
  )(rank_files)
  server.tool(
      name="score_files",
      title="Score exact source files",
      description="Score exact Java, TypeScript, or Rust files.",
      annotations=annotations,
      structured_output=True,
  )(score_files)
  server.tool(
      name="get_config",
      title="Get Slopslap configuration",
      description=(
          "Return configuration precedence, analyzer installation status, "
          "dependencies, and scoring components."
      ),
      annotations=annotations,
      structured_output=True,
  )(get_config)
  return server


def main() -> None:
  try:
    server = create_server()
  except RuntimeError as error:
    raise SystemExit(f"error: {error}") from error
  server.run("stdio")


if __name__ == "__main__":
  main()
