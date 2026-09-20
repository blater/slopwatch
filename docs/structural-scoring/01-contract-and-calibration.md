# Plan 1: Scoring contract and calibration

Status: complete · Parent: [epic](epic.md) · Dependency: none

## Outcome

A reviewed, cross-language benchmark freezes the structural scoring contract
before formulas change. The shared manifest and source fixture cover unused
work, guard flattening, coherent and cosmetic shared-state extraction, index
versus rescans, dispatch/state interaction, distinct functions, and rename or
move invariance across Go, Java, TypeScript, and Rust.

## Deliverables

- [Frozen calibration contract and source map](calibration.md)
- [Versioned shared fixture and expectation table](../../go/internal/scoring/testdata/structural-v1/manifest.json)
- [Pre-change baseline report](baseline-v1.json)

The contract separates grouped control-flow evidence from full SCORE, preserves
raw metrics and uncertainty, defines custom-weight and disabled-metric behavior,
and records candidate and release performance budgets. Tuning cases are
distinct from held-out transformations and real-code examples. Baseline
failures remain visible; they are evidence for plans 2–4 rather than reasons to
weaken the expectations.

## Acceptance

Later implementation tests must load the manifest as their sole expectation
table and run through `make build`. Grouping arithmetic, file inventory,
numeric SHALLOW publication, raw rename invariance, and unchanged-component
checks are hard invariants. Candidate transformation relations remain
separately reportable until the relevant scorer and continuous curve exist.

Plan 1 changes no production scoring policy.
