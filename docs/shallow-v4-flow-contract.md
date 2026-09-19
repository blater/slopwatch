# SHALLOW v4 normalized flow contract

Revision: `flow-v1-r6`. Normative companion to the
[implementation specification](shallow-v4-implementation-spec.md).
The source examples and expected slices are in [flow-cases.json](evidence/shallow-v4/flow-cases.json).

## 1. Transport and identities

Each artifact carries functions, types, creation routes, public routes and source
provenance. A function has canonical symbol/signature ID, formal slots, receiver,
entry block, blocks and exits. A block has ordered instructions and successor edges.
Edges are `normal`, `true`, `false`, `throw(error_tag)`, `return_error(error_tag)`,
`panic`, `cleanup` or `suspend_resume`. Each instruction records ID, opcode, operands,
result IDs, resolved type, roots/effects and provenance. Expression trees alone
cannot replace exceptional/control-flow edges.

Supported opcodes: `constant`, `bind`, `phi`, `primitive`, `field_read`, `field_write`,
`allocate`, `pack`, `call`, `select_target`, `branch`, `return`, `throw`, `acquire`,
`use_resource`, `cleanup_attempt`, `lock`, `unlock`, `atomic_rmw`, `escape`, `unknown`.
Calls identify exact candidate symbols and formal-to-actual bindings, normal result
bindings, error continuations and ownership effects. Language adapters lower their
control constructs to these operations; the shared transfer rules derive obligations.

A value is `(origin expression DAG, type/concepts, may_roots, constant-or-top)`.
An alias root is receiver/static field, formal slot, allocation-site, trusted resource
token, or unknown; it has a field path, mutability and ownership state. Root IDs use
qualified symbols or normalized allocation/producer expressions, not file positions.
Private helper boundaries and local names are erased when hashing outcome DAGs;
formal placeholders retain normalized type/position. Moving source locations must
not change evidence identity. Distinct declared state fields remain distinct roots.

Provenance is a set of `(artifact, source path, source span, rule ID, input fact IDs)`.
Multiple locations can support one interned obligation. Store provenance separately
from semantic identity. Emit at most 8 representative spans per fact (canonical
path/span order), with `omitted_location_count`; location truncation does not change
knowledge because the complete semantic evidence and dependency IDs remain stored.
Opaque semantic evidence is never dropped under the guise of truncating display.

