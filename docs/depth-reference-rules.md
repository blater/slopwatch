# Situational depth-reference rules

This document makes the Stage A classifier in `depth-reference-plan.md`
operational. It describes evidence, not quality.

## Normalized inputs

The classifier consumes only:

- public-operation count;
- per-operation parameter count, result count, output flag, and mutation flag;
- public named-type count and whether a public type is an interface/trait;
- public field/state count.

Visibility is language-specific at the adapter boundary: Go uses exported
identifiers; Java uses public members plus all methods declared by interfaces;
Rust uses `pub` items and methods of `pub` traits; TypeScript uses exported
top-level declarations and class/interface members not marked `private` or
`protected`.

## Deterministic precedence

Apply these rules in order:

1. `protocol`: at least one public interface/trait type and at least one public
   operation.
2. `data`: no public operations and at least one public field/state member.
3. `command`: at least one operation and zero operations with a result/output.
4. `query`: at least two thirds of operations have results/outputs and no more
   than one third are observable mutations, with a minimum allowance of one
   mutation for very small surfaces.
5. `general`: public operations exist but no stronger rule applies.
6. `unknown`: no public evidence or contradictory/incomplete evidence.

The structural implementation cannot identify status-like return types without
using names, so a status-returning operation is conservatively `query` or
`general`; TypeScript treats only `void`, `undefined`, and `boolean` as
status-like. This difference is explicit and must not affect the reference
until cross-language calibration is available.

## Metadata schema

Every available Stage A SHALLOW measurement reports:

```text
reference_role: protocol | command | query | data | general | unknown
role_confidence: number in [0, 1]
role_basis: public-contract-operations | public-state-surface |
             operation-result-shape | mixed-operation-shape |
             insufficient-or-contradictory-evidence
reference_basis: role-shape-v2
reference_policy: role-shape-v2
reference_sample_count: 0
```

Confidence is descriptive only. It does not alter `F`, `I`, or the score.
`D_ref` remains exactly `1.0` until a reviewed corpus supplies a versioned
role reference.

## Invalid reference behavior

The fast release policy uses finite, positive role priors and shrinks them
toward `1.0` using confidence and normalized surface size. Invalid or missing
role evidence uses `D_ref = 1.0`, `reference_basis: role-shape-v2`, and
`reference_sample_count: 0`.
