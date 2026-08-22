# Functional depth delivery plan

Target: improve `module_shallowness/ousterhout-v2` from the current
`static-approximation` toward signature-based evidence.

This plan prioritizes the largest immediate improvement: making caller-visible
interface cost accurate before attempting difficult semantic inference such as
authorial design decisions.

## Delivery principles

- Every stage produces a usable, testable result.
- No stage uses LOC, NPath, cyclomatic complexity, or private implementation
  size to increase functional capability.
- Unknown evidence is reported as unknown, never silently converted to zero.
- Cross-language work follows the same normalized fact contract.
- Existing v2 attributes remain interpretable; new attributes are additive.
- A stage is releasable only when its invariants and regression fixtures pass.

Effort is expressed in engineer-days for someone familiar with this repository.
Ranges include implementation and focused tests but exclude waiting for review,
release administration, and empirical calibration.

## Stage 0 — Freeze the measurement contract

Effort: **0.5–1 day**

### Work

- Confirm the normalized names and types for `PublicOperation`, `TypeShape`,
  `CapabilityProcess`, and `RepresentationExposure`.
- Define how unavailable fields are represented in the bridge and report.
- Add `evidence_level`, `evidence_coverage`, and `capability_basis` to the
  documented measurement attributes.
- Confirm that v2 remains the definition version; this is an evidence-quality
  improvement, not a new mathematical formula.

### Benefit

Prevents each analyzer from inventing a different interpretation of interface
cost or capability. It also makes later score comparisons explainable.

### Acceptance

- Schema examples exist for a function with parameters/results and a public
  field.
- Missing type or capability evidence has an explicit representation.
- A report consumer can distinguish static approximation from signature-based
  evidence.

## Stage 1 — Add shared signature facts

Effort: **2–3 days**

Dependency: Stage 0

### Work

Extend the normalized structural facts with:

```text
PublicOperation {
  stable_id
  visibility
  parameters: [ValueShape]
  result: ValueShape?
  receiver_shape?
  emits_output: bool | unknown
  observable_mutation: bool | unknown
}

ValueShape {
  stable_id
  kind
  children
  exposed_members
  complexity
}
```

Implement deterministic type-complexity calculation centrally:

```text
primitive/opaque named type = 1
container/wrapper           = 1 + child costs
record/tuple                = 1 + member costs
union/generic               = 1 + constituent costs
recursive edge              = 1
per-signature cap           = 32
```

### Benefit

Replaces declaration-count approximations with the information callers
actually need to learn and supply. It establishes the foundation for accurate
Entries, Exits, and public representation detection.

### Acceptance

- Protocol round-trips primitive, generic, recursive, tuple, and function types.
- Stable IDs are unchanged by unrelated source edits.
- Recursive type graphs terminate and respect the cap.
- Existing analyzers remain compatible when the new facts are absent.

## Stage 2 — Implement Go signature extraction

Effort: **2–3 days**

Dependency: Stage 1

### Work

Extract from Go AST/type information:

- exported functions and methods;
- parameter and result tuples;
- pointer, slice, map, channel, struct, interface, generic, and named types;
- exported struct fields and their mutability;
- method receiver shape;
- exportedness at the file/package boundary.

Do not use `ForeignTypes` from private type declarations as interface evidence;
that fact includes representation dependencies and is not necessarily caller
visible.

### Benefit

Go results become sensitive to actual API shape. For example, adding a complex
request parameter or exposing a mutable field increases interface cost, while
adding private helpers does not.

### Acceptance

Fixtures prove:

- `Run()` costs less than `Run(ComplexRequest) ComplexResponse`;
- private fields and helpers do not increase interface cost;
- exported fields do increase representation exposure;
- equivalent named and generic type shapes produce deterministic costs.

## Stage 3 — Integrate signature facts into capability and scoring

Effort: **1–2 days**

Dependency: Stages 1–2

### Work

Replace the current Go operation fallback with:

```text
Entry = each distinct public input/triggering data group
Exit  = each distinct public result/output data group
```

Calculate:

```text
F = Entries + Exits + recognized Reads + recognized Writes
I = operation cost + parameter/type cost + exposed-state cost + IH cost
```

For this stage, `Reads`, `Writes`, and advanced `IH` remain unavailable and are
excluded from the available-weight calculation. Emit:

```text
evidence_level: 2
capability_basis: "cosmic-inspired-signature"
```

### Benefit

This is the first stage that materially improves the depth ratio itself rather
than merely preparing data. Modules with the same operation count but more
complex caller contracts will receive a lower depth ratio.

### Acceptance

- Same capability with a larger interface produces higher SHALLOW.
- Adding private implementation code leaves `F`, `I`, and SHALLOW unchanged.
- Adding a public input or output changes the appropriate movement count.
- Missing optional evidence is renormalized, not treated as zero quality.

## Stage 4 — Bring TypeScript to parity

Effort: **1–2 days**

Dependency: Stages 1 and 3

### Work

Use the existing TypeScript checker and AST to emit the shared shapes for:

- exported functions and variables containing functions;
- exported classes and public methods/properties;
- interfaces, type aliases, unions, intersections, tuples, generics, and
  callbacks;
- return types and parameter types;
- readonly versus mutable properties.

Replace the local approximation with the shared formula and attribute names.

### Benefit

TypeScript stops being a separate interpretation of depth. Cross-language
comparisons between equivalent Go and TypeScript APIs become meaningful at the
signature evidence level.

### Acceptance

- TypeScript and Go equivalent fixtures have matching intermediate costs where
  their normalized shapes match.
