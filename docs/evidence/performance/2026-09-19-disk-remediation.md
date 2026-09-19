# Lossless disk reduction — 2026-09-19

The user requested radical disk reduction after the small-sample pprof baseline,
then required normal `make` to discard old compiler-cache output automatically.
This change addresses disk storage; it does not claim to fix the SHALLOW CPU and
allocation hotspots identified in [the profile](2026-09-19-pprof.md).

## Measured outcome

| Item | Before | After | Verification |
|---|---:|---:|---|
| Existing user artifact payloads | 3,126,896,090 bytes | 260,964,911 bytes | 5,372 canonical contents unchanged; zero hash mismatches |
| Entire user analysis cache, filesystem allocation | 2.9 GiB | 279 MiB | `du -sh ~/.slopwatch/analysis` |
| 200-file sample artifacts | 26,130,974 bytes | 2,170,529 bytes | Existing refs retained; before/after full reports identical |
| Project compiler cache | about 11 GiB | absent | Successful ordinary `make` deleted `build/go-cache` |
| Profiling directory | 291 MiB | 82 MiB | Reports compressed; redundant temporary caches removed |

Artifact payload reduction is 91.65% for the existing user cache and 91.69% for
the sample. No analysis records or generations were deleted. The original
5,372-file manifest was independently rechecked after compaction: every stored
payload, once decompressed, has exactly its original SHA-256. See
[disk-compaction-results.json](disk-compaction-results.json).

The existing user cache was compacted with:

```sh
./build/slopcache compact --root /Users/blater/.slopwatch/analysis
```

Result: `artifacts=5372 compacted=5372 skipped=0 errors=0`.

## Storage and compatibility

New unit/projection artifact writes use deterministic gzip BestSpeed when smaller.
Artifact IDs still hash the canonical uncompressed envelope. Reads accept legacy
plain JSON and compressed representations, then verify the canonical digest and
existing envelope checksum, schema and key. Replacement is atomic and private;
logical content remains immutable. References and historical manifests do not
need rewriting. Older binaries may treat compressed entries as cache misses;
the workspace binaries were rebuilt with the new reader.

Compressed decoding is bounded at 128 MiB and rejects truncation, CRC failures,
concatenated members and trailing data. Larger new envelopes retain plain storage
so enterprise cache availability is preserved. Explicit compaction skips physical
files over the limit. This is not a new bound on legacy plain artifact loading.

The maintenance command requires an explicit existing cache root with the expected
nonsymlink subdirectories. It validates canonical artifact paths and checksums,
leaves corruption/noncanonical files unchanged, reports write/read errors and
supports cancellation. No compaction scan is added to normal analyzer startup.

## Latency and correctness

The same isolated 200-file Ingres source prefix produced:

| Phase | Before compression | After compression |
|---|---:|---:|
| Cold cache miss | 5.606 s | 5.588 s |
| Unchanged warm | 85.7 ms | 98.8 ms |
| One-file comment edit | 5.624 s | 5.509 s |
| Cache reads disabled | 5.581 s | 5.476 s |

These are individual local captures, not statistically established speedups.
Warm decompression cost about 13 ms in this pair. Cold analysis still allocates
about 6.68 GB. Compression is a disk improvement, not the CPU remediation.

The complete baseline and compressed cold JSON reports compare equal, including
all file/component ratings, diagnostics and SHALLOW depth evidence. The harness
also verifies zero backend invocations on warm reuse and incremental/fresh SCORE
agreement. Captures live in `build/performance-profile/ingres-200-verified` and
`build/performance-profile/ingres-200-compressed`; full reports use `.json.gz`.

## Build cache policy

The 11 GiB `build/go-cache` was Go compiler/test output, not analysis evidence or
runtime data. The Makefile explicitly sets GOCACHE there. It contained 51,052
entries, most last used September 16–18. The installed Go toolchain evicts by age
(roughly five unused days), not a byte budget, so repeated development builds
retained many variants. A separate default Go cache also exists outside this
repository and was not deleted.

Successful ordinary `make`, `make build` and `make dev-build` now call
`clean-go-cache`, removing that directory while retaining completed binaries.
The full `make test` defers cleanup until its prerequisites finish, avoiding
cleanup during its parallel test work. `make clean` already removed the entire
build tree and continues doing so. `make clean-go-cache` is available for explicit
compiler-cache-only cleanup. Targeted low-level build/test commands and failed
builds can retain temporary compiler entries until the next successful normal
build or cleanup. Subsequent changed-source builds may take longer because old
compiler intermediates are no longer retained.

An actual ordinary `make` passed, removed the cache, and left `build/slopmark` and
`build/slopwatch` present. Its only output was existing Java deprecation warnings.
No tests were rerun after cleanup merely to repopulate the discarded cache.

## Validation and remaining work

Full analysiscache and maintenance-command suites passed, including race checks.
Focused native cache suites passed, including race checks. Added cases cover
legacy roundtrips, compression corruption/limits, unchanged generation references,
concurrent reads/writes/compaction, accurate same-process concurrent compaction
accounting, cancellation, write failures, malformed canonical files, and actual
CLI compaction of a populated legacy cache. Permission-failure coverage explicitly
skips privileged runners that bypass directory permissions. Small sample harness
runs passed; source sampling excludes nonregular files.

Independent reviews produced fixes for bounded physical compaction reads, large
artifact compatibility, filesystem error reporting, concurrent accounting,
cancellation before replacement, CLI regression coverage and source FIFO safety.

No retention/eviction policy was added to the analysis cache: old generations still
accumulate, though each new artifact is much smaller. That pre-existing gap is
recorded in the deferred-work ledger. The SHALLOW repeated-scan/allocation work,
backend-specific profiling, configured incremental-unit testing and enterprise
capacity validation remain separate follow-up work.
