# All-language analysis scaling — 2026-09-19

Baseline: `e2fe04c` (the preceding inventory/normalization optimization). Hardware: Apple M5, Darwin arm64. This change preserves scoring rules, limits and persistent cache formats.

## Changes

- Build one control-flow index per exact operation body rather than repeatedly scanning prefixes and recursively checking exits. Short-body scratch stays on the stack. The recursive reference is test-only.
- Index call delimiters and borrow argument spans. Bodies of at most 64 tokens use bounded direct scans to avoid index overhead; this constant cutoff preserves asymptotic scaling. Materialize exact signatures only when needed; evidence filtering matches existing published reasons without manufacturing all nested signatures.
- Cache Rust function, visibility, helper, pointer-contract and workspace release inventories within an analysis. Preserve candidate order and lazy mixed-language lookup behavior.
- Share immutable field-type maps, use contextual overlays, and resolve only receiver names required by local tokens or callee evidence. Avoid the fields × methods cross product across shared caller/constraint passes.

## Measurement method

Synthetic benchmarks use `AnalyzeWithAttribution`, the native frontend's shared production pipeline, for Java, Go, TypeScript and Rust. They validate applicable numeric grades before timing. File-count fixtures grow from 8 to 512 files, both independent files and resolved fixed-fan-out dependency chains. Declaration fixtures grow fields and fixed-sized methods together from 8 to 256. Parser limits are checked so truncation cannot masquerade as better scaling. These are controlled algorithm probes, not representative enterprise workloads.

The same benchmark files run against frozen baseline source and changed source with `-benchtime=100ms -count=2`. Earlier exploratory timings using narrower language-specific entry points are superseded by these shared-pipeline measurements.

Native profiles use two sequential baseline/changed pairs on 200 Ingres Java files, plus one pair each for 140 Go, 40 TypeScript and 40 Rust files. Each capture runs cold, unchanged warm, one-file edit and cache-disabled fresh analysis. Isolated source copies and caches leave user repositories and caches untouched. Source hashes and full reports must match; only invocation IDs are removed from comparison.

Elapsed native time includes backend execution. CPU and Go allocation profiles cover frontend work. `wait4` peak RSS is OS-reported child resource usage over the four-phase harness, not per-phase memory or simultaneous aggregate process-tree memory. Warm runs still include discovery, validation and report work.

Build binaries using `GOCACHE=$PWD/build/go-cache GOPROXY=off go test -C go -c -o <binary> ./internal/native`. Run from `go/internal/native` with `SLOPWATCH_PROFILE_SOURCE`, a new `SLOPWATCH_PROFILE_OUTPUT`, `SLOPWATCH_PROFILE_FILES`, and `SLOPWATCH_PROFILE_EXT`, passing `-test.run=^TestPerformanceProfile$ -test.v -test.timeout=3m`. Inspect `cold.cpu.pprof` with `pprof -top -cum` and `cold.heap.pprof` with `pprof -top -alloc_space`.

## Production-pipeline scaling results

Mean of two benchmark samples. Times are milliseconds; allocations are decimal MB per analysis.

| Language | 256 declarations, before → after | Allocations, before → after | Changed 8 → 256 time growth (32× input) |
|---|---:|---:|---:|
| java | 29.11 → 5.37 | 33.86 → 6.33 | 36.4× |
| go | 24.60 → 6.52 | 25.86 → 8.22 | 33.2× |
| typescript | 15.70 → 4.88 | 14.65 → 6.39 | 33.0× |
| rust | 48.34 → 6.76 | 16.67 → 11.90 | 32.8× |

| Language | 512 independent files, before → after | 512 connected files, before → after | Changed 8 → 512 connected time growth (64× files) |
|---|---:|---:|---:|
| java | 23.23 → 26.85 | 24.67 → 24.59 | 80.1× |
| go | 32.82 → 35.80 | 17.76 → 21.55 | 78.0× |
| typescript | 23.41 → 27.38 | 17.11 → 21.56 | 78.8× |
| rust | 27.61 → 35.63 | 30.90 → 34.10 | 80.6× |

