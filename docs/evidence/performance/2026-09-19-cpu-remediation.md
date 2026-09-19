# SHALLOW frontend CPU remediation — 2026-09-19

The frontend now reuses owner field inventories and full-owner constraints within one analysis, and reuses normalized operation tokens. Unchanged token streams require no copying. Selected roots and contextual receiver copies retain uncached constraint evaluation; preparation invalidates constraints when annotation context changes. No score policy or persistent cache format changes.

## Validation

Race-enabled sourceestimate, native, report, scoring and follow suites pass. Tests cover separate analyses/owners, negative inventory lookups, root subsets, copied receivers, context invalidation, equal-length replacement bodies, empty/nil identity, append ownership and exact token equivalence against the previous normalization loops in all four languages.

Independent review found two remaining normalization reuse opportunities; both were fixed. Additional review requests for enterprise capacity and peak retained memory remain outside these deliberately bounded measurements. Allocation totals are not peak RSS.

## Reproduction

Baseline is commit `49e4c52630b9a2bf04c2bc8f692d45ef86a640b9`. Build baseline and changed `./internal/native` test binaries with `go test -c`. Run `TestPerformanceProfile` from `go/internal/native` with `SLOPWATCH_PROFILE_SOURCE`, a new `SLOPWATCH_PROFILE_OUTPUT`, `SLOPWATCH_PROFILE_FILES=200`, and `SLOPWATCH_PROFILE_EXT=.java`. The harness copies sources and uses an isolated analysis cache. CPU profiles cover frontend work; elapsed times include backend execution. Each capture records source hashes and cold, warm, edit and cache-disabled fresh metrics.

Full JSON reports are compared after removing nondeterministic `invocation_id` fields. Source hashes must match. Warm captures must make zero backend calls. Raw captures are temporary under `/private/tmp/slopwatch-cpu-final`; compact results are retained alongside this document.

These small samples do not establish 30,000-file readiness. Next work should measure progressively larger bounded workloads, retained memory, and the remaining repeated unconditional/control-flow scans before making enterprise claims.

## Repeated Java results

Two sequential before/after pairs on the same 200 Ingres Java files; median values.

| Phase | Before | After | Speedup | Allocated before → after |
|---|---:|---:|---:|---:|
| cold | 10.727s | 4.599s | 2.33× | 6687.6 → 626.2 MB |
| warm | 0.187s | 0.187s | 1.00× | 154.9 → 154.9 MB |
| edit | 10.590s | 4.838s | 2.19× | 6684.6 → 653.6 MB |
| fresh | 10.461s | 4.434s | 2.36× | 6570.6 → 513.3 MB |

Cold allocations decreased 90.6%. Absolute elapsed times varied from the earlier session; these comparisons use adjacent runs in the same session. Edit timings include the backend work triggered by the harness edit, not a claim of file-local analysis. All eight report comparisons pass; warm backend calls are zero.

## Additional language checks

One small paired capture per language; all four phases have identical reports except invocation IDs. These are directional checks, not stable throughput estimates.

| Language/files | Cold before → after | Allocations before → after |
|---|---:|---:|
| go/140 | 1.762s → 1.343s | 1383.5 → 349.5 MB |
| typescript/40 | 1.131s → 0.971s | 99.8 → 55.7 MB |
| rust/40 | 0.267s → 0.182s | 186.5 → 76.5 MB |

## Final checks and remaining hotspot

Frozen acceptance passes all 60 synthetic cases and 12 real cases with zero failures. The first acceptance invocation placed the CLI outside its installation tree and could not find the component catalog; rerunning the same binary under `build/` resolved that setup error.

The new Java CPU profile attributes 0.55s of 2.00s sampled frontend CPU to `gradedUnconditional` (27.5%, cumulative). Repeated control-flow checks during caller analysis are the next optimization target. Cached owner lookup and normalization benchmarks report zero allocations per warmed lookup; these microbenchmarks do not measure cold construction or enterprise capacity.

See `cpu-remediation-results.json`, `cpu-remediation-benchmarks.txt`, `ingres-200-after-cpu-cumulative.txt`, and `ingres-200-after-alloc-top.txt`.

Normal `make` completed successfully and `build/go-cache` was absent afterwards. Temporary isolated profiling caches and copied workspaces were removed after preserving metrics, source-manifest digests and comparison results. No user analysis cache was modified by these measurements.
