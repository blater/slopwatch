# Real-repository scoring comparison

The frozen Slopwatch and Kafka snapshots exercise 958 and 3,782 analyzed files.
Both comparisons use identical source snapshots and preserved analyzer binaries.
[Machine evidence](../evidence/structural-scoring/repository-comparison-v1.json)
records report hashes, distributions and the largest ranking changes.

Raw measurement evidence is identical for every component in every compared
file. GOD, coupling and SHALLOW contributions are unchanged. No files disappear.
SHALLOW estimation and coverage states also remain unchanged; a finite
contribution alone is not proof of complete semantic evidence.

| Repository | Median SCORE, old → new | p95 SCORE, old → new | Increased / decreased / unchanged |
| --- | ---: | ---: | ---: |
| Slopwatch | 10.52 → 20.06 | 74.65 → 73.43 | 646 / 231 / 81 |
| Kafka | 10.85 → 13.92 | 83.69 → 102.03 | 2,074 / 121 / 1,587 |

Continuous severity makes moderate complexity visible below the old thresholds.
Separate routines still accumulate, so files containing many moderate routines
can rise substantially. Slopwatch's `fix_view.go` rises from 22.69 to 200.77;
Kafka's `GroupMetadataManager.java` rises from 795.79 to 1,178.12. Grouping
reduces repeated charges elsewhere: Slopwatch's `graded_surface_main.go` falls
from 90.88 to 28.88.

The refactored `graded_caller_scan_refs.go` remains at zero under both policies.
Its result did not select the formula. These repository rankings expose
migration effects; they are not independently labelled quality judgments.
Review configured pass thresholds against the new distribution rather than
silently converting them. The frozen paired calibration supplies directional
acceptance, while repository measurements check preservation and scale.

Slopwatch includes analyzer fixtures and checked-in evidence sources; Kafka's
configured discovery excludes some source inventory. Report analyzed counts
separately from inventory counts. Performance and capacity acceptance are
recorded in the [performance evidence](../evidence/structural-scoring/performance-baseline.md).
