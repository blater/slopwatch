# Repository instructions

## SHALLOW release priorities (user mandate, 2026-09-17)

Read [the P0 delivery directive](docs/shallow-p0-delivery-directive.md) before
changing SHALLOW, its reporting, acceptance tests or release criteria.

1. **P0 first: publish useful numeric ratings.** Applicable supported-language
   source must receive a SHALLOW rating in the normal report, ranking and SCORE.
   Incomplete semantic evidence must not turn its rating into `X`, blank or an
   omitted contribution. Report estimation and limitations separately.
2. **P0 second: enterprise performance.** Five minutes for 30,000 files is
   unacceptable. Measure and improve cold, warm and incremental performance;
   small-repository timings do not establish enterprise readiness.

These instructions supersede older documents that require estimates to be hidden
or defer enterprise capacity validation. Do not solve the first P0 by silently
calling every unknown responsibility zero, emitting blanket 0/100 scores, or
merely displaying the known misleading recognition-only 100 estimates. Deliver
a defensible bounded approximation across Java, TypeScript, Go and Rust.
Preserve honest evidence metadata and separate safety gates for automated fixes.