Index construction has a constant cost: some tiny-file workloads remain slower and allocate more than baseline, despite near-linear growth and substantial declaration-heavy improvements. These regressions are retained in the table rather than hidden by the large-method results. Raw allocations and timings are in [before](linear-scaling-before.txt) and [after](linear-scaling-after.txt).

Additional targeted probes:

- Nested-call extraction at 16/64/256/1024 calls: 1.69/6.37/24.14/101.48 µs; [raw results](linear-call-extraction-bench.txt).
- Needed-only nested-call evidence filtering at 1024 calls: 216.3 ms → 0.160 ms, 69 MB → 0.503 MB; [raw results](linear-call-filter-bench.txt).
- Rust release build plus first lookup at 8/32/128 units: 54/222/908 µs; warm batches allocate zero bytes. Malformed parser probes grow near-linearly in tested shapes; [raw results](linear-rust-inventory-bench.txt).

## Native profile results

| Sample | Cold seconds, before → after | Frontend allocated MB, before → after |
|---|---:|---:|
| Java, 200 files (two-pair median) | 4.532 → 3.555 | 626.3 → 722.5 |
| go, 140 files (one pair) | 1.441 → 1.172 | 350.0 → 434.4 |
| typescript, 40 files (one pair) | 1.103 → 0.951 | 55.7 → 62.7 |
| rust, 40 files (one pair) | 0.184 → 0.176 | 76.5 → 98.2 |

Java median warm time is unchanged at 0.185 s; one-file edit falls from 4.549 to 3.646 s and cache-disabled fresh from 4.424 to 3.417 s. Full reports match across all four phases for all four languages, with invocation IDs excluded. Source manifests match. See [complete metrics](linear-analysis-results.json).

Native allocation totals increased despite lower elapsed time; this is a remaining regression, not a memory reduction claim. Java harness peak RSS ranged from 327–647 MB before and 326–650 MB after, with no demonstrated improvement. Per-capture values and other languages are retained in the metrics. The allocation profile identifies repeated call extraction/index construction as a next optimization target: reuse within an immutable body would avoid rebuilding equivalent transient indexes.

The changed Java CPU profile samples 0.42 s cumulatively in AnalyzeWithAttribution; unconditional prefix rescanning no longer dominates. Profiles cover frontend CPU only. See [CPU top](linear-java-cpu-top.txt) and [allocation top](linear-java-alloc-top.txt).

## Limits and remaining work

Cold work cannot be sublinear in source bytes that must be read. The target is input plus dependency edges plus emitted evidence, with warm/incremental work proportional to the affected closure. Complete report output also imposes an output-size lower bound.

These changes do not prove that every input is linear. Ambiguous resolution buckets and dense dependency graphs can require more work; exact nested emitted signatures can themselves contain quadratic output bytes. Complex malformed Rust impl headers with unmatched angle/parenthesis nesting retain legacy depth-sensitive scans. Unprepared manually constructed Rust workspace slices deliberately use compatibility lookup; `BenchmarkRustReleaseFallbackScaling` labels that path explicitly, while `BenchmarkRustReleaseInventoryScaling` exercises the production index.

Incremental analysis still rediscovers/replans the workspace in `currentChangePlan` before selecting affected units. Sublinear end-to-end incremental behavior is therefore not established. A safe persistent plan/dependency index and conservative invalidation are the next major target.

The 30,000-file cold/warm/edit/idle-watch capacity validation remains open. Small samples and synthetic scaling do not establish enterprise readiness. No analysis limits or evidence requirements were weakened to obtain these results.

## Verification

Race-enabled suites passed for sourceestimate, native, report, scoring, follow and fixanalysis/nativeadapter. All 60 synthetic and 12 real frozen graded acceptance cases passed. Existing numeric ratings, uncertainty and evidence metadata remain intact. See [validation summary](linear-validation.json). Independent review covered malformed syntax, exact signature matching, contextual cache identity, shadowing and callee-only dependencies; fixes include randomized frozen-reference comparisons.

Normal `make` rebuilds the shipped binaries and removes the repository Go compiler cache after verification. The cache cleanup policy is unchanged from the prior performance commit.
