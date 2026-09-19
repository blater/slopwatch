# SHALLOW P0 delivery directive

Status: controlling product requirement, 2026-09-17. This records the user's
latest instructions and supersedes conflicting release amendments, including
the r20 policy that hides estimates and earlier enterprise-benchmark deferrals.

## P0 first: numeric product coverage

The product promises a rating. A report with almost all SHALLOW values shown as
`X` fails that promise. Every applicable source file in Java, TypeScript, Go and
Rust must receive a finite, deterministic SHALLOW rating in the main report,
normal numeric sorting, exported reports and the SHALLOW contribution to SCORE.
Use the strongest available analysis, then a bounded fallback when precise
analysis is incomplete. Missing types, dependencies, project configuration or
unsupported semantic constructs must not make the rating disappear. Preserve
precise diagnostics, estimation status and evidence completeness as separate
metadata. A proven absence of an applicable abstraction can remain N/A; do not
expand exclusions to inflate numeric coverage.

Useful ratings remain essential. The metric concerns caller burden relative to
responsibility hidden by an abstraction. Unrecognized responsibility does not
establish zero responsibility. Do not merely unhide recognition-only 100s, invert
the scale, insert blanket zeroes, cap scores arbitrarily, or reward implementation
size/complexity as if it were hidden responsibility. Specify and test the fallback
model and its known limitations. Improve supported evidence for delegated
same-workspace behavior and non-throwing validation, including StatusCode results.
StoredTableRowCodec and SqlDerivedReferenceValidator in River are required
regression examples: explain their rating using their actual responsibilities.

Implementation order:

1. Add failing end-to-end coverage tests for numeric display, sorting, export and
   SCORE on precise, estimated and semantically unsupported source.
2. Implement the bounded rating path and report projection together; preserve
   honest uncertainty metadata and version/invalidate cached projections.
3. Cover Java, TypeScript, Go and Rust. Include TS project references/classes,
   Go incomplete type information, and Rust out-of-line modules/cfg/reexports.
4. Run normative semantic acceptance and real-repository coverage separately.
   Report applicable files, numeric ratings, estimates, N/A and failures per
   language. Every missing promised rating is a product defect, not a passing
   unsupported case. Estimate coverage alone does not demonstrate semantic
   conformance. Inspect score distributions and deep-versus-shallow examples.

Numeric publication does not establish complete semantic proof. Keep automated
fix eligibility and evidence fingerprints gated by their own safety requirements.
An analyzer crash must remain observable as a failure; any recovered rating must
be identified as fallback analysis rather than a successful precise evaluation.

## P0 second: enterprise performance

Start performance remediation after the numeric-rating path works end to end.
Do not close release readiness using the former small-repository scope exception.
The user explicitly rejects five-minute analysis for 30,000 files. Establish
reproducible 30K-file runs representative of the supported languages, with
hardware, source mix, command, cache state, elapsed time, peak memory and output
size recorded. Measure cold analysis, unchanged warm analysis, one-file edit and
idle watch behavior. Profile parsing/type loading, semantic analysis, IPC,
serialization and report rendering before choosing optimizations. Set and record
the numerical release latency budget; a small-repository extrapolation is not an
accepted capacity measurement.

Preserve rating coverage and deterministic results while optimizing. Avoid
repeated whole-project work, bound analysis and evidence growth, and validate
cache invalidation for changed dependencies. Report remaining performance gaps
explicitly; neither P0 is complete merely because unit tests pass.
