# Functional module depth design

Status: approved design; Go/TypeScript signature and representation evidence implemented

Definition version: `ousterhout-v3`

## Purpose

`SHALLOW` is a penalty for a module whose callers must understand a large or
awkward interface to obtain relatively little useful functionality. It is the
inverse presentation of module depth:

```text
depth = useful capability provided / caller-visible interface cost
SHALLOW = a bounded penalty derived from depth and interface leakage
```

Higher `SHALLOW` values are worse, as with the other Slopslap measures. The
metric is an engineering proxy for Ousterhout's deep-module principle; it is
not a claim that Ousterhout specified this exact numeric formula.

This definition deliberately does not use LOC, cyclomatic complexity, NPath,
private-method count, or implementation effort as the numerator. Adding 5,000
lines of unnecessary implementation must not make a module appear deeper.

## Conceptual basis

Ousterhout describes a deep module as one that hides substantial functionality
behind a relatively simple interface. His treatment is a design model rather
than a standardized measurement formula.

Rising and Calliss provide the closest directly relevant module-level
measurement precedent found for this purpose. Their original information
hiding indicator was:

```text
number of design decisions + number of extraneous visible entities
```

Their revised Ada-package metric was:

```text
IH = 2 × multiple-design-decision-flag
   + visible-simple-variables
   + visible-structured-variables
```

The design-decision flag is zero when one design decision is hidden and one
when more than one is hidden. They reported this as an indicator of likely
change, not as a precise universal measure of maintainability.

The new metric uses that work as an interface-hiding component, while using a
COSMIC-inspired functional-size component for useful capability. COSMIC
defines functional size as the number of data movements in a defined scope:
Entry, Exit, Read, and Write. COSMIC measurements are based on functional user
requirements and an explicitly defined software boundary; this design follows
that discipline but uses static source inference, so the result is described
as COSMIC-inspired rather than COSMIC-compliant.

References:

