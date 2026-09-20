# Plan 2: Shared scoring and overlap grouping

Status: implemented; performance comparison pending · Parent: [epic](epic.md) · Dependency: [plan 1](01-contract-and-calibration.md)

## Outcome

Correlated control-flow metrics contribute once per function; separate difficult
functions remain visible in the file score.

## Work

1. Centralize aggregation used by `go/internal/native/scoring_files.go` and
   `go/internal/scoring/projection.go`. Derive projections from preserved raw
   evidence, avoiding repeated scaling of already projected scores.
2. Evaluate `control_flow(f) = max(COG, CYCLO, NPATH, nesting)` using existing
   weighted severities, then sum function contributions. Keep existing severity
   curves here so overlap effects can be assessed separately.
3. Associate nesting evidence with its enclosing function. Define stable
   routine identity, deterministic tie handling and explicit treatment of
   unassociated or missing evidence; never silently discard it.
4. Preserve raw metric displays. Report the grouped contribution and supporting
   signals so displayed contributions reconcile with SCORE. Apply user weights
   and enablement before grouping; keep SHALLOW outside this group.
5. Audit type-level cyclomatic and GOD overlap. Record what is already captured
   and retain only justified additional contributions; do not take a file-wide
   maximum that conceals unrelated problems.
6. Version the policy and affected cache identities. Document ranking and pass
   threshold changes; do not silently migrate user thresholds.

## Acceptance and tests

- The plan 1 benchmark passes its grouping criteria without curve changes.
- Extend `scoring/projection_test.go` and `native/scoring_evidence_test.go` for
  same-function overlap, separate functions, nesting association and settings.
- Reuse existing report/follow/fix tests for one end-to-end consistency case.
  Replace obsolete sum assertions; preserve evidence and immutability checks.
- `make build` passes. Compare cold, warm and incremental performance using
  existing representative workloads; grouping must avoid pairwise scans.
