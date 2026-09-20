# Structural scoring performance baseline

Measured 2026-09-20 on an Apple M5 host (10 CPUs, 24 GiB RAM), macOS 26.6.2,
Darwin 25.6.0, arm64. Toolchain versions were Go 1.27.1, Node v26.9.0,
Rust 1.98.1, and OpenJDK 27. The preserved
baseline binary is
`/private/tmp/slopwatch-baseline-runnable/build/slopmark` with SHA-256
`0b7c26cb20f190ce0250674ba5b19349626a7ef661e114c2d1b7df6eda3b9965`.

The reusable measurement driver is
[`tools/structural_scoring_performance.py`](../../../tools/structural_scoring_performance.py).
It snapshots sources, creates a unique hardlink workspace for each repetition,
runs cold (cache write, no cache read), warm (cache read), one-file edit (cache
read), and a PTY follow interval. The edit uses an atomic replacement and the
source snapshot hash is checked after every run. CLI CPU, system CPU, and peak
RSS come from `os.wait4`; idle CPU is the process-tree delta between the
settled dashboard and the end of the explicit ten-second interval. A valid idle
capture requires the dashboard `CACHED <count>` marker, two seconds of quiet
output, and both CPU samples.

The three-repetition reports are compact evidence artifacts:

- [`performance-baseline-slopwatch.json`](performance-baseline-slopwatch.json)
- [`performance-baseline-kafka.json`](performance-baseline-kafka.json)

The first repetition's raw reports are retained under `/private/tmp` for later
candidate ranking comparison:

- Slopwatch: `/private/tmp/slopwatch-structural-perf-szciyigx/rep-1/raw/`
- Kafka: `/private/tmp/slopwatch-structural-perf-t188xmhz/rep-1/raw/`

The Slopwatch source snapshot contains 1,259 supported-language files (989 Go,
158 Java, 66 TypeScript, 46 Rust), 5.56 MB, and has tree hash
`8bb44f68c088da758583ca49b5fd2c278b06a5b8e9249e3b8b593f54b09368f6`.
The normal source discovery policy analyzed 958 files. Kafka contains 6,174
Java files, 71.19 MB, and 1,568,603 lines, with tree hash
`588bc1c94def21028546021b56da5cdb1e8c393de63d12542ee077d4ba705273`; normal
source discovery analyzed 3,782 files.

| Snapshot and phase | Median wall | Median CPU | Maximum RSS | Median report/output |
| --- | ---: | ---: | ---: | ---: |
| Slopwatch cold | 1.498 s | 5.898 s | 393.3 MiB | 27.52 MB |
| Slopwatch warm | 0.323 s | 0.828 s | 255.3 MiB | 27.51 MB |
| Slopwatch edit | 0.274 s | 0.676 s | 372.2 MiB | 27.51 MB |
| Slopwatch idle (10 s) | 12.479 s total; 0.15 CPU-s idle | 0.894 s total | 166.8 MiB | 87,492 PTY bytes |
| Kafka cold | 17.874 s | 21.030 s | 2.39 GiB | 144.81 MB |
| Kafka warm | 1.676 s | 2.572 s | 2.38 GiB | 144.78 MB |
| Kafka edit | 1.784 s | 2.192 s | 2.37 GiB | 144.78 MB |
| Kafka idle (10 s) | 13.939 s total; 0.19 CPU-s idle | 2.195 s total | 1.09 GiB | 123,230 PTY bytes |

Warm and edit phases carry `cache_mode: "read"` and `--use-cache` in every
recorded command. Their stable report sizes and hashes, together with the
large cold-to-warm reduction on Kafka, are the practical cache-reuse evidence;
the raw execution plans provide stronger evidence: Slopwatch cold has four
invocation plans and warm/edit have 66 unit plans with no invocation IDs; Kafka
cold has one invocation plan and warm/edit have four unit plans with no
invocation IDs. One first Kafka warm/edit repetition retained a high JVM RSS
outlier despite unit-plan reuse, so the table reports the maximum RSS across
all three repetitions rather than hiding it behind the median. Idle captures require
the current terminal frame to show the discovered row count (958 for Slopwatch,
3,782 for Kafka) and no scanning, verify, refresh, cached, or provisional
status before the quiet interval. The baseline already exceeds the 2 GiB
peak-RSS reference on Kafka cold runs, so this synthetic and real-repository
evidence does not establish enterprise readiness. Candidate comparison must
use the same snapshots, three repetitions, and contract.
