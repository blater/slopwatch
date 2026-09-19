# Calibration safeguards and first holdout evaluation

The safeguards now expose model failures rather than allowing calibration fit to
stand in for independent positive detection. The production numeric weights are
unchanged and centralized in
[the named profile](../../../go/internal/sourceestimate/calibration_default.json).
All 21 parameters are validated and run through the actual grading pipeline;
request-local profiles cannot mutate defaults. Profile identity participates in
unit, report and display-cache identity. Persisted incompatible projections are
rejected. Exact QueryResultRow variants and prior guard acceptance remain tested.

## Parameter sensitivity

The unchanged 60 synthetic and 12 real calibration cases run under 42 individual
±20% perturbations and 24 paired perturbations (six pairs, four corners each).
Default equivalence, ranges, relationships, positive uncertainty preservation,
parameter coverage and four-language guard monotonicity are asserted. Sparse
fields require explicit, component-specific mechanism probes. Perturbed range
and relationship failures are reported as observations, not silently equated
with default acceptance failures.

Across 66 models × 72 cases, results include 50 case/model range escapes, 291
strict rank reversals, maximum absolute score change 31, zero directional
violations and zero guard violations. One uncertainty invariant fails under
operation weight 0.8: Rust lifecycle baseline 1 becomes dependency-hidden 0.
This is evidence of calibration fragility, not a robustness pass. The
[full reproducible artifact](calibration-sensitivity-results.json) includes every
parameter setting and case grade; [compact results](calibration-safeguards-results.json)
record totals and identities. Limited local perturbations do not establish
parameter identifiability or global robustness.

## Independent source-selected holdout

A separate reviewer selected four real Java boundaries and expected bands from
source and caller witnesses without scorer output. Sources, witnesses, labels
and relationships were frozen and hashed before the first evaluation. None is
in the calibration manifests. Caller witnesses support source review but are
not analyzer inputs. The first evaluation is retained with an exclusive-write
record and checksum; the runner rejects output writes into the frozen directory.
Tests exercise failed first-run persistence and overwrite rejection.

| Frozen case | Expected | First rating | Result |
| --- | --- | --- | --- |
| River SqlBoundAccess | 65–100 | 0 | Fail |
| River SqlDescriptorScanContext | 65–100 | 22 | Fail |
| NQL HierarchyRowContext | 0–25 | 64 | Fail |
| xmltoaster SqlRowCursor | 26–64 | 80 | Fail |

**High-band positive detection: 0/2. Band acceptance: 0/4.** Two high-versus-low
ordering requirements also fail. All four publish numeric estimates with
incomplete-evidence metadata, which establishes publication coverage only.
The original real calibration benchmark still has no high-band cases; it was
not rewritten. The separate holdout now measures that missing dimension and
shows that this model does not satisfy it.

[Source-selection rationale](calibration-holdout/selection.md),
[frozen manifest](calibration-holdout/manifest.json) and
[unaltered first evaluation](calibration-holdout/first-evaluation.json) are retained.
No production weights, labels or selection changed after these results. This is
a purposive four-case Java sample from three local repositories, with incomplete
workspace dependencies, not broad independent empirical certification or
four-language holdout coverage. Any later tuning against these cases consumes
the holdout and requires a fresh blind sample for another generalization claim.

## Required checks and remaining work

`make test-shallow-sensitivity`, `make test-shallow-safeguards`, and
`make test-shallow-regressions` pass locally. The packaged 60+12 original cases
pass. `python3 tools/shallow_holdout_acceptance.py --record-first` exits 1 with
the retained failures above; future `make test-shallow-holdout` runs use the
same frozen cases without replacing that first record. The combined
`make test-shallow-calibration` attempts every check and fails if any fails.
The added PR workflow runs those checks and uploads evaluation artifacts even
on failure. It has not been run remotely.

The requested safeguards are implemented; the model's real-source detection is
not validated, and the holdout gate is intentionally red. The prior unrelated
analysiscache empty-map roundtrip failure remains known. Enterprise 30K cold,
warm and incremental performance acceptance remains open.
