# Epic: Structural scores that reflect distinct maintenance problems

Status: in progress

## Outcome

SCORE rewards meaningful simplification without repeatedly charging for the
same control flow or rewarding cosmetic extraction. Raw metrics remain visible.

## Delivery order

1. [Scoring contract and calibration](01-contract-and-calibration.md): define
   expected behavior and the evidence used to judge changes.
2. [Shared scoring and overlap grouping](02-overlap-grouping.md): combine
   correlated signals per function through one scoring path.
3. [Continuous severity curves](03-continuous-severity.md): remove threshold
   cliffs and measure resistance to cosmetic extraction.
4. [Branch interaction](04-branch-interaction.md): add bounded structural
   evidence only if it improves held-out rankings.

Plans 2 and 3 can ship before plan 4. Plan 1 supplies their acceptance contract.
Plan 4 may conclude with an evidence-backed decision to retain the prior model.

## Constraints

- Preserve numeric SHALLOW, uncertainty metadata and automated-fix safety gates
  under the [P0 directive](../shallow-p0-delivery-directive.md).
- Do not exempt parsers or infer quality from names, file size or presumed
  necessity. Measure performance separately; do not award speculative bonuses.
- Version changed scoring semantics, invalidate affected cached projections,
  and explain changes to rankings and configured pass thresholds.
- Keep analysis, dashboard reweighting, exports and fix verification consistent.
- Preserve enterprise performance requirements; small fixtures cannot establish
  30,000-file readiness.

## Lean validation

Extend existing scoring, analyzer and report tests. Consolidate duplicate cases;
lightly refactor helpers only where needed. Replace obsolete additive-score or
threshold assertions with the new contract; retain legacy assertions only for
supported legacy behavior. Do not regenerate expected numbers without review.

Use a compact shared case set and existing performance harnesses. Add coverage
only for a distinct failure mode. Run implementation validation through
`make build`, including packaged smoke tests; add no release-only suite or flags.
Preserve caches; use `make test-clean` only for an explicitly needed clean run.

Exercise rankings and performance on `~/src/slopwatch` and `~/src/kafka`.
Record revisions and settings; use fixed snapshots for before/after comparisons
and temporary copies for edit measurements. Keep 30,000-file capacity evidence
separate when these repositories do not provide that source count.

## Completion

Plans 1–3 meet their acceptance criteria; plan 4 records its measured decision.
Publish baseline/candidate ranking changes, remaining limitations and performance
results. A lower score for one motivating file is not acceptance evidence.
