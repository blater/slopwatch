# Kafka partial-display observation

Status: insufficient. This run demonstrated an initial burst of rows, not
sustained progress through the expensive analysis. It does not satisfy the
[corrective acceptance plan](../../streaming-scan-plan.md).

Scanned `~/src/kafka` with the rebuilt dashboard in a 160×40 terminal.
`--follow-symlinks` bypassed analysis-cache reuse; Kafka sources were unchanged.

| Elapsed from launch | Visible files |
|---|---:|
| 5.46 s | 0 |
| 10.00 s | 307 |
| 12.18 s | 3,782 |

The table and graph populated while SHALLOW analysis remained active. The
previous producer first displayed 35 files at 25.66 s. These are UI observations,
not an enterprise performance benchmark or a promise of results within two seconds.

Java now flushes completed syntax facts before depth analysis. Local metrics are
computed once per file and reused in the final report. Later SHALLOW results
replace the provisional rows through the same displayed document.

`make build` produced the executable; its full suite currently fails scoring and
cache expectations during concurrent edits to those subsystems. No tests were
added for this correction. All 40 feature source/test files score below 100 with
the preserved baseline scorer; maximum 95.09. The concurrently changed scorer
returned blanket zeroes, so those readings were not used as quality evidence.
