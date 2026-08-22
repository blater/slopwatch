# Functional module depth implementation plan

Target definition: `module_shallowness/ousterhout-v3`

The normative algorithm is in [`depth-design.md`](depth-design.md). This file
defines the implementation sequence, interfaces, and acceptance criteria.

The detailed feasibility and evidence decisions for capability/data groups,
persistence, and information hiding are in
[`depth-evidence-design.md`](depth-evidence-design.md). That document estimates
13–21 engineer-days for a minimum credible evidence implementation and 45–65
engineer-days for cross-language evidence parity.

The task-level delivery sequence, benefits, estimates, and release checkpoints
are in [`depth-delivery-plan.md`](depth-delivery-plan.md).

## 1. Freeze the versioned contract

1. Add `ousterhout-v3` to the component catalog with the same file scope and
   0–100 integer range as the current component.
2. Decide the migration switch: v2 becomes the default only when every bundled
   adapter can provide at least the static-approximation capability basis;
   otherwise retain v1 as the default and expose v2 as an opt-in component
   until coverage is complete.
3. Preserve `ousterhout-v1` during the migration. Do not reinterpret historical
   cached values or silently change score meaning.

Acceptance: catalog, protocol, cache, and report tests distinguish v1 and v2
by definition version.

## 2. Extend the normalized structural facts

Add language-neutral facts rather than teaching the scoring strategy to parse
source syntax:

```text
PublicOperation {
  stable_id, name, visibility, parameters, return_type,
  triggering_entry, emits_output, observable_mutation,
  capability_process_id
}

TypeShape {
  kind, stable_id, children, exposed_members, opaque
}

CapabilityProcess {
  stable_id, owner, entries, exits, reads, writes, basis
}

RepresentationExposure {
  kind, stable_id, source, evidence
}

DesignDecisionEvidence {
  cluster_id, evidence_kind, confidence
}
```

Required invariants:

- stable IDs are deterministic for unchanged source;
- private helper calls do not become capability movements;
- each movement has exactly one owning module at the selected boundary;
- unavailable evidence is represented explicitly, never as zero;
- adapters may report `static-approximation` or `cosmic-inspired` basis.

Update the bridge/protocol schema and reject malformed v2 facts with a clear
validation error.

## 3. Implement adapter extraction

Implement the facts in this order:

1. Go structural adapter: exported operations, parameters/results, exported
   fields, recognized persistence calls, and public-operation fallback events.
2. TypeScript adapter: exports, public class/interface members, checker-derived
   type shapes, return/parameter types, and recognized observable outputs.
3. Java adapter: public/protected members and generic/type shapes, including the
   existing PMD-backed path where source facts are unavailable.
4. Rust adapter: `pub` visibility, signature types, public fields, and the
   crate/module-boundary rule.

Each adapter must include fixtures for: no-argument commands, void operations
with observable effects, overloaded operations, generic types, recursive
types, public mutable state, and private-only implementation helpers.

## 4. Implement the language-neutral strategy

Create a new strategy beside the existing shallowness strategy. It must:

1. aggregate `E`, `X`, `R`, and `W` from capability facts;
2. compute operation, exposed-state, recursive bounded type, and information-
   hiding costs;
3. compute `D`, depth penalty, leakage penalty, and final `SHALLOW` exactly as
   specified;
4. omit unavailable optional terms and report available/total weights;
5. return unavailable rather than a misleading score when `F` or the public
   boundary is unavailable;
6. emit all diagnostic attributes in the reporting contract;
7. never inspect LOC, statement count, NPath, cyclomatic complexity, or private
   method count for the v2 score.

The recursive type calculator must memoize by stable type ID, charge one for a
cycle back-edge, and enforce the per-signature cap of 32.

## 5. Update TypeScript's separate analyzer

The TypeScript analyzer currently calculates shallowness independently from
the Go structural host. Replace its old statement/export calculation with the
same v2 facts and formula, or route TypeScript through the shared structural
contract. The output must use identical definition, attribute names, and
rounding rules.

Acceptance: equivalent TypeScript fixtures produce the same intermediate
values regardless of analyzer route.

## 6. Update consumers and documentation

1. Update the component documentation and README to say “functional capability
   per caller-visible interface cost”, not “implementation complexity hidden”.
2. Update help/detail views to explain `F/I`, the COSMIC-inspired basis, and
   that higher SHALLOW is worse.
3. Show `raw_depth_ratio` and capability basis in detail output, not the full
   attribute list in the overview.
4. Ensure unavailable v2 values render as `-` and do not become zero in score
   aggregation.
5. Keep the column label `SHALLOW`; this remains a penalty, not the raw depth
   ratio.

## 7. Verification and calibration

Unit tests must verify these properties:

- same `F` with lower `I` gives higher raw depth and lower penalty;
- adding unnecessary private code leaves v2 unchanged;
- adding a real public capability changes `F`;
- adding a parameter or complex public type increases `I`;
- exposing representation increases leakage penalty;
- splitting one operation into private helpers does not change `F`;
- internal local reads/writes do not change `F`;
- duplicate executions do not duplicate movement counts;
- recursive types terminate deterministically;
- zero-public-API modules are unavailable, not infinitely deep;
- missing optional evidence renormalizes weights deterministically;
- v1 and v2 values can coexist without cache or report collisions.

Build a small hand-labelled corpus of modules with the following paired cases:

- same capability, simple versus bloated interface;
- same interface, useful versus dead implementation code;
- one cohesive responsibility versus two unrelated responsibilities;
- abstract representation versus exposed storage;
- equivalent implementations in Go, Java, TypeScript, and Rust.

Use the corpus to test ordering and invariants first. Do not present `D_ref =
1.0`, the 80/20 split, or the 32 type cap as empirical thresholds. If later
calibration changes them, publish the evidence and create `ousterhout-v3`.

## 8. Delivery sequence

1. Land design and schema documentation.
2. Add facts and protocol validation without changing the default score.
3. Implement Go strategy and fixtures.
4. Implement adapters and cross-language fixtures.
5. Add TypeScript parity.
6. Add report/UI compatibility and README/help text.
7. Run the full test suite and compare v1/v2 reports on representative projects.
8. Enable v2 by default only after coverage and migration criteria pass.

The evidence-specific rollout is: shared facts and Go/TypeScript signatures;
persistence recognizers and configuration; Java/Rust parity; information-
hiding graph evidence; then cross-language conformance and default migration.
