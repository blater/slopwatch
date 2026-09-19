# Graded calibration safeguards

`calibration_default.json` is the named embedded production profile. Its 21
positive finite numeric fields cover caller-surface units, recognized duties,
bounded uncertainty envelopes and denominator calibration. Typed validation
rejects missing/nonpositive, nonfinite and excessively large values. The
100-point scale, rounding, recognizers and traversal bounds are model structure.

`DefaultCalibration()` returns a value copy. `AnalyzeWithCalibration(files,
profile)` validates a request-local value and runs actual source grading before
choosing abstraction maxima. Observed duties replace overlapping estimated
categories. Production defaults, descriptive evidence and fix gates are not
overridden. The profile digest enters report, unit-cache and display identities.
The CLI/watch Makefile targets depend on the embedded JSON.

Run from `go/`:

```sh
SHALLOW_SENSITIVITY_OUTPUT=/tmp/shallow-sensitivity.json \
  go test ./internal/sourceestimate -run TestCalibrationSensitivity -count=1 -v
go test -race ./internal/sourceestimate -run TestCalibrationValidationAndIsolation -count=1
go test ./internal/sourceestimate ./internal/native
```

The suite reads both frozen calibration manifests and verifies real snapshot
SHA-256 hashes. Each numeric parameter is exercised at 0.8 and 1.2 times default
with all others fixed, plus six declared interacting pairs at all four 0.8/1.2
corners (66 models total). JSON records parameter/value/profile identity, per-case
default and perturbed grades, deltas, range escapes, selected abstractions,
selection changes, maximum absolute delta, evidence/rating coverage, strict rank
reversals and frozen direction/invariance violations. Zero changed-case coverage
requires an explicit tested mechanism-probe exemption; unexplained missing
coverage fails. Connected-result guard monotonicity and category replacement are
checked in all four languages. Default equivalence/band/relationship failures, invalid profile
acceptance, selection errors, concurrency contamination and guard violations fail
tests. Perturbed ranges, directions and invariances are sensitivity observations,
not requirements that deliberately changed models satisfy default bands.

These local single-parameter and paired perturbations do not establish global robustness or
parameter identifiability. Recognition paths may be absent or masked; zero
changed evidence/rating is a coverage limitation. Calibration fit is separate
from holdout detection. Holdout source review, freeze and first evaluation are
owned separately; this suite never reads it. A small source-reviewed holdout
does not establish broad empirical generalization. No enterprise performance
claim follows from these tests.

`make test-shallow-sensitivity` writes `build/shallow-sensitivity.json`.
`make test-shallow-holdout` builds and evaluates the separate frozen holdout.
`make test-shallow-safeguards` checks the harness and cache identity.
`make test-shallow-regressions` checks source, native publication and scoring/fix boundaries.
`make test-shallow-calibration` runs regression, sensitivity, safeguard, original
holdout, frozen-context and fresh-holdout checks and returns failure if any fails,
while still attempting each report. Sparse synthetic mechanism probes exercise
mutable-alias, state and transform weights; these are wiring checks, not additions
to either frozen calibration corpus or holdout.

## Required review before another weight change

Do not adjust numeric weights on calibration-fit or aggregate sensitivity alone.
Before applying an adjustment, prepare a change report and obtain review covering:

- Per-case score/evidence deltas from the current profile, including numeric
  publication, applicable versus N/A counts and the previous acceptance suite.
- Important ranking changes, with responsibility/caller-burden rationale;
  distinguish material inversions from exchanges between near-equal cases.
- Mechanism coverage for every affected weight, explicitly identifying corpus
  gaps and the narrower coverage supplied by isolated probes.
- Fresh independently source-reviewed examples selected and frozen before
  scoring, never reused from a set consumed by tuning. Include supported-language
  coverage and isolated versus caller/workspace context where relevant.

`tools/shallow_calibration_change.py` compares against the retained pre-change
profile and rejects numeric changes without `calibration-change-review.json`
under `docs/evidence/shallow-v4/`. That report must carry `status: reviewed`,
a nonempty reviewer, canonical baseline/proposed profile hashes, and an assessment
plus repository-relative artifact path and SHA-256 for each `score_deltas`,
`important_ranking_changes`, `mechanism_coverage`, and `fresh_independent_examples`
section. Fresh examples also require independent reviewer, pre-evaluation
selection and no-tuning attestations. The check is part of the Make/CI safeguards.
These fields attest review; they do not authenticate a reviewer or replace
repository review of evidence quality. Do not edit the retained baseline to
bypass review. No weights changed in the structural-responsibility correction.

The existing real holdout has already been observed and remains a regression and
diagnostic set, not fresh validation. `tools/shallow_holdout_context.py` reports
isolated targets, frozen caller-witness context and full live workspace results
separately with the same labels/relationships. `--frozen-only` runs reproducibly
in CI without external workspaces. Adding context does not convert source-review
assumptions into proof. Preserve the original first-evaluation artifact.


The fresh structural holdout adds two source-selected cases each in Go,
TypeScript and Rust. It contains low/mixed expectations and **no high-band
cases**. `make test-shallow-fresh` therefore explicitly declares absent high-band
coverage; it does not relax the original Java high-detection gate or claim new
high detection. Both manifests, source hashes, relations and exclusive first
records remain separate. Selection rationale discloses any source-review limits.


The connected-obligations correction adds a separately frozen real high/low pair
in each of Go, TypeScript and Rust. `make test-shallow-high` requires predeclared
high-band examples in all three languages and checks their original bands and
rankings; it reports detection per language. Selection is independent of the
implementer and candidate scores, with earlier scorer-source exposure explicitly
disclosed. This small purposive sample cannot establish population-wide accuracy.
The first evaluation is recorded once, including any failures, after the corrected
model is frozen. Neither a missing language nor an observed failed example may be
silently replaced by an aggregate pass. The original Java and low/mixed holdouts
remain separate, unchanged checks in the combined calibration target.


`make test-shallow-confirmation` evaluates a separate post-correction source-only
confirmation sample. Its Go/Rust high cases remain independently selected before
scoring; the reviewed TypeScript case is mixed, so missing fresh TypeScript-high
coverage stays explicit. This check supplements, rather than replaces, the failed
TypeScript-high gate above. Preserve all first results, including failed low cases;
do not adjust recognizers or weights against these scores and call the same sample
fresh validation. Compiler-internal syntax limits and numeric fallback are reported
separately from a band hit.
