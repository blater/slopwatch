# SHALLOW r21 P0 review

Reviewed 2026-09-17: dirty working tree at HEAD `3cedf99`, packaged CLI reporting
policy `r21`. This is a review of the available implementation, not a claim that
the P0 implementation fixes have been made. Controlling requirements are in the
[delivery directive](shallow-p0-delivery-directive.md).

## Findings

1. **P0: numeric publication is deliberately suppressed.**
   `go/internal/native/scoring_depth.go:24` rejects estimated boundaries from the
   file maximum and SCORE contribution. `go/internal/scoring/metrics.go:99`
   independently hides estimated values. The r20 release amendment explicitly
   required this behavior. River currently publishes only 3 numeric file ratings
   out of 2,207 files: 66 N/A, 2,134 partial and 4 unavailable. Excluding N/A,
   2,138 of 2,141 files (99.86%) lack a numeric rating. This directly violates the
   product requirement. Fix the rating path and all report projections together.

2. **P0 scope: the fallback does not cover ordinary Go, Rust or TypeScript
   projects.** Current real-repository probes below demonstrate that restoring
   Java estimates alone cannot close numeric coverage. TypeScript project
   selection, Go incomplete type information, and Rust module visibility need a
   usable rating fallback as well as better precise analysis. Fast unavailable
   reports are not successful performance results.

3. **High: recognition-only scores still violate the metric's intent.**
   r21 follows more same-workspace Java delegation, including the codec's encoder,
   decoder, validator and publisher. Nevertheless StoredTableRowCodec retains
   `H=0`, SHALLOW 100, with 120 limitation entries; SqlDerivedReferenceValidator
   retains `H=0`, SHALLOW 100, with 110. Merely following dependency edges has not
   translated the hidden responsibility into a useful rating. Neither hiding the
   score nor simply revealing the existing 100 resolves this. Handle non-throwing
   validation and meaningful delegated work in a calibrated bounded estimator.

4. **P0 performance readiness remains unestablished.** The observed River run
   took 20.49 seconds for 2,207 files and emitted 265,261,650 bytes of JSON. This
   was a single run without controlled cache state, peak-RSS measurement or a
   baseline comparison. It does not reproduce or disprove the reported five-minute
   30K-file run. Output/evidence growth and repeated per-boundary delegation
   analysis are profiling candidates: JavaDepthMinimum creates a new behavior
   analyzer per boundary, whose summary cache is instance-local. Do not describe
   either candidate as a measured dominant bottleneck yet. Restore actual 30K
   capacity validation after numeric coverage; the old benchmark deferral cannot
   establish enterprise readiness.

## Fresh language checks

Normative source acceptance was rerun using
`python3 tools/shallow_adapter_acceptance.py --output /private/tmp/shallow-p0-acceptance.json`.
It exited 1: 20 of 59 cases conformed to the existing contract.

| Language | Cases | Numeric boundary values in ledger | Conforming |
| --- | ---: | ---: | ---: |
| Java | 15 | 15 (8 estimated) | 5 |
| Go | 14 | 8 | 8 |
| TypeScript | 16 | 6 | 5 |
| Rust | 14 | 2 | 2 |

Ledger numeric availability is not main-report numeric coverage. These are small
normative cases, not representative repository percentages. Some old acceptance
requirements explicitly expect unknown values and must now be split into
precise-evidence assertions and numeric-product assertions; this must not erase
the remaining genuine semantic mismatches.

Go's forwarding/default-route and validation-bypass examples now pass. Shared
state, behavioral construction and owned aliases remain unsupported in the
normative cases. TypeScript covers a useful scalar/function slice but still
miscounts validation bypass (H=3 instead of 2), and fails reductions, shared state,
default-route variants, behavioral construction and owned aliases. Rust conforms
on only two cases; reduction, validation, state, default routes and construction
remain major gaps. Java still miscounts validation bypass and default-route
burden, and recognizes no hidden responsibility for the pure reduction fixture.

Fresh packaged CLI repository probes used `--format=json`, with `--languages`
for each non-Java probe. Counts are file-level `module_shallowness.raw_max`, not
the internal ledger's `shallow` field.

| Repository | Language | Files | Published numeric ratings | Main limitation |
| --- | --- | ---: | ---: | --- |
| River | Java | 2,207 | 3 | Estimated values suppressed |
| slopwatch/go | Go | 344 | 1 | 31 of 32 package boundaries lack types |
| cargo-coupling | Rust | 40 | 0 | Module visibility, unsupported items/behavior |
| ap/warren | TypeScript | 486 | 0 | `typescript.multiple_projects` |

These probes include environment/build-selection limitations. Those limitations
are precisely why a useful source fallback is needed; the probes are not claims
that every file contains a proven applicable abstraction.

## Instruction changes made during review

Added root `AGENTS.md` and the P0 directive; placed explicit precedence notices in
the active interim release, implementation specification and remediation plan.
Numeric publication comes first across all four languages; enterprise performance
comes second. Preserve estimation metadata and separate automated-fix safeguards.
No scoring or analyzer implementation was changed during this review.

Local raw results: `/private/tmp/shallow-p0-acceptance.json`,
`/private/tmp/shallow-p0-{river,go,rust,typescript}.json` and matching timing files.
