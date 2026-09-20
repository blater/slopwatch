# Plan 3: Continuous severity curves

Status: in progress · Parent: [epic](epic.md) · Dependency: [plan 2](02-overlap-grouping.md)

## Outcome

Small metric changes cause proportionate severity changes. Crossing a threshold
or extracting a fragment does not create an artificial score improvement.

## Work

1. Replace structural control-flow severity cliffs with continuous,
   nonnegative, monotone curves. Fit a small parameter set using plan 1's tuning
   cases; specify behavior below, at and above each reference threshold.
2. Hold grouping fixed. Compare curve-only changes against the plan 2 baseline
   and evaluate held-out orderings without tuning against held-out results.
3. Check coherent extraction against fragmented shared-state extraction. If
   curves cannot distinguish them, record the limitation for plan 4; do not
   claim extraction resistance from continuity alone.
4. Document the curve, parameters and applicable components in the catalog.
   Preserve raw metric definitions and SHALLOW semantics. Version the policy,
   refresh affected projections and document threshold migration effects.

## Acceptance and tests

- Meet plan 1's ranking and extraction criteria; report remaining failures.
- Extend existing scalar-scoring tests with a small table around thresholds,
  zero and large values. Check monotonicity, finite output and weight handling.
- Reuse the shared transformation cases; replace obsolete cliff expectations.
  Keep exact formula assertions in one layer, not repeated across UI tests.
- `make build` passes. Report score distributions and ranking changes without
  treating lower aggregate scores as evidence of improvement.