- Existing TypeScript structural tests continue to pass.
- TypeScript reports the same definition, evidence level, and attribute names.

## Stage 5 — Add public representation exposure

Effort: **1–2 days** for Go and TypeScript; **2–3 additional days** for parity
in Java and Rust.

Dependency: Stages 1–4

### Work

Emit `RepresentationExposure` facts for:

- public mutable fields/properties;
- public collection values that callers can mutate;
- concrete storage types in public signatures;
- direct getter/setter exposure of internal state;
- exposed structured members that force representation knowledge.

Keep simple and structured visible variables separate for the Rising–Calliss
component. Do not flag immutable value objects merely for being public.

### Benefit

This is the first real information-hiding signal. A module can no longer score
well solely because it has a compact API if that API exposes its representation
directly.

### Acceptance

- Public mutable state increases leakage penalty.
- Private state and immutable opaque values do not.
- Getter/setter fixtures identify direct representation exposure.
- Evidence is omitted when the language adapter cannot determine mutability.

## Stage 6 — Add configured persistence movements

Effort: **2–3 days** for recognizer/configuration infrastructure and Go;
**1–2 additional days** for TypeScript; **2–4 additional days** for Java/Rust.

Dependency: Stage 3

### Work

Add project-scoped recognizers for configured database, file, cache, and
repository APIs. Each rule declares:

```text
language, receiver/module, operation, read/write kind,
store identity, data-group identity rule
```

Count each distinct `(process, direction, store, data-group)` once. Ordinary
local assignments and unconfigured ambiguous methods do not count.

### Benefit

The numerator begins to measure actual useful work rather than only API
surface. A module that exposes a small API but performs meaningful persistence
operations receives more capability credit.

### Acceptance

- Configured reads and writes are detected exactly once.
- Local variable access is never counted.
- Read followed by write counts as two movements.
- Configuration changes invalidate relevant cached analysis.

## Stage 7 — Detect observable outputs and mutation

Effort: **2–3 days**

Dependency: Stages 1–3

### Work

Recognize high-confidence output/mutation evidence:

- writes to public receiver state;
- mutation of caller-owned reference arguments;
- configured event/emitter calls;
- standard output/response APIs;
- returned values already covered by Stage 3.

Do not infer output from method names such as `process`, `save`, or `update`.

### Benefit

Void operations that nevertheless provide a real externally observable service
are no longer treated as capability-free.

### Acceptance

- A void mutating operation gets an Exit or observable-effect movement.
- Private local mutation does not.
- Ambiguous mutation is reported as unknown rather than counted.

## Stage 8 — Add movement ownership and deduplication

Effort: **2–4 days**

Dependency: Stages 3, 6, and 7

### Work

Build a per-module/process movement set. Enforce:

- private helper calls never become movements;
- repeated loop execution counts once;
- cross-file calls are counted only at the selected boundary;
- one movement has one owning module;
- overloads share capability process identity when appropriate but retain
  separate interface operation cost.

### Benefit

Prevents the most damaging semantic inflation risk: a refactor that introduces
helpers, loops, or extra internal calls must not make a module appear more
functional.

### Acceptance

- Helper extraction leaves `F` unchanged.
- Loop wrapping leaves movement counts unchanged.
- Cross-module fixtures have no double-counted movement.
- Overload fixtures have stable process and interface counts.

## Stage 9 — Java and Rust parity

Effort: **2–3 days per adapter** for signatures; **2–4 days per adapter** for
persistence/configuration and exposure parity.

Dependency: Stages 1–8 as applicable

### Work

- Java: extend the compiler-tree facts and binary bridge for visibility,
  parameters, results, generic types, fields, and configured effects.
- Rust: extend `syn` facts for `pub` visibility, signatures, generic types,
  fields, mutability, and configured effects.
- Add equivalent fixtures to the common conformance corpus.

### Benefit

All bundled language analyzers use the same v2 semantics rather than silently
providing different quality levels under the same column.

### Acceptance

- Equivalent fixtures match at the normalized fact level.
- Binary Java protocol remains backward-compatible within the repository.
- Unsupported language constructs report evidence gaps explicitly.

## Release checkpoints

### Checkpoint A — signature-quality release

After Stages 0–4, estimated **6.5–11 engineer-days**.

Ship when Go and TypeScript pass the signature conformance corpus. This is the
highest-value early release: it makes interface cost substantially more real
without requiring persistence or design-decision inference.

### Checkpoint B — representation-aware release

After Stage 5, estimated **9.5–16 engineer-days**.

Ship when public mutable representation affects leakage consistently in Go and
TypeScript. Java/Rust parity may follow without blocking the first two
languages if evidence level is reported.

### Checkpoint C — capability-aware release

After Stages 6–8, estimated **16.5–28 engineer-days**.

Ship when configured persistence, observable effects, ownership, and
deduplication pass the corpus. This is the first release that deserves the
`cosmic-inspired` label without qualification.

### Checkpoint D — cross-language parity

After Stage 9, estimated **28.5–48 engineer-days**, depending on persistence and
exposure scope. Enable broad cross-language comparison only after normalized
fixtures pass for all bundled adapters.

## Immediate recommendation

Implement Stages 0–4 first. They provide the largest immediate benefit for
the lowest risk, are already supported by the existing TypeScript checker and
Go AST/type information, and create the shared facts needed by every later
stage. Do not begin responsibility-cluster inference until movement ownership
and representation exposure are available; otherwise the graph will be built
on unreliable capability nodes.
