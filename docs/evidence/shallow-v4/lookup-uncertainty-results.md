# Actual xmltoaster lookup/formatting regression

The exact xmltoaster QueryResultRow is a separate case from the frozen NQL
QueryResultRow. The former uses nested conditional expressions, LocalDateTime
formatting, lookup-result receivers and `newValue()`. The latter uses switch-based
formatting, decimal handling and `getScalarKind()`. Passing the latter never
established that the former was covered.

The actual source snapshot is preserved unchanged in
[the supplemental fixture](../../../go/internal/sourceestimate/testdata/xmltoaster/QueryResultRow.java).
Its SHA-256 is `7b8327b09f7f8b0b512aab2cdf2d05b18b84c6bf43b33c46079af9c3b4520823`.
The frozen NQL snapshot has SHA-256
`43ab9bf4f338af96f37241c756e59be6a3a7135fe828ae65fd64287dd26ba627` and is unchanged.

Before production changes, the new native report/export test failed on actual
xmltoaster at 80 while the separate NQL test passed at 36. The full 84-file Java
xmltoaster repository also reproduced 80: residual and supported burden 4,
recognized responsibility 0, estimated responsibility 0, and six unresolved-call
limitations. This is the reproduced defect, not an inference from the namesake.

The supplemental 15–45 nonzero acceptance band was set before this correction's
implementation, based on its ordinary lookup/null/formatting responsibilities and
visible multi-operation surface. This is a new engineering regression criterion;
it does not modify or claim independent frozen approval for the existing benchmark.

## Corrected r38 result

Actual xmltoaster now scores **36** both as an isolated exact-source fixture and
inside its repository: residual/supported burden 4, recognized responsibility 0,
estimated responsibility 3. All six unresolved-call limitations remain visible.
The independent NQL namesake still scores **36**. The estimate is one bounded
validation/result envelope per abstraction, not credit for each call. Recognized
validation and transformation-family duties replace overlapping estimated units;
predicate-only returns receive possible validation, not a second transformation.

The full xmltoaster run retains 84/84 numeric Java ratings, 73 estimates, no N/A
and no missing applicable ratings. Nineteen SHALLOW ratings changed; no other
metric's raw value, subjects or contribution changed. Actual QueryResultRow's
SCORE changes from 15 to 9.239984532775. Two ratings increase when previously
duplicated transformation estimates are removed: ConnectionContext 26→33 and
YamlOutputWriter 22→29. These are evidence-category corrections, not guard
regressions. Per-file before/after ledgers are retained in
[the results](lookup-uncertainty-results.json).

All 60 frozen synthetic and 12 frozen real cases pass their unchanged ranges,
relationships and hashes. Real SqlQuery changes 63→60 and TypeScript config 7→4;
StoredTableRowCodec stays 17 and SqlDerivedReferenceValidator stays 7. Passing
means preservation of declared criteria, not that every calibrated rating is
unchanged. The four-language direct-call guard regression remains 10→10, with
native publication and SCORE checks. Lookup tests cover aliases, private helper
returns, overwrite/discard/closure negatives, constant and equivalent ternary
arms, predicate-only flow, logical precedence and Go statement boundaries.

Validation: packaged CLI synthetic/real acceptance; full sourceestimate, native,
report, scoring and fix adapter tests; follow tests; cache policy invalidation
and unchanged warm/cold ledger tests. The pre-existing analysiscache empty-map
roundtrip failure remains separate from this correction. No 30K enterprise
performance claim is made.

This correction is not general model validation. The new independent holdout
fails; see [calibration safeguards results](calibration-safeguards-results.md).
