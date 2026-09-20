# Continuous severity calibration

Status: frozen candidate passes shared calibration; repository comparisons pending.

Use `weight × log2(1 + max(0, value − baseline) / (reference − baseline))`.
Keep COG reference 15, weight 10, baseline 0; routine CYCLO reference 10,
weight 5, baseline 1; NPATH reference 200, weight 8, baseline 1. Nesting
retains its count severity. Type CYCLO retains its existing curve and the
weighted overlap residual from plan 2. GOD and SHALLOW remain unchanged.

The first candidate passes the tuning relations when applied to the frozen
baseline's raw routine measurements, with per-routine maximum grouping:

| Transformation | Before | After |
| --- | ---: | ---: |
| Remove unused branching | 0.93109 | 0 |
| Flatten guards | 2.63034 | 1.80572 |
| Consolidate repeated state transition | 3.61144 | 1.80572 |

These values agree across the four fixture languages. Each improvement exceeds
0.5 plus the declared tolerance. Index versus rescan remains record-only.
No parameters were adjusted and no held-out relations were evaluated to select
them. Inputs come from [baseline-v1.json](baseline-v1.json), whose provenance
records the source and report hashes.

The implemented candidate passes all 28 gated comparisons across four languages;
the four index-versus-rescan comparisons remain record-only. Raw structural
metrics match the grouping checkpoint. [Measured results](../evidence/structural-scoring/curves-fixture.json)
record policy, binary, catalog, manifest and report hashes.

`make build` passed the shared calibration, cache and projection tests, and
packaged smoke tests. Real-repository ranking and performance comparisons remain
pending. Report failures without retuning against held-out cases.