- Ousterhout, *A Philosophy of Software Design*, discussion of deep modules:
  [sample chapter](https://ptgmedia.pearsoncmg.com/images/9780137353484/samplepages/9780137353484_Sample.pdf).
- Rising and Calliss, *An Indicator of Information Hiding*, PNSQC 1992:
  [paper PDF](https://www.pnsqc.org/archives/uploads/proceedings/pnsqc1992.pdf).
- COSMIC, [measurement process](https://cosmic-sizing.org/cosmic-sizing/intro/measurement-process/)
  and [glossary](https://cosmic-sizing.org/cosmic-sizing/cosmic-glossary/).

## Measurement subject and boundary

The subject is one analyzed source file, matching the current report model.
The file is the module boundary for v2. A file containing several language
module declarations produces one aggregate observation.

The module's functional users are code or systems outside that file that call
its public API. Internal helper calls are not functional users. A capability
is counted only when it is attributable to an externally usable operation or
an externally observable interaction; internal helper decomposition does not
create additional capability.

If an adapter cannot identify a public boundary or cannot produce the required
capability evidence, v2 is unavailable for that subject. It must not emit zero
capability and accidentally classify the module as maximally shallow.

## Numerator: functional capability `F`

For every externally meaningful functional process owned by the module, count
its distinct data movements:

```text
F = E + X + R + W
```

Where:

- `E` is an Entry of a data group from a functional user into the process;
- `X` is an Exit of a data group from the process to a functional user;
- `R` is a read of a data group from persistent storage; and
- `W` is a write of a data group to persistent storage.

The following rules prevent implementation detail from inflating `F`:

1. A public operation invocation is not automatically one COSMIC movement.
   The adapter must identify the data group or externally meaningful event it
   accepts. If it cannot do so, it may use the operation-level fallback below,
   but must mark the result as approximate.
2. Calls between private helpers are never `E`, `X`, `R`, or `W`.
3. Calls to another module count as an Exit and/or Entry only when they are an
   externally meaningful interaction in the module's functional process, not
   merely a helper-level implementation call.
4. A persistent-store read or write counts only when the adapter can identify
   the operation as persistent storage access. Ordinary local variable reads
   and writes do not count.
5. Repeated execution of one movement is counted once. Distinct data groups or
   distinct object-of-interest keys are separate movements.
6. A capability must be assigned to exactly one owning module at the selected
   boundary. The same movement must not be counted in both caller and callee.

### Static fallback

Most source adapters cannot recover full functional requirements. Until an
adapter supports data-group inference, it may emit an explicitly approximate
capability count:

```text
E = number of externally callable operations with input or triggering event
X = number of externally callable operations with a meaningful result/output
R = recognized persistent reads in owned public processes
W = recognized persistent writes in owned public processes
```

The fallback must be recorded in measurement attributes as
`capability_basis: "static-approximation"`. A full data-group measurement is
recorded as `capability_basis: "cosmic-inspired"`.

An operation with no parameters may still have an Entry if its invocation is
the triggering event. An operation with no return value may still have an Exit
if it emits, mutates an externally visible resource, or otherwise produces an
observable result. The adapter must use language-specific evidence for those
cases; it must not infer them from the method name alone.

## Denominator: interface cost `I`

Interface cost models what a caller must learn, construct, and provide:

```text
I = API surface cost + type cost + exposed-state cost + IH cost
```

### API surface cost

For each externally callable operation:

```text
operation cost = 1
```

The operation is counted once regardless of body size. Each overload is a
separate operation because it is a separate choice callers must understand.

For each public field/property/constant, add one exposed-state unit. A public
type declaration is counted once when it is part of the public boundary.

### Type cost

For every public parameter and return type, add `typeComplexity(type)`.
Type complexity is structural and bounded:

```text
typeComplexity(opaque named type) = 1
typeComplexity(primitive type)    = 1
typeComplexity(T)                 = 1 + sum(child costs)
```

The recursive cases are:

- pointer, reference, optional, or nullable wrapper: one child;
- array, slice, or collection: one element child;
- map/dictionary: key child plus value child;
- tuple/record/object: each exposed member child;
- function/callback type: each parameter child plus result child;
- union/intersection/generic instantiation: each constituent or argument
  child.

Recursive and repeated type references are memoized and contribute their
already-computed cost once per occurrence, with a recursion back-edge costing
one. A type's private representation is not traversed unless it is exposed in
the public signature. The result is capped at `32` per signature type to stop a
pathological type graph dominating the module score.

### Rising–Calliss information-hiding cost

Let:

```text
IH = 2 × multiple-design-decision-flag
   + visible-simple-variables
   + visible-structured-variables
   + extraneous-visible-entities
```

The first three terms follow the revised Rising–Calliss metric. The additional
extraneous-entity term preserves the key idea from their original metric.

An entity is extraneous when it is public but is not required by any other
public operation's functional process or by the module's declared capability
boundary. Static analysis cannot prove this in every language; when it cannot,
the adapter omits the term rather than guessing. The score records which
terms were available.

The multiple-design-decision flag is one when the module has evidence of two
or more unrelated responsibility clusters. Evidence may come from explicit
module declarations, disconnected capability/reference clusters, or an
adapter-specific design-decision fact. It is zero when the adapter finds one
cluster. If the adapter cannot assess it, the term is unavailable and its
weight is omitted.

## Depth and penalty conversion

The raw depth ratio is:

```text
D = F / I
```

`F` and `I` are positive real values after weighting. `D` is retained in the
measurement attributes for diagnosis; it is not displayed as the user-facing
SHALLOW score.

To avoid arbitrary claims that a universal ratio is “good” or “bad”, convert
the ratio to a bounded penalty using a versioned reference depth `D_ref`:

```text
depthPenalty = 100 × max(0, 1 - D / D_ref)
```

v3 selects `D_ref` from normalized interface shape. The fast-path policy uses
priors of `0.72` for protocols, `1.12` for commands, `1.05` for queries, and
`0.85` for data surfaces; general and unknown surfaces use `1.0`. Each prior
is blended toward `1.0` by classifier confidence and surface size, capped at
four normalized public items. The policy is reported as
`reference_policy: role-shape-v2` and is a versioned heuristic, not a claim
of empirical calibration.

Apply information-hiding leakage as a separate penalty:

```text
leakagePenalty = 100 × min(1, exposedRepresentation / max(1, I))
```

The reported value is:

```text
SHALLOW = round(clamp(0, 100,
    0.80 × depthPenalty + 0.20 × leakagePenalty))
```

The 80/20 split ensures that the metric remains primarily about capability
per interface burden while still making direct representation leakage visible.
The raw `IH` value and all available components are reported so the score is
auditable. These constants are part of `ousterhout-v3`; changing them requires
a new definition version.

If capability or interface cost is unavailable, the measurement is unavailable
with a reason. If only the optional leakage evidence is unavailable, calculate
the result with the available weights and report
`available_weight`/`total_weight`.

## Examples

Suppose two modules each provide `F = 20` capability points:

```text
Module A: I = 4,  D = 5.0
Module B: I = 20, D = 1.0
```

Module A is deeper because it delivers the same functional capability through
one fifth of the interface burden. Adding unnecessary implementation code to
either module does not change `F`, `I`, or `D`.

## Reporting contract

Emit one `module_shallowness/ousterhout-v3` observation per report file with:

```text
value: SHALLOW
subject: module/file
attributes:
  functionality
  entries
  exits
  reads
  writes
  capability_basis
  interface_cost
  operation_cost
  type_cost
  exposed_state_cost
  information_hiding_cost
  exposed_representation
  raw_depth_ratio
  depth_reference
  reference_role
  role_confidence
  role_basis
  reference_policy
  reference_basis
  depth_penalty
  leakage_penalty
  available_weight
  total_weight
```

The detail view should explain the final penalty and show the raw ratio and
its two penalty components. The overview should show only the final SHALLOW
value.

## Non-goals

This metric does not claim to measure:

- implementation quality or correctness;
- performance, resource use, or runtime complexity;
- cohesion independently of the capability/interface relationship;
- the absolute business value of a module;
- standard COSMIC functional size without a requirements-level measurement.

Those concerns remain separate Slopslap measures or evidence sources.

## Compatibility

The former `ousterhout-v1` implementation used implementation statements,
decisions, NPath, and exported-declaration counts. It was not silently
relabelled as v2. The current v2 implementation uses signature-level evidence
for Go, Java, Rust, and TypeScript and reports `evidence_level: 2` with
`capability_basis: "cosmic-inspired-signature"`. It also reports mutable public
fields/properties and direct accessor exposure as representation leakage.
Data-group inference and persistence remain later stages.
