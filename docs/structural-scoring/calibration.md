# Structural scoring calibration contract

Status: frozen before candidate evaluation · Parent: [epic](epic.md)

## Outcome

A small, reviewed benchmark defines better scoring before formulas change. The
versioned fixture is
[`structural-score-calibration-v1`](../../go/internal/scoring/testdata/structural-v1/manifest.json).
The manifest is the machine-checkable contract; this document explains why
each relation is present. The fixture is frozen before the first candidate
policy evaluation. Baseline measurements are descriptive and do not alter the
relations.

## Work completed

1. Inventory current scoring and analyzer fixtures. Existing coverage includes
   `go/internal/native/testdata/balanced`, native guard/depth fixtures, and the
   source-estimate transformation/state cases. The shared fixture reuses their
   shapes and adds only the missing cross-language pairs.
2. Record expected ordering and rationale for paired transformations: remove
   unused work, flatten guards, encapsulate a responsibility, extract fragments
   sharing state, and replace an index with repeated scans. Runtime expectations
   are separate. Index versus rescan is deliberately record-only for
   maintainability; an algorithmic speedup must not become a score bonus.
3. Include flat dispatch versus interacting mutations, multiple difficult
   functions, and unchanged code renamed or moved with equivalent context.
   Preserve underlying findings across moves; file totals may redistribute.
4. Separate tuning examples (`unused-work`, `flatten-guards`,
   `coherent-shared-state-extraction`, `index-vs-rescan`) from held-out examples
   (`cosmetic-shared-state-extraction`, `flat-dispatch-vs-interacting-state`,
   `distinct-functions`, `rename-move`). The partition is frozen in the
   manifest; held-out cases must not be used to tune a curve or weights.
5. Freeze expected orderings, tolerances, regression limits, and performance
   budgets before any candidate scoring evaluation. Record baseline raw metrics,
   SCORE, policy, settings, and scope in
   [`baseline-v1.json`](baseline-v1.json). The recorded baseline came from the
   preserved pre-change binary identified in that artifact, rather than the
   concurrent working build; no scoring implementation changes are part of
   plan 1.
6. Define missing evidence, custom weights, and disabled metric behavior. A
   missing finding remains a coverage/limitation diagnostic; it does not become
   a zero contribution. A custom weight is applied after raw evidence is
   preserved, and disabled components contribute zero while remaining visible
   in the raw evidence when available. An unknown file is a failure of the
   applicable benchmark relation, not an omitted file.

## Frozen contract

The score relation uses `epsilon = max(0.25, 0.02 * max(abs(scores)))` for
declared ties. A directional improvement requires at least `0.5` SCORE points
after that tolerance. Raw metric comparisons use `1e-6` tolerance. A file not
covered by a transformation may not regress by more than
`max(0.5, 0.02 * max(abs(baseline), 1))`; a missing file or finding is a
failure unless the case explicitly declares evidence unsupported. These are
comparison limits, not formula parameters.

The primary comparison is the grouped control-flow contribution for the
functions named by a case. Full SCORE remains recorded, but a full-score
relation is admissible only when unchanged components and SHALLOW state are
equivalent. Numeric or estimated SHALLOW state is reported separately; a
parser/context difference cannot masquerade as a structural improvement.

The frozen maintainability relations are:

| Case | Expected relation | Why it is in the contract |
| --- | --- | --- |
| Unused work | after lower than before by at least 0.5 | Dead computation is a distinct maintenance burden and must not be rewarded as useful work. |
| Flatten guards | after lower than before by at least 0.5 | Removing avoidable nesting reduces control-flow burden while preserving behavior. |
| Coherent shared-state extraction | after lower than before by at least 0.5 | A helper that owns a complete repeated state transition is a real responsibility boundary. |
| Cosmetic shared-state extraction | after no lower than before beyond tie tolerance | Moving fragments while leaving the state protocol split is extraction gaming. |
| Index versus rescan | record only | Runtime belongs to the runtime measurement; SCORE must not infer speed from algorithm names or size. |
| Flat dispatch versus interacting state | interacting at least 0.5 above flat | Branches coupled through mutable state are harder to reason about than independent alternatives. |
| Distinct functions | two-function file at least 0.5 above one-function file | Separate difficult routines remain visible; a file maximum must not conceal one of them. |
| Rename/move | tie within epsilon and same raw metric multiset | Names and paths are not evidence; file redistribution is allowed but findings survive. |

