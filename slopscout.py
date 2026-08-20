#!/usr/bin/env python3
"""Slopscout command-line entry point."""

from __future__ import annotations

import sys

from slopscout_app.cli import main


def cli() -> None:
  raise SystemExit(main(sys.argv[1:]))


if __name__ == "__main__":
  cli()