Loop-carried values use canonical recurrence definitions/references as specified in
[adapter contract section 2](shallow-v4-adapter-contract.md#2-common-lowering-invariants).
The expression DAG must not expand loop iterations during fixed-point computation.
In transport, `Recurrence.PhiSlot` names the loop-header phi's SSA result, not
the instruction ID. That phi's predecessor/value bindings are authoritative:
entry edges supply initial values and dominated backedges supply updates.
An optional `Definition` asserts one common update SSA value on every backedge;
leave it empty when different latches supply different values. `References` names
other recurrence IDs (including self-reference), not expression text. These links
do not replace deriving data dependencies from the actual value graph. Reject
contradictory descriptors locally without discarding unrelated valid functions.
Standard Go error tuples and Rust Result/Option packing carry tagged success/error
values; packing alone earns no X and does not create a second delegated service.
Typed Rust i32 wrapping_add/wrapping_mul lower as primitive arithmetic with wrapping
mode, only after resolving the standard primitive receiver. Standard error objects
and buffer constructors use the explicit registry entries, not name guesses.

### Implemented immutable Java carrier lowering

`CreationFacts.passive_accessors` optionally names the complete set of proven
identity-getter routes. It does not remove their public signatures or route burden.
The passive-creation rule requires exact agreement with the route inventory and no
independent behavior, failures, lifecycle burden or leakage before granting N/A.

For the supported final scalar carrier with constructor rejection, construction
instead returns a normalized tuple of the stored scalar values. Storage/packing
earns no X; the existing guarded-flow proof determines validation responsibility.
`initial_fields` records each canonical field and its constructor-qualified SSA
value. Rejection metadata prevents the passive N/A exemption. All constructors
must pass the bounded structural check, and every stored field must have a visible
exact getter so these payload values are observable.

Each such getter is an identity flow over one implicit scalar field formal. This
formal is not a caller argument and adds no route slot or burden. This lowering is
restricted to the proven getter-only surface; it is not a general receiver model
and cannot be reused for method calls without binding the receiver field. Mutable
fields, computed getters, additional methods and unsupported constructor bodies
remain outside this implementation. Public API signatures remain authoritative for
inventory fingerprints and fix comparisons.

## 2. Finite abstract domain

| Element | Limit and widening |
| --- | --- |
| Access path | 4 field/index steps; deeper path becomes `prefix.*` with unknown alias precision. Dynamic index is `[*]`, not a fabricated exact element. |
| Alias set | 16 roots per value; overflow becomes UnknownAlias. |
| Resolved receiver set | 16 symbols; overflow becomes UnknownDispatch. |
| Obligation alternatives | 32 distinct canonical ID sets at a join/composition; overflow becomes UnknownAlternatives. |
| Facts in a function summary | 256 semantic facts, including root/effect/binding relations; overflow becomes UnknownSummary for the affected dimension. |
| Binding instantiations | 32 unique normalized actual-root tuples per callsite; overflow joins to unknown binding, never creates longer call strings. |
| Context | Parameterized, context-insensitive summary; no call-stack strings. Bind formal roots at callsites with the bounded tuple domain. |
| Expression representation | Interned DAG; max 256 nodes in one outcome recipe; overflow is unknown transformation identity, not a fresh extra X. |

Absence is known only after processing the complete relevant supported graph.
Joins union may-alias/target sets, intersect must-ownership/guard guarantees, and
retain obligation alternative sets as described below. Precision can only decrease:
Once widened to an unknown element in a pass, a later visit cannot regain exactness.
Use canonical ID ordering for transfers/joins and sorted SCC worklists. Unknown
alias/target/binding precision propagates only to dimensions that depend on it;
if essential to scoring, numeric eligibility is partial. Never replace unknown with
an empty set. Revisit count and scheduling cannot choose which facts survive.

Recursive calls reuse parameterized SCC summaries. Newly discovered paths/bindings
join into the bounded domain; no string substitution can extend paths past four
steps. Stop on fixed point or deterministic element-visit limit. Recursion itself
is neither an obligation nor evidence that behavior is absent.

## 3. Transfers, aliasing and ownership

- `bind` preserves origins/aliases. `phi` joins predecessors and records edge guards.
- Primitive values and immutable string results have no mutable alias root. A copy
  of an object reference preserves its roots. `pack` maps field bindings; it is not
  X by itself. A proven deep-enough defensive copy introduces fresh storage for the
  copied shape only. Nested uncopied references remain aliases.
- `field_read/write` use the resolved declared field identity plus receiver roots.
  Strong update is allowed only for a singleton, nonescaped owned root. Otherwise
  weakly join; do not erase a possible alias or earlier effect.
- `allocate` produces a site root and initialization bindings. Allocation alone
  contributes no obligation. Repeated executions share the abstract site; do not
  assume concrete instances are identical or disjoint when that distinction matters.
- A measured data-only creation boundary may include adapter-proven read-only
  identity accessors over immutable primitive or string fields. The complete
  accessor route set remains in the inventory; missing, extra or unknown routes
  prevent passive applicability.
- Ownership states are `borrowed`, `owned`, `moved`, `escaped`, `unknown`; resource
  states are `not_acquired`, `live`, `cleanup_attempted`, `transferred`, `unknown`.
  Join disagreements explicitly. Rust move changes ownership, not alias identity.
  JS/Java assignment and Go pointers do not magically transfer exclusive ownership.
- Returning/storing a live owned resource outside the boundary transfers it and
  leaves the caller lifecycle relation. A borrowed input buffer is not owned backing
  state. Returning an owned mutable reference produces L unless copying/read-only
  semantics and absence of writable aliases are established.
- A summarized call binds formals to actual roots and substitutes returned alias
  relations; its effects follow the registry or resolved body. An unsummarized call
  may mutate/retain mutable inputs and receiver, throw and return unknown aliases.
  Mark those relevant dimensions unknown. Do not apply this to unrelated inaccessible
  private roots without a possible alias, but unknown calls may still affect failures.

## 4. Exceptional exits and cleanup

Model source-level normal return, ordinary throw/panic and explicit error-result
paths. Treat process termination, OOM and external kill as runtime analysis/worker
failures, not ordinary branches granting/denying source-level responsibilities.
An entry with no successful acquisition owes no cleanup for that attempted resource.

- Java `finally` and try-with-resources insert cleanup edges from every supported
  normal/exceptional exit after acquisition, in reverse acquisition order. Keep
  cleanup failures, suppression/overriding and rethrow edges explicit.
- Go `defer` records receiver/argument bindings at registration, then runs in reverse
  order on return and supported panic unwinding. A defer before successful acquisition
  or with unknown nil receiver behavior cannot prove R. A direct error return is an
  exit; a ignored cleanup error is recorded, not translated into guaranteed success.
- Rust scoped Drop for a trusted resource/guard runs on normal return, `?` and
  configured unwind paths. A moved/returned handle transfers the obligation. Unknown
  custom Drop is not a standard close. Honor supplied panic strategy; abort has no
  unwinding guarantee and is an abrupt termination, not a successful completed route.
- TypeScript `try/finally` carries both resolution/rejection edges. An awaited close
  is a cleanup attempt before completion. An unawaited promise does not establish R.
  `await` is a suspension edge, not a lock or proof of race freedom.

R establishes a matching cleanup attempt on all supported relevant exits, not that
I/O release can never fail. Y establishes guard coverage/release attempts for all
relevant accesses, not absence of every race in arbitrary code. An unsupported exit
or ownership escape makes the affected guarantee unknown, not vacuously true.

Error-result values use the resolved `error_result` kind with correlated presence
and opaque payload recipes. `error_present` reads only that presence relation;
unknown presence is not truthy/falsy data. Physical return tuples retain the error
slot on both success and failure, including nil success. Return control partitions
the tuple into normal and `return_error` completions. A proven failure may also
carry `completion_kind: "return_error"` on `OpReturn`; other instruction/completion
kind combinations are rejected by the compiler.

An error completion is callee outcome data, not an exception automatically thrown
into its caller. Calls bind the whole returned tuple. Caller tests, forwarding,
fallbacks or overwrites determine whether a rejection remains visible. Error
messages and passive error allocation do not independently earn transformation
credit. Boundary proofs omit the successful error slot from X; an error-only
validator can still establish V through its normal-success completion.

Scalar multi-result functions carry ordered `results` descriptors with stable IDs,
resolved types and value kinds. Calls bind each descriptor to its destination SSA
ID through ordered `result_bindings`; all result positions and types are checked
before any destination is committed. Result tuples share one correlated completion
guard. Error arguments, arbitrary error implementations and typed-nil pointer
conversions require explicit support; unsupported cases remain partial.

## 5. Alternatives and category-specific joins

Alternatives represent distinct **normal service behaviors**, not an enumeration
of each instruction trace. Collapse identical obligation sets. Explicit conditional
implementation selection and finite virtual/interface target selection use the
same alternative construction. Correlation known from a local predicate is retained;
unknown correlations may add conservative feasible alternatives, never remove them.

- V is derived from a guard controlling the affected normal outcome and a supported
  rejection/translation exit. The rejection exit is evidence for V, not a second
  empty successful implementation. If a public bypass or normal branch avoids the
  guard, V is conditional in the combined family.
- R/Y are universal proofs over their relevant exits/accesses. A cleanup/guard on
  only one branch cannot become guaranteed by unioning facts from another branch.
- C/X attach to the normal alternatives establishing the state/transform relation.
  A C alternative and an X alternative retain `{C}` and `{X}`; their guaranteed
  category intersection is empty, but minimum weighted capability is 2.
- At boundary composition, union obligation identities across each combination of
  family alternatives, deduplicate and then take minimum weight. Do not sum each
  family's minimum independently: overlapping obligations would be counted twice.

No numeric result if unknown alternatives or the 32-set cap affect required H.
The source/flow suite must include conditional-versus-receiver equivalence,
validated/bypass routes, error-only rejection paths and alternative overlap between
families. Fixtures compare intermediate roots/edges/obligation IDs, not scores alone.

## 6. Deterministic cost and retained-resource limits

A logical work unit is one visit to a normalized node, edge, alias element, binding
pair, obligation ID or alternative-set member. Processing a set costs its cardinality,
not one cheap “worklist update”. Charge interning/hashing by input element count.
Record summary `logical_cost`, source graph counts and instantiation dependencies.
Cache hits restore those logical costs for eligibility even when physical execution
is zero. Run the same canonical admission traversal cold and warm.

| Limit | Action |
| --- | --- |
| Per boundary: 2,000 reachable functions, 50,000 flow nodes, 1,000,000 element visits | Affected boundary partial with named limit. |
| Per artifact: 20,000,000 unique-summary/instantiation element visits per source revision | Admit canonical sorted roots; remaining unassessed roots partial with artifact-work reason. Cached costs are charged identically. |
| Per function artifact: 256 facts / 64 KiB canonical semantic payload | UnknownSummary; never serialize an unbounded binding/effect list. |
| Per artifact: 250,000 unique facts / 64 MiB canonical semantic payload | Deterministic partial remainder under sorted admission, independent of cache/scheduling. |
| Runtime resident summary arena: 128 MiB; provenance arena: 32 MiB | Spill completed immutable summaries; do not discard semantic facts. Count backing array/map capacities and string bytes with a conservative 2x metadata charge. |
| At most two summary workers; coordinator instantiation queue <=256 requests | Apply backpressure; no queued copies of complete helper graphs. |

The resident arenas are allocation-accounting limits for new semantic state, not a
claim to cap an entire VM's RSS. Existing parser processes still count in the whole
process-tree benchmark gate. A failure to spill/read or an OS/resource cancellation
is transient partial evidence, never a reusable deterministic limit result.
Source/semantic payload limits, unlike residency, apply even if artifacts are spilled.
All limits and domain versions are profile inputs.

Intern roots, recipes, obligations and proof DAGs once per artifact. Consumers store
IDs, not copied transitive evidence. Memoize SCC summaries and normalized binding
tuples. Reference-count or release inactive artifact arenas between units; retain
only bounded indexes in the dashboard. Unit dependencies persist independently of
in-memory residency. Repeated consumers must not recursively expand a shared DAG.

## 7. Release benchmark and follow-up scale fixtures

The release benchmark is `~/src/river`, as approved by the user on 2026-09-17;
the runner and acceptance gates are specified in implementation spec §8. Retain
deterministic tests of shared work, caps, scheduling and cache equivalence in this
release. The large generated performance workloads below are follow-up capacity
validation, not release gates, and River results do not establish their performance.

For that follow-up, generate deterministic source with stable seed 1 and publish
the manifest. In addition to the 30K independent-
module fixture (3,000 units of ten files, one ten-unit chain), run these shapes:

1. Fan-in: 10,000 public roots reference the same 100-function helper DAG.
2. Diamonds: 12 layers, two branches per layer reconverging to shared summaries;
   1,000 callers. Do not expand its exponentially many syntactic paths.
3. Deep chain: 2,001 local functions; verify the boundary cap is identical cold/warm.
4. SCCs: 100 SCCs of 20 functions, including recursive field-path growth and alias
   growth past domain caps; each SCC is referenced by 100 public roots.
5. Wide binding: a value with 17 possible roots and a function with 257 semantic
   facts; verify deterministic widening and limit reasons, not arbitrary truncation.

On fan-in/diamonds/SCCs, physical summary construction is at most once per unchanged
function per pass; unique normalized binding instantiations are memoized. Logical
charges remain as specified. Doubling only public consumers with the same bindings
must add at most 2.2x consumer bookkeeping work, not recompute the helper DAG.
During follow-up capacity validation, apply the cold/warm process-memory gates to
every numeric-capable shape, and require
all shapes to finish or emit their planned partials within the same 120-second p95
ceiling. Tests for caps compare states, IDs and reasons across cold/warm cache,
reversed scheduling and two worker counts; no wall-clock timing assertions in unit tests.