Runtime measurements use the same inputs and report indexed lookup at query
counts 1, 8, 64, and 512. The expected result is indexed lookup faster as query
count grows; runtime does not change SCORE. The selected fixture references are
cold p95 `<=5s`, warm no-change p95 `<=2s`, representative edit p95 `<=5s`,
and peak RSS `<=512MiB`; they are pre-evaluation calibration references, not
enterprise claims. Candidate scoring overhead uses three paired runs and the
median: elapsed time may be at most baseline median ×1.10 plus
`max(100ms, 5% of baseline median)`, RSS at most baseline ×1.10 plus 32MiB,
and output at most baseline ×1.10 plus 64KiB. Retain all samples and host,
toolchain, cache, source, and manifest hashes. The P0 directive separately
requires reproducible 30,000-file cold, warm, edit, and idle-watch evidence;
its release thresholds remain governed by the reviewed release policy rather
than by these fixture references.

## Source map

The compact examples are source-reviewed translations of existing cases:

| Property | Existing source evidence | Fixture cases |
| --- | --- | --- |
| unused work / transformations | `go/internal/sourceestimate/transformation_test.go` and `go/internal/sourceestimate/source_surface_inputs_test.go` | `unused-work` |
| guard shape | `go/internal/native/source_depth_guard_test.go` and `go/internal/native/source_depth_polarity_test.go` | `flatten-guards` |
| coherent and cosmetic state extraction | `go/internal/sourceestimate/structural_responsibility_test.go` and `go/internal/sourceestimate/connected_polarity_test.go` | both shared-state cases |
| index versus repeated scans | `docs/evidence/shallow-v4/graded-real/java-codec/StoredTableRowBodyValidator.java` | `index-vs-rescan` |
| dispatch and interacting mutations | `go/internal/sourceestimate/graded_constraint_review_test.go` and `go/internal/sourceestimate/typescript_module_state_test.go` | `flat-dispatch-vs-interacting-state` |
| multiple routines and move invariance | `go/internal/native/depth_file_invariants_test.go` and module shallowness file-attribution rules | `distinct-functions`, `rename-move` |

The fixture keeps the same decision shapes in all four supported languages,
without claiming that a tiny synthetic corpus establishes semantic coverage or
30,000-file performance.

The baseline also records source-reviewed real-code examples from the current
Slopwatch checkout and the Kafka checkout. Kafka's original files are read-only
inputs; their repository revisions and file hashes are frozen in the manifest.
They are held out for ranking/evidence review after focused tuning, so their
scores are not used to choose a formula.

`baseline-v1.json` includes one full-SCORE observation for every case and
language. These explicitly retain old-policy failures (15 of 32 observations)
and record-only cases; they are diagnostic because the old report does not
expose the future grouped-control-flow measure.

The freeze is reproducible from the artifact provenance: manifest SHA-256
`f4d685594f3ff2d931ddfc50d689596428996a07c51c0309dd9fb9132956b8de`, fixture
tree SHA-256
`05fd5c41a6dee99ac0e39e47bff12c74f875d59023e9b8182c32d325a9167f76`, and
old-policy report SHA-256
`a24c94e122df93d3f850575fff39b0dfe9f4e939ed9f386629766e8710893cb3`.

## Acceptance and tests

- Expectations describe code properties, not a desired score for this repository.
- Rank improvement and anti-gaming criteria are explicit and independently
  reviewable. Baseline failures remain visible; do not weaken expectations.
- Reuse existing test helpers and fixtures; keep one shared expectation table
  in the manifest. Later plan tests should load it rather than copy tables into
  language-specific suites.
- Evaluate candidate policies through the existing suite under `make build`.
  Plan 1 itself adds no scoring behavior and runs no ad hoc test/build command.
- Preserve raw-metric conformance tests. Add no new test framework or duplicate
  SHALLOW calibration suite.

## Deliverable

Versioned cases, acceptance criteria, and the compact baseline report are now
present. No scoring change is part of this plan. Baseline values describe the
current `slopslap-balanced-v1` catalog and are not evidence that every relation
passes; failed baseline relations remain visible for plans 2–4.
