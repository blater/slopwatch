#!/usr/bin/env python3
"""MCP interface for targeted smellscout analysis."""

from __future__ import annotations

from pathlib import Path
from typing import Any

from smellscout import AnalysisResult, FileMetrics, analyze_java
from smellscout import build_report_document, project_relative_path


SERVER_INSTRUCTIONS = (
    "Use rank_java_files to find high-scoring Java files worth simplifying. "
    "Use score_java_files with every Java file changed by feature work, or "
    "with a cleanup target before and after refactoring. Lower scores are "
    "better; work toward scores below 100. The tools are read-only and "
    "stateless, so they neither modify code nor maintain a baseline."
)


def rank_java_files(
    directories: list[str] | None = None,
    limit: int = 10,
) -> dict[str, Any]:
  """Rank Java files recursively under one or more directories.

  Directories may identify repositories, modules, source roots, or packages.
  Project and module inputs use production-source and Java-version inference.
  An omitted or empty list analyzes the current working directory.
  """
  if limit < 0:
    raise ValueError("limit must be non-negative")
  analysis = analyze_java(
      directories=(Path(directory) for directory in directories or [])
  )
  return build_report_document(analysis, limit=limit, max_score=None)


def _requested_metrics(analysis: AnalysisResult) -> list[FileMetrics]:
  by_path = {item.path: item for item in analysis.metrics}
  return [
      by_path[project_relative_path(analysis.workspace, source_file)]
      for source_file in analysis.requested_files
  ]


def score_java_files(files: list[str]) -> dict[str, Any]:
  """Score exactly the requested Java source files and return all results.

  Use this after feature work with the changed Java files, or before and after
  simplifying a known target. Module and Java-version inference still applies.
  """
  if not files:
    raise ValueError("files must contain at least one Java source path")
  analysis = analyze_java(files=(Path(source_file) for source_file in files))
  return build_report_document(
      analysis,
      limit=0,
      max_score=None,
      ordered_metrics=_requested_metrics(analysis),
  )


def create_server() -> Any:
  """Create the optional MCP server without making MCP a CLI dependency."""
  try:
    from mcp.server.mcpserver import MCPServer
    from mcp_types import ToolAnnotations
  except ModuleNotFoundError as error:
    raise RuntimeError(
        "MCP support is not installed; install smellscout with its mcp extra"
    ) from error

  server = MCPServer(
      name="smellscout",
      title="smellscout Java design-debt scorer",
      description=(
          "Read-only tools for ranking Java design debt and scoring exact "
          "changed files."
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
      name="rank_java_files",
      title="Rank Java files",
      description=(
          "Rank Java files recursively under repository, module, source, or "
          "package directories. Omit directories to use the current working "
          "directory. Use this to choose cleanup work."
      ),
      annotations=annotations,
      structured_output=True,
  )(rank_java_files)
  server.tool(
      name="score_java_files",
      title="Score exact Java files",
      description=(
          "Score exactly the supplied Java files and return every result in "
          "request order. Use this for changed files or a known cleanup target."
      ),
      annotations=annotations,
      structured_output=True,
  )(score_java_files)
  return server


def main() -> None:
  try:
    server = create_server()
  except RuntimeError as error:
    raise SystemExit(f"error: {error}") from error
  server.run("stdio")


if __name__ == "__main__":
  main()
