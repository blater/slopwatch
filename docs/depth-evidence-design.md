# Depth evidence designs

Status: design complete; implementation estimate

The staged delivery sequence is specified in
[`depth-delivery-plan.md`](depth-delivery-plan.md).

Target metric: `module_shallowness/ousterhout-v2`

This document retires the three evidence risks left open by the initial depth
implementation:

1. identifying useful functional capability and COSMIC-like data movements;
2. identifying genuine persistent Reads and Writes; and
3. identifying information-hiding leakage, multiple design decisions, and
   extraneous public entities.

The design is intentionally conservative. Static analysis may produce a
positive fact only when it has evidence for it. It must produce an explicit
`unknown`/unavailable state when syntax alone cannot distinguish an
implementation detail from an externally meaningful behavior.

## Executive assessment

| Workstream | Practicality | First useful implementation | Full four-language implementation |
| --- | --- | ---: | ---: |
| Capability/data groups | Medium | 5–8 engineer-days | 20–30 engineer-days |
| Persistence movements | High with configuration | 3–5 engineer-days | 10–15 engineer-days |
| Information hiding | Medium for leakage; low for fully automatic design decisions | 5–8 engineer-days | 20–30 engineer-days |
| Shared facts/protocol/tests | High | 5–8 engineer-days | included in totals below |

Combined estimate:

```text
minimum credible v2 evidence: 13–21 engineer-days
cross-language v2 evidence:   45–65 engineer-days
```

These are implementation estimates for an engineer familiar with the current
repository, not calendar promises. They exclude UI redesign, empirical
calibration against external projects, and a full requirements-level COSMIC
measurement tool.

The work is practical if v2 is explicitly described as a static, evidence-based
proxy. It is not practical to promise exact COSMIC function points or exact
Rising–Calliss design-decision counts from arbitrary source code alone.

## Shared evidence model

All three workstreams use the same module boundary: one analyzed source file.
Every fact has:

```text
stable_id       deterministic identity within the analysis inventory
owner_module    canonical source-file path
basis           inference method
confidence      exact | inferred | configured | unknown
evidence        source location and normalized explanation
```

The normalized protocol adds these concepts:

```text
PublicOperation {
  stable_id
  owner_module
  name
  visibility
  parameters: [ValueShape]
  result: ValueShape?
  emits_output: bool | unknown
  observable_mutation: bool | unknown
}

ValueShape {
  stable_id
  type_kind
  data_group_key
  exposed_members
  complexity
}

CapabilityProcess {
  stable_id
  owner_module
  triggering_entries: [Movement]
  exits: [Movement]
  reads: [Movement]
  writes: [Movement]
  basis
  confidence
}

Movement {
  stable_id
  kind: entry | exit | read | write
  data_group_key
  external_endpoint
  persistence_store?
  evidence
}

RepresentationExposure {
  stable_id
  kind
  entity
  evidence
  confidence
}

ResponsibilityCluster {
  stable_id
  symbols
  evidence
  confidence
}
```

Facts are additive: an adapter may provide some exact facts and leave others
unknown. The scoring strategy must not convert unknown into zero and must
report the evidence basis in measurement attributes.

## 1. Capability and data-group design

### Objective

Replace operation-count capability with an approximation of:

```text
F = distinct Entries + Exits + Reads + Writes
```

This measures externally meaningful functionality rather than code volume.

### What counts as a functional process

A functional process is a coherent externally initiated service owned by the
module. At source level, use this priority order:

1. An explicitly configured public operation is one process.
2. A public operation with a distinct return/output type is one process.
3. A public operation with a distinct triggering input type is one process.
4. If multiple public operations are obvious overloads of one service, they
   share a process ID but remain separate interface operations.
5. Private helper functions are never separate processes.

The analyzer must not infer separate capabilities merely because a function
contains multiple branches or calls many helpers.

### Entry inference

For a public operation:

- a parameter or receiver state used by the operation creates an Entry for the
  corresponding data group;
- a parameterless public operation creates one triggering Entry for the
  invocation event when it has an observable result or mutation;
- a public operation with no evidence of input, output, or mutation produces an
  operation-level approximate Entry only and is marked `inferred`.

Each distinct parameter data-group key is counted once. A key is derived from
the normalized public type and parameter role; parameter names alone are not
data groups.

### Exit inference

Create an Exit when the operation:

- returns a non-void value;
- emits a value through a recognized output API;
- mutates a caller-visible object or resource; or
- is explicitly configured as an output operation.

Returning an internal helper result does not create an additional Exit if the
same data group is already returned by the owning public process.

### Data-group identity

