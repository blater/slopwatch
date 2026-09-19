# Graded SHALLOW delivery — r36

Date: 2026-09-18. The rebuilt packaged CLI passes **60/60 synthetic cases and
12/12 real-source cases**, with zero benchmark failures. The independent reviewer
approved the bounded graded-scoring gate after adversarial fixes; this is not an
enterprise-performance or whole-program semantic-completeness certification.

The frozen expectations and source hashes were not changed to fit outputs.
[Independent review](graded-independent-review.md),
[synthetic benchmark](graded-benchmark.json), and
[real-source benchmark](graded-real-benchmark.json) contain the engineering
judgments, ranges, positive detections and tolerances established before scoring
changes. The real set deliberately emphasizes earlier false positives; high-band
positive calibration comes from the matched synthetic mechanisms, not repository
distribution quotas.

## Complete evidence

- [Synthetic before](shallow-graded-before.json) and [after](shallow-graded-after.json): every case, pair, invariant, limitation, contribution and failure.
- [Real-source before](shallow-graded-real-before.json) and [after](shallow-graded-real-after.json): every frozen real case.
- [Repository results](graded-repository-results.json): every file's before/after SHALLOW and SCORE, uncertainty, zero reason, missing-rating explanation, distribution, timings and changed non-SHALLOW components.

Before is the preserved r35 executable. After is the newly built r36 executable;
its SHA-256 is recorded in the repository results. Both benchmark runs use the
same runner and frozen sources. The old synthetic report has 110 recorded failed
assertions and the old real report 15, including missing zero explanations;
these are assertion counts, not unique failing-file counts. Both after reports
have zero failed assertions.

## Per-language benchmark distributions

Counts below use bands 0 / 1–25 / 26–64 / 65–100. Each synthetic language has 15
cases, including invariance and missing-dependency twins.

| Language | Before bands | After bands | Numeric | Material uncertainty | Cases passing |
|---|---|---|---|---|---|
| Java | 15 / 0 / 0 / 0 | 2 / 6 / 3 / 4 | 15/15 | 14/15 | 15/15 |
| Go | 15 / 0 / 0 / 0 | 2 / 6 / 3 / 4 | 15/15 | 14/15 | 15/15 |
| TypeScript | 13 / 2 / 0 / 0 | 7 / 1 / 3 / 4 | 15/15 | 14/15 | 15/15 |
| Rust | 15 / 0 / 0 / 0 | 2 / 6 / 3 / 4 | 15/15 | 14/15 | 15/15 |

The all-zero implementation fails the intermediate/high and directional checks.
Uncertainty is reported independently of range conformance. Complete precise
semantic evidence remains uncommon: the runner counts one complete synthetic
case each in Java and TypeScript, and none in Go/Rust. Passing these behavioral
expectations does not turn heuristic analysis into complete semantic proof.

The real set has numeric results for all 12 cases and 11/11 semantic-applicable
cases. The Go declaration-only control remains visible in total coverage but is
excluded from semantic coverage: Go is one semantic case plus one control.
Material uncertainty affects 5/6 Java, 1/2 Go, 2/2 TypeScript and 2/2 Rust cases.

Examples across bands, from Java's matched source families:

| Caller responsibility transferred inside | Low | Intermediate | High |
|---|---:|---:|---:|
| Lifecycle management | 2 | 32 | 89 |
| Coupled representation invariants | 17 | 39 | 68 |
| Pipeline coordination versus direct forwarding | 0 | 55 | 93 |

All predeclared directionality gaps and helper/rename/syntax tolerances pass in
all four languages. Missing-dependency twins retain their graded values rather
than becoming zero or increasing. Additional independently discovered regressions
now cover combined/split guards (89/89), private validation-helper loss (14→7),
parenthesized unresolved-call arguments (7/7), returned-local bindings, and Go
defer scope. A discarded unresolved call no longer erases independently recognized
returned arithmetic.

## Real-source explanations

| File | Before | After | Why |
|---|---:|---:|---|
| DirectoryOperationResult | 0 | 0 | Recognized supporting data role. |
| StoredTableRowCodec | 0 | 17 | Residual caller burden 3; seven recognized validation duties across reachable codec implementations. |
| SqlDerivedReferenceValidator | 0 | 7 | Residual burden 0.5; three recognized validation duties behind its entry point. |
| SqlQuery | 0 | 63 | Broad graph/staging obligations remain; validation, coordination and related state updates hide meaningful work. |
| QueryResultRow | 0 | 36 | Mutable representation/lifetime obligations coexist with conversion and validation. |
| KeyedPath | 0 | 0 | Supported lowest-range value assessment; constructor behavior remains analyzed. |
| workspace.go | 0 | 0 | Construction/configuration surface falls in the lowest range; supporting error accessors do not replace its validation assessment. |
| TypeScript typed-context | 0 | 7 | Configuration work behind a compact entry point. |
| Rust location / expression | 0 / 0 | 20 / 5 | Small callable surfaces with distinct remaining burden and recognized transformation. |

