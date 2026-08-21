"""Manage the repository's Slopslap GitHub Actions workflow."""

from __future__ import annotations

import re
import sys
from pathlib import Path


DEFAULT_WORKFLOW = Path(".github/workflows/slopslap.yml")
MANAGED_MARKER = "# Managed by slopslap; edit with `slopslap action`."
PASS_SCORE_PATTERN = re.compile(r'^(\s*pass-score:\s*)"[^"]+"\s*$', re.MULTILINE)
ACTION_LINE = "      - uses: blater/slopslap@main"


def workflow_document(pass_score: float | None = None) -> str:
  score = "" if pass_score is None else (
      f'\n        with:\n          pass-score: "{pass_score:g}"'
  )
  return f'''{MANAGED_MARKER}
name: Slopslap

on:
  pull_request:
  push:
    branches: [main]
  workflow_dispatch:

permissions:
  contents: read

jobs:
  analyze:
    name: Analyze design debt
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
{ACTION_LINE}{score}
'''


def managed_pass_score(content: str) -> float | None:
  if MANAGED_MARKER not in content:
    raise ValueError("workflow is not managed by slopslap")
  match = PASS_SCORE_PATTERN.search(content)
  return None if match is None else float(match.group(0).split('"')[1])


def manage_action(command: str, workflow: Path, pass_score: float | None = None) -> int:
  if command == "add":
    if workflow.exists():
      print(f"refusing to overwrite existing workflow: {workflow}", file=sys.stderr)
      return 2
    workflow.parent.mkdir(parents=True, exist_ok=True)
    workflow.write_text(workflow_document(), encoding="utf-8")
    print(f"Added Slopslap action at {workflow}.")
    return 0

  if command == "list":
    if not workflow.is_file():
      print(f"Slopslap action is not installed ({workflow}).")
      return 0
    try:
      score = managed_pass_score(workflow.read_text(encoding="utf-8"))
    except ValueError:
      print(f"{workflow} exists but is not managed by slopslap.", file=sys.stderr)
      return 2
    suffix = "" if score is None else f" (pass score: {score:g})"
    print(f"Slopslap action is installed at {workflow}{suffix}.")
    return 0

  if not workflow.is_file():
    print(f"Slopslap action is not installed ({workflow}).", file=sys.stderr)
    return 2
  content = workflow.read_text(encoding="utf-8")
  try:
    managed_pass_score(content)
  except ValueError:
    print(f"refusing to modify unmanaged workflow: {workflow}", file=sys.stderr)
    return 2

  if command == "remove":
    workflow.unlink()
    print(f"Removed Slopslap action at {workflow}.")
    return 0

  if pass_score is None:
    raise ValueError("set-pass-score requires a score")
  replacement = f'\\g<1>"{pass_score:g}"'
  if PASS_SCORE_PATTERN.search(content):
    updated = PASS_SCORE_PATTERN.sub(replacement, content, count=1)
  else:
    updated = content.replace(
        ACTION_LINE,
        ACTION_LINE + f'\n        with:\n          pass-score: "{pass_score:g}"',
        1,
    )
  workflow.write_text(updated, encoding="utf-8")
  print(f"Set Slopslap pass score to {pass_score:g} in {workflow}.")
  return 0
