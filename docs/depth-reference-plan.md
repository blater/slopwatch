# Situational depth-reference plan

## Goal

Make `SHALLOW` sensitive to the kind of interface being measured. A universal
`D_ref = 1.0` treats a two-method protocol contract as though it were a
general-purpose service boundary.

The first implementation therefore separates three concerns:

1. classify the interface role from normalized facts;
2. select and report a role-shaped reference; and
3. conservatively shrink that reference toward the global reference when
   evidence is weak or the surface is small.

The release policy is explicitly heuristic because it must ship before a
labelled corpus is available. It is versioned as `role-shape-v2`, exposed in
measurement attributes, and must be replaced by reviewed calibration under a
new metric definition rather than silently retuned.

## Role taxonomy

The classifier uses only caller-visible facts and returns a role plus a
confidence and basis:

| Role | Evidence |
| --- | --- |
| `protocol` | Public interface/trait/type with abstract or contract-like operations |
| `command` | Operations predominantly accept inputs and return no data or status-like results |
| `query` | Operations predominantly return data and have limited observable mutation |
| `data` | Public state/type surface dominates and there are few or no operations |
| `general` | Public operations exist but do not meet a stronger role rule |
| `unknown` | Evidence is insufficient or contradictory |

Rules are deterministic, conservative, and language-neutral. An ambiguous
surface is `unknown`, not force-fitted into a role. The role is descriptive
evidence; it is not itself a quality judgement.

## Reference selection

The raw ratio remains:

```text
D = F / I
```

Reference selection is:

```text
prior = role_prior(role)
size = min(1, normalized_public_items / 4)
D_ref = 1 + (prior - 1) × confidence × size
```

The existing conversion then applies:

```text
depthPenalty = 100 × max(0, 1 - D / D_ref)
```

Every measurement reports `reference_role`, `depth_reference`,
`reference_basis`, `reference_policy`, and `reference_sample_count`.

## Calibration protocol

Create a small hand-labelled corpus containing representative protocol,
command, query, data, and general interfaces in Go, Java, Rust, and
TypeScript. Labels must describe role, not quality. For each role:

1. exclude unavailable and ambiguous examples;
2. compute raw `D` using the normative capability/interface algorithm;
3. publish the sample count, median, and robust spread;
4. review paired examples with domain experts;
5. freeze the selected reference and definition version.

Use hierarchical shrinkage when a role has few examples:

```text
role_reference = w × role_median + (1 - w) × global_median
w = n / (n + k)
```

`n`, `k`, the corpus, and the chosen statistic must be published with the
metric version. A live repository median is allowed only as a separate
relative diagnostic; it must not silently change the primary score.

## Delivery stages

### A — evidence and observability

- add deterministic role classification to the shared structural strategy;
- add equivalent role classification to the TypeScript analyzer;
- report role, confidence, evidence basis, and reference basis;
- apply the versioned `role-shape-v2` reference policy;
- add fixtures and cross-language agreement tests.

### B — corpus and calibration

- assemble and review the labelled corpus;
- calculate role references and uncertainty;
- add frozen calibration data and golden score tests;
- replace the heuristic references under a new definition version only after
  review.

### C — validation and maintenance

- monitor classification coverage and unknown rates;
- rerun corpus tests for every analyzer change;
- revise references only through a new definition version.

## Non-goals

The classifier does not infer business value, architectural quality, cohesion,
or whether a method is well designed. Those remain separate concerns. It also
does not use names, comments, LOC, or implementation complexity as evidence.