The codec's 17 is `round(100×3/(1+3+2×7))`; the validator's 7 is
`round(100×0.5/(1+0.5+2×3))`. Neither has a filename exception or a score cap.
These counts are recognized source duties, not a claim to have resolved every
byte operation or delegate. Their remaining limits stay visible. See the
[rubric and uncertainty assumptions](../../components/module_shallowness.md).

## Repository runs

All runs used the final rebuilt executable without `-use-cache`. Reported times
are observed elapsed time on the recorded development machine, with other
validation work concurrent; they are not controlled performance benchmarks.

| Corpus/language | Numeric / files | N/A | 0 | 1–25 | 26–64 | 65–100 | Material uncertainty |
|---|---:|---:|---:|---:|---:|---:|---:|
| River / Java | 2,153 / 2,207 | 54 | 221 | 629 | 942 | 361 | 2,023 |
| NQL / Java | 276 / 280 | 4 | 99 | 91 | 62 | 24 | 255 |
| SlopWatch selected sources / Go | 358 / 358 | 0 | 94 | 194 | 60 | 10 | 324 |
| SlopWatch selected sources / Java | 3 / 3 | 0 | 1 | 0 | 2 | 0 | 2 |
| SlopWatch selected sources / TypeScript | 16 / 16 | 0 | 6 | 6 | 4 | 0 | 14 |
| SlopWatch selected sources / Rust | 10 / 10 | 0 | 2 | 7 | 1 | 0 | 9 |

River changes from 2,127 zero ratings and 15 positive ratings to 221 zeros and
1,932 positive ratings. NQL changes from 271 zeros to 99 zeros and 177 positives.
This is graded delivery, not a lower-false-positive-only acceptance claim.

Every remaining N/A is enumerated with a source-based explanation in the JSON:
interface contracts without executable implementations, or package documentation.
An outer interface no longer hides a nested executable record or package-visible
implementation. Uninventoried executable Java initializers receive an explicit
conservative numeric estimate instead of N/A.

Every zero has a reason. River has 53 recognized-role, 109 supported-lowest-range
and 59 conservative-uncertainty zeros; NQL has 81 supported-lowest-range and 18
conservative-uncertainty zeros. Conservative zeros are **not successful semantic
coverage**. Their remaining attribution gaps are visible in the per-file artifact.
The high uncertainty counts above are not presented as complete primary semantic
coverage; ordinary source estimation remains the common mode.

River and NQL have **zero changed non-SHALLOW component values/contributions**
against r35. SCORE changes through SHALLOW's existing contribution curve. The
SlopWatch source itself changed during implementation, and its selected language
scope expanded; its seven other-component differences are listed rather than
misrepresented as a controlled same-source comparison.

Observed elapsed times: River 23.87s, NQL 3.12s, selected SlopWatch sources 2.45s.
River's exported JSON is still 101,570,141 bytes. Neither output size nor the
enterprise-performance P0 is closed by these results. No 30K capacity, sustained
idle, or incremental-latency acceptance claim is made here.

## Build, tests and reproduction

`make build` completed. The following packages passed after the production fixes:

```sh
GOCACHE=$PWD/build/go-cache go test -C go ./internal/sourceestimate ./internal/native ./internal/report ./internal/follow ./internal/scoring ./internal/fixanalysis/nativeadapter
GOCACHE=$PWD/build/go-cache go test -C analyzers/structural ./internal/metrics ./internal/depth
python3 tools/shallow_graded_acceptance.py --binary build/slopmark --benchmark docs/evidence/shallow-v4/graded-benchmark.json --output build/shallow-graded-after.json
python3 tools/shallow_graded_acceptance.py --binary build/slopmark --benchmark docs/evidence/shallow-v4/graded-real-benchmark.json --output build/shallow-graded-real-after.json
```

The reporting checks exercise numeric display/sorting, export, SCORE contribution,
cold/warm cache equality, edit invalidation and independent automated-fix safety.
Policy r36 invalidates incompatible r35 projections. The benchmark runner exposes
all failures and both semantic and total coverage; its positive groups reject an
all-zero implementation. No tag, merge, push or release was performed.