Use the following deterministic key, in priority order:

1. configured object-of-interest name;
2. stable public type identity plus direction (`input`, `output`, `stored`);
3. normalized structural type shape plus direction;
4. operation and parameter identity as an approximate fallback.

Two fields of one record type form one data group unless the adapter has
evidence that they describe different objects of interest. Two different
public type identities are different data groups even when structurally
similar.

### Ownership and deduplication

At file scope:

- a movement crossing from a functional user into the module belongs to the
  callee file;
- a movement leaving the module belongs to the caller-visible process that
  owns it;
- an internal call between functions in the same file is not a movement;
- a call to another file is a candidate Exit/Entry pair, but counts only when
  it is an externally meaningful process interaction rather than a helper call;
- repeated calls in loops count once per data-group key and direction.

The analyzer maintains a per-process set of movement keys to enforce this.

### Capability basis levels

```text
static-approximation
  Public operation and return/output inference; no reliable data groups.

cosmic-inspired
  Data groups and movement kinds inferred from signatures, recognized effects,
  and configured boundaries.

requirements-backed
  Future mode only; facts supplied from explicit functional requirements.
```

Go, Java, Rust, and TypeScript now provide Level 2 signature evidence plus a
conservative representation-exposure signal. Their basis is reported as
`cosmic-inspired-signature`; this is deliberately distinct from the later
`cosmic-inspired` level, which requires data-group and observable-effect
inference. No adapter may label source-derived evidence
`requirements-backed`.

### Capability work estimate

| Task | Shared | Per adapter |
| --- | ---: | ---: |
| Fact types, identity, deduplication | 2 days | — |
| Go signatures and public operations | — | 3–4 days |
| TypeScript checker/type-shape extraction | — | 3–4 days |
| Java compiler-tree signatures | — | 3–4 days |
| Rust `syn` signatures and visibility | — | 3–4 days |
| Fixtures and cross-language conformance | 2 days | 1 day each |

Risk retirement criterion: at least 90% of the hand-labelled fixture movements
are classified correctly, and all known implementation-only helper cases are
excluded. Remaining unknowns must be visible in attributes rather than hidden
in the numeric result.

## 2. Persistence movement design

### Objective

Distinguish COSMIC-like persistent Reads and Writes from ordinary local data
access:

```text
Read  = data group retrieved from persistent storage
Write = data group stored in persistent storage
```

### Recognition model

Persistence recognition is a two-layer system:

1. built-in language/library recognizers for high-confidence APIs;
2. project configuration for wrappers and organization-specific repositories.

Each recognizer declares:

```text
library or symbol pattern
operation kind: read | write | query | transaction
store identity rule
data-group argument rule
confidence
```

Examples of high-confidence patterns include SQL query execution, file read or
write calls, key-value `Get`/`Put` operations, and ORM repository methods when
the receiver or imported package identifies persistence. Names such as
`ReadConfig` are not sufficient by themselves.

### Store and data-group identity

The store key is the normalized receiver/database/file abstraction plus the
configured store identity where available. The data-group key comes from:

1. explicit schema/table/entity metadata;
2. query/table/resource literals;
3. argument/result type identity;
4. a stable call-site fallback marked approximate.

A read followed by a write of the same data group in one process remains two
movements because the directions differ. Repeated reads of the same group in
one process count once.

### Configuration

Add an optional configuration section:

```toml
[[depth.persistence]]
language = "go"
receiver = "example.com/project/store.Repository"
method = "Get"
kind = "read"
store = "database"
data_group = "return-type"

[[depth.persistence]]
language = "typescript"
module = "./db"
function = "save"
kind = "write"
store = "database"
data_group = "first-argument-type"
```

Configuration must be scoped to the analyzed project and included in the
analysis/profile identity so changing it invalidates cached scores.

### Uncertainty rules

- recognized read/write with known data group: count normally;
- recognized operation with unknown data group: count once with approximate
  key and report reduced confidence;
- ambiguous operation: do not count, report an unavailable/ambiguous fact;
- local variable or in-memory collection access: never count;
- cache access counts only when configured as persistent for this measurement;
- transaction begin/commit/rollback are control operations, not data movements.

### Persistence work estimate

| Task | Estimate |
| --- | ---: |
| Recognizer interface and config validation | 2–3 days |
| Go built-ins and configuration | 2–3 days |
| TypeScript built-ins and configuration | 2–3 days |
| Java and Rust built-ins | 2–4 days |
| Fixtures, cache identity, and false-positive tests | 2 days |

