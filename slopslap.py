#!/usr/bin/env python3
"""Slopslap command-line entry point."""

from __future__ import annotations

import sys

from slopslap_app.cli import main


def cli() -> None:
  raise SystemExit(main(sys.argv[1:]))


if __name__ == "__main__":
  cli()
