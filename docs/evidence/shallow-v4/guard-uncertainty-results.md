# Guard monotonicity correction — r37

The r36 packaged CLI reproduced the reported regression. With identical caller
inputs, adding a useful guard to an unresolved returned conversion raised Java,
TypeScript and Go SHALLOW from 10 to 20. The analogous Rust early-return guard
before a tail expression raised 10 to 43.

The corrected CLI reports **10 for both versions in all four languages**.
The [recorded results](guard-uncertainty-results.json) retain ratings, SCORE and
observed/estimated responsibility separately.

## Model correction

Strict protocol transparency is unchanged. Unresolved returned-duty eligibility
has its own bounded result-flow check. Adding recognized validation now replaces
the estimated validation unit without discarding the two-unit unknown result
transformation allowance: Java, TypeScript and Go move from H=0/U=3 to H=1/U=2.
Rust's early-return/tail validation remains unrecognized in this example, so its
H=0/U=3 stays explicitly estimated. This fix does not claim new semantic proof.

Discarded results and overwritten aliases cannot earn a returned-result allowance.
This remains bounded syntactic analysis, not general control-flow or alias proof.
Policy r37 invalidates r36 workspace views, analysis units and display projections.

## Existing acceptance preserved

The rebuilt packaged CLI passes **60/60 frozen synthetic cases and 12/12 frozen
real-source cases**, with zero failures and **no rating changes** relative to the
previous r36 acceptance artifacts. All ranges, source snapshots, hashes, pairs and
invariance expectations are unchanged. StoredTableRowCodec remains 17 and
SqlDerivedReferenceValidator remains 7, with the same responsibility explanations
in [the prior delivery report](graded-delivery-results.md).

Each synthetic language has 15/15 numeric ratings. All 12 real-source cases have
numeric ratings; the declaration-only Go control is still excluded from semantic
coverage, leaving 11/11 applicable semantic cases. Numeric estimates do not become
complete semantic conformance or automated-fix authorization.

Permanent tests cover cross-language guard and returned-local pairs, disconnected
and overwritten results, report/export publication and incomplete-evidence status.
Existing numeric display/sorting, SCORE, source cache/edit and fix-safety tests
remain part of the verification run. Cache tests explicitly reject r36 keys and
projections and accept the current policy.

## Verification and limits

Passing commands (with `GOCACHE` set to the workspace's `build/go-cache`):

```sh
make build
go test -C go ./internal/sourceestimate ./internal/native ./internal/report ./internal/follow ./internal/scoring ./internal/fixanalysis/nativeadapter
go test -C analyzers/structural ./internal/metrics ./internal/depth
python3 tools/shallow_graded_acceptance.py --binary build/slopmark --benchmark docs/evidence/shallow-v4/graded-benchmark.json --output build/shallow-guard-graded-after.json
python3 tools/shallow_graded_acceptance.py --binary build/slopmark --benchmark docs/evidence/shallow-v4/graded-real-benchmark.json --output build/shallow-guard-graded-real-after.json
```

The broader `internal/analysiscache` suite has a **pre-existing failure** in
`TestProjectionArtifactRoundTrip`: an empty `Depth` map round-trips as `nil`.
It failed before this correction and reproduces afterward. It is not hidden by
changing its expectation; the targeted policy/cache acceptance tests pass.

This correction adds no whole-project pass. The enterprise P0 remains open:
30K-file cold, warm, incremental and idle performance must still satisfy the
original delivery directive. No enterprise capacity claim, new full-corpus
coverage claim, commit, push or release is made by this correction.

## Review disposition

Three independent review passes examined result-flow edge cases and verification
coverage. Accepted findings require preserving supported `await`/cast wrappers,
parenthesized local returns and expression arrows, recognizing the adjacent local
declaration in semicolonless TypeScript, excluding field/conditional assignments,
and covering all four languages through the native exported report.

The suggested guard-only extra parameter changes the caller interface, outside
this fixed-input regression; existing full-input forwarding eligibility is retained.
General Rust early-return validation recognition remains a documented existing
limit. Current cache tests verify old/current keys and projections plus existing
cold/warm/edit behavior; a second synthetic old-report integration harness adds
no necessary guarantee here. Enterprise capacity remains an open P0 and is not
reclassified as passed by these bounded regression tests.