Risk retirement criterion: zero false positives on local assignment fixtures,
and 100% detection of configured recognizers in the conformance corpus. Built-in
recognizers may remain intentionally incomplete; unsupported libraries must be
handled through configuration rather than guessed.

## 3. Information-hiding design

### Objective

Measure interface exposure and responsibility leakage without pretending that
static analysis can perfectly recover a designer's decisions.

The output has three separate components:

```text
representation exposure
multiple-design-decision evidence
extraneous-visible-entity evidence
```

### Representation exposure

High-confidence exposure facts are:

- public mutable field/property;
- public collection whose contents are directly mutable;
- public signature containing a private or implementation-only type;
- public signature exposing a concrete storage type where an opaque type is
  available;
- getter/setter pair that directly exposes one representation field;
- public enum/union/record members that force callers to depend on
  representation details.

Each fact contributes one visible entity, with simple versus structured fields
retained separately for the Rising–Calliss calculation. The adapter must not
flag immutable value types merely because they have public fields unless those
fields expose mutable representation or an explicit configuration says they
are representation-sensitive.

### Responsibility clusters

Construct a graph whose nodes are:

- public operations;
- capability processes;
- internal functions/types;
- persistent stores and external endpoints.

Edges represent calls, shared state use, shared data groups, or ownership of a
capability. Cluster the graph using connected components after removing weak
syntactic edges such as common utility imports.

The multiple-design-decision flag is:

```text
0 if there is one non-trivial responsibility cluster
1 if there are two or more materially independent clusters
unknown if the graph has insufficient evidence
```

“Materially independent” means the clusters have no shared capability process,
data group, persistent store, or internal state. Mere unrelated imports do not
create a second design decision.

This is an evidence-based approximation of the Rising–Calliss binary flag,
not a claim to read the author's intent.

### Extraneous visible entities

An entity is a candidate extraneous entity when it is public but has no edge to
an identified capability process, public data group, or required type contract.

Rules:

- count only candidates with a complete public-boundary inventory;
- do not count an entity when capability extraction is unavailable;
- do not infer that an intentionally extensible/plugin API is extraneous;
- allow project configuration to mark extension points;
- report candidates with evidence and confidence for inspection.

This makes the metric conservative: unknown API purpose reduces evidence
coverage rather than manufacturing a penalty.

### Information-hiding score

For available evidence:

```text
IH = 2 × multiple-design-decision-flag
   + visible-simple-variables
   + visible-structured-variables
   + extraneous-visible-entities
```

`exposedRepresentation` for the leakage penalty is the count of representation
exposure facts, not the complete IH value. This prevents a design-decision flag
from being double-counted as representation leakage.

### Information-hiding work estimate

| Task | Estimate |
| --- | ---: |
| Shared exposure/cluster/unknown fact model | 2–3 days |
| Go visibility, fields, call/state graph | 4–6 days |
| TypeScript checker-backed visibility and types | 4–6 days |
| Java compiler-tree visibility and signatures | 4–6 days |
| Rust visibility and syntax graph | 4–6 days |
| Configuration, fixtures, review output | 3–4 days |

Risk retirement criterion: representation exposure reaches high precision on
the hand-labelled corpus; cluster and extraneous-entity results are treated as
ranked evidence with confidence, not binary truth. A future empirical study
may calibrate their weights, but no universal threshold is assumed here.

## Cross-language rollout strategy

Do not require every language to achieve full evidence before delivering value.
Use capability levels in the report:

```text
Level 0: no public boundary — unavailable
Level 1: operation fallback — current static approximation
Level 2: signatures and type shapes — capability/interface estimate
Level 3: persistence and observable effects — COSMIC-inspired estimate
Level 4: information-hiding graph and exposure evidence — complete v2 evidence
```

The score must include `evidence_level` and `evidence_coverage`. Comparisons
between modules are valid only when their evidence levels and metric definition
match.

Recommended order:

1. shared schema and TypeScript/Go Level 2;
2. persistence recognizers and configuration;
3. Java/Rust parity;
4. information-hiding exposure and graph evidence;
5. cross-language conformance and default-score migration.

## Go/no-go decisions

Proceed with full v2 implementation if:

- the project accepts an evidence-based approximation rather than exact
  COSMIC sizing;
- unsupported libraries can be configured;
- unknown evidence is visible and does not become zero;
- score comparisons are gated by definition/evidence level;
- the hand-labelled corpus confirms ordering invariants.

Do not proceed with automatic full scoring if the requirement becomes “infer
the author's exact design decisions and business capabilities from arbitrary
source code”. That requires requirements, architecture metadata, or human
annotations and should be a separate assisted-measurement feature.
