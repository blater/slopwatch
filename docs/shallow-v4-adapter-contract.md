# SHALLOW v4 adapter implementation and conformance contract

Status: r6 implementation-detail supplement; required for all four adapters.
This elaborates the [specification](shallow-v4-implementation-spec.md), without
changing its scoring weights. Its hash is part of the policy fingerprint.

## 1. Adapter boundary and delivery

Adapters extract typed, resolved flow and public contracts. The shared engine owns
transfers, joins, obligation recognition, limits and scoring. A separate TypeScript
scorer must pass the same fixtures; it must not introduce its own interpretation.
Existing complexity/expression walkers do not supply enough information to infer
ownership or cleanup: add dedicated depth extraction beside them.

| Adapter | Existing integration points | New responsibility |
| --- | --- | --- |
| Go | `analyzers/structural/internal/goadapter/adapter.go`, `syntax.go`, surface collectors | `depth_surface.go`: effective API and type bindings; `depth_flow.go`: typed CFG and call binding. Use go/types object and selection identities, not spelling. |
| Java | `analyzers/structural/adapters/java/src/dev/slopslap/structural/JavaParser.java`, `JavaClassFacts.java`, `JavaMethodBodyFacts.java`, `JavaExpressions.java`, `Facts.java` | `JavaDepthFacts.java` plus focused surface/flow helpers: attributed Trees/Elements/Types, symbols, CFG and exception continuations; serialize new flow facts. |
| Rust | `analyzers/structural/adapters/rust/src/model.rs`, `parser.rs`, `surface.rs`, `control.rs`, `expression.rs` | `depth.rs` plus resolver/flow helpers: syn syntax plus bounded source-local symbol/type/ownership resolution. Syntax alone is not proof of a trait implementation or standard-library call. |
| TypeScript | `analyzers/typescript/src/model.ts`, `context.ts`, `operations.ts`, `surface.ts`, `structural.ts` | `depth.ts`, `depth_flow.ts`, `depth_score.ts`: checker-backed exports, value flow and common policy. Reuse the configured compiler graph even with type-safety metrics disabled. |

Each adapter delivers surface extraction, flow extraction, registry binding,
source→flow→fact tests, unsupported-case tests and dependency invalidation tests.
Do not postpone one of these languages to a later release. Shared schema and
fixture runner precede adapter implementation; independent adapter work can then
proceed against that frozen contract.

## 2. Common lowering invariants

Every expression has evaluation order, value bindings and explicit continuations.
A statement visitor that merely collects calls cannot establish these properties.

| Construct | Required normalized behavior |
| --- | --- |
| Literals, parentheses, local bindings | `constant`/`bind`; preserve resolved type, signedness/arithmetic mode and alias identity. Parentheses introduce no capability. |
| Unary/binary arithmetic and comparisons | Typed `primitive`; division/indexing/overflow failures follow supplied language mode. User overloads/coercions are calls, never primitive assumptions. |
| Short circuit, conditional, nullish/default evaluation | Branch with only evaluated operands on each edge, join with `phi`. Do not eagerly evaluate skipped calls or cleanup. |
| Assignment, destructuring, update | Evaluate operands once in language order, then bind/read/write. Retain old/new values for postfix/prefix and multiple assignment. |
| Field/index access | Qualified root/path, dynamic index wildcard; distinguish value copy from reference alias. Unknown getters/indexers remain calls/effects. |
| Sequence, return and abrupt exit | Ordered basic blocks, explicit result/error payload, no fall-through after exit. Preserve cleanup obligations. |
| Loops, break and continue | Entry/test/body/latch/exit blocks; labeled targets where legal; fixed-point joins. Zero iterations remain feasible unless disproven. No source unrolling. |
| Switch/match | Evaluate selector once, preserve guards, ordered tests and fall-through where the language permits it; join normal alternatives. |
| Function/method call | Exact finite targets, receiver and actual/formal bindings, normal/error continuations. Bind captures for local closures. Missing target or relevant binding is partial. |
| Construction | Evaluate initializers in specified order, `allocate`/`pack` and field bindings; resolve explicit/synthetic creation routes and constructor delegation. Allocation alone earns no X. |
| Recursion | Parameterized SCC summary and finite root substitutions; no call strings or recursive AST expansion. |
| Unsupported construct | Emit located unknown fact for affected dimensions; retain supported observations and unrelated files. Never silently skip it or turn it into an empty body. |

Loop-carried values use canonical recurrence references keyed by the loop header
and phi slot. Store an acyclic recipe containing references to those definitions;
do not expand a recurrence on each worklist visit. References are internal semantic
IDs derived from normalized control structure, not line numbers. Local pure numeric
reductions with resolved primitive operators produce one X for the returned outcome.
They do not produce one X per iteration/operator. Unknown loop calls or alias writes
remain unknown; recognizing a recurrence does not prove termination or correctness.
The 256-node recipe limit counts recurrence definitions and references once each.
Tests must include a numeric accumulator loop that stabilizes below the cap.

## 3. Go lowering

| Go syntax / resolution | Required handling and tests |
| --- | --- |
| `ast.FuncDecl`, `TypeSpec`, methods and selections | Resolve package objects, exported access, pointer/value method sets and promoted methods. Unexported types returned by exported APIs retain externally accessible methods. Ambiguous promotion is not an invented method. |
| `CompositeLit`, `new`, zero values | Record legal external creation routes; unexported fields restrict literals but do not imply an unconstructible exported zero-value type. `make` creates typed slice/map/channel storage with relevant failure effects, no X by allocation. |
| `AssignStmt`, `IncDecStmt`, `ValueSpec` | Multiple RHS values are bound before LHS writes. `:=` uses object identity to distinguish shadowing. Model tuple/error results separately. |
| `IfStmt`, `ForStmt`, `RangeStmt`, `SwitchStmt`, `TypeSwitchStmt` | Scope init bindings, explicit branches/loop joins, break/continue/fallthrough. Array/slice/string range is required. Map iteration order is unspecified; do not prove ordering. Channel range/concurrency is partial unless source behavior is resolved. |
| `CallExpr`, `SelectorExpr`, `FuncLit` | Direct functions, local closures and finite interface targets from assignments; capture current alias roots. Caller-supplied open interfaces are unknown, not all implementers in the repository. |
| `ReturnStmt`, error tuples, `panic`, `defer` | Bind deferred callee/arguments at registration, run LIFO on normal return and supported panic. Deferred writes to named results affect returned values. Test early error before acquisition, panic after acquisition and return after defer. |
| `GoStmt`, `SelectStmt`, `recover`, reflection/unsafe | Emit specific partial ordering/effect reasons for unsupported concurrent/interception/dynamic semantics. Other units still complete. Do not treat an unknown goroutine as an irrelevant call. |
| Imports, generics, build selection | Supplied package/export data only; concrete supported substitutions preserve identity. Unresolved type-parameter operations or missing imports are partial. Cache selected files/build facts and imported summaries. |

Go has no optional parameters or method overloads. Represent a defaulted API by
source-resolved configurable/default wrapper functions. Required pointer-to-bool
parameters are still required slots; nullable is not optional. Record/parameter
packaging parity uses equivalent required fields, not fabricated Go defaults.

## 4. Java lowering

| javac tree / resolution | Required handling and tests |
| --- | --- |
| Class/method/variable trees and attributed symbols | Effective enclosing visibility, inherited public members of nonpublic bases, overload signatures and override precedence. Exclude bridge/alias duplication. Missing attribution is located partial evidence. |
| `NewClassTree`, constructors, initializers | Explicit/synthetic default constructors, accessible creation, `this`/`super` delegation, field/instance initializer order. No synthetic public constructor for interfaces or classes with inaccessible creation. Passive records earn no X for packing. |
| `IdentifierTree`, `MemberSelectTree`, assignments, array access | Resolve declared fields and actual receiver roots; distinguish arrays/reference copies from primitive values. Include null/bounds failures where relevant to cleanup. |
| Binary/unary/conditional trees, loops, switch | Primitive evaluation modes, short-circuit branches, accumulator recurrences, statement/expression switch joins and yield. Unsupported patterns are partial, not discarded cases. |
| Method invocation, lambda and member reference | Exact overload and receiver bindings; finite receivers from local construction/assignment. Source-resolved lambda captures. Open virtual dispatch and unavailable generated members remain explicit unknowns. |
| `TryTree`, catches, finally, throw | Exception continuations, first compatible catch where known, finally on every exit including return/throw. A finally return/throw overrides pending completion. Try-with-resources closes in reverse order, including earlier resources when later acquisition fails. |
| Synchronized block/method | Monitor identity, balanced release on exceptional paths, relation to protected field accesses. Static methods use the class monitor. A lock on an unrelated object earns no Y. |
| Records, sealed hierarchies, enums, annotation processing | Explicit source members and compiler-provided language constructors are usable. A sealed declaration alone does not prove the actual receiver set. Never execute annotation processors; Lombok-dependent gaps retain specific reasons. |

Test both `finally` and try-with-resources, including close failure/suppression and
a return inside the try. Java overload defaults must resolve to the same delegated
service; same-name unrelated overloads remain distinct families.

## 5. Rust lowering

| syn syntax / source resolution | Required handling and tests |
| --- | --- |
| Items, `use`, modules, impls and visibility | Crate/module symbol index, renamed re-exports, `pub(crate)`/restricted audience, inherent methods and finite locally resolved trait impls. No rustc/build/macro execution requirement. |
| `ExprStruct`, tuple constructors, associated factories | Accessible fields/creation and bindings; private fields block external literals. `Default` is not universally synthesized: only an explicitly resolved implementation provides a route. |
| `ExprPath`, `ExprField`, `ExprIndex`, references and assignments | Track moves, borrows, dereferences and storage roots; Copy only for proven primitive/structural Copy values. Source-local primitive field mutation is required. Unknown overloaded Deref/Index/operator behavior is partial. |
| `ExprIf`, `ExprMatch`, loops, blocks | Pattern bindings and guards, block tail result, break value, loop phi recurrences. Preserve exhaustiveness knowledge and unknown patterns. Typed `i32::wrapping_add`/`wrapping_mul` are fixed primitive intrinsics; do not generalize method names. |
| `ExprCall`, `ExprMethodCall`, closures | Local type propagation plus exact receiver/impl identity. Concrete local trait objects and function pointers are finite targets; externally supplied trait objects remain open. Bound closure captures preserve move/borrow effects. |
| `ExprTry`, return, `Result`/`Option` constructors | `?` splits success/error and schedules in-scope cleanup before propagation. Standard enum packing has no X. Custom Try/conversion behavior is partial unless resolved. |
| Scope exit and Drop | Reverse lifetime cleanup for owned trusted handles/guards, including early return and supplied unwind mode. Moving/returning a handle transfers ownership; leaked/forgotten handles cannot get R. Unknown custom Drop invalidates relevant cleanup/effects. |
| Macros, cfg, async, unsafe | Match only explicitly registered standard macros. Supplied cfg selection is required; unsupported expansion, async state machines/cancellation and unsafe alias effects receive specific partial reasons. Ordinary unaffected functions must remain measurable. |

Rust has no implicit default constructor or optional parameter syntax. Literal versus
associated factory is the creation-equivalence case. Default wrapper functions are
the overload analogue. Do not demand a Java-specific syntax fixture from Rust or
weaken the equivalent behavioral assertion.

## 6. TypeScript lowering

| Compiler node / resolution | Required handling and tests |
| --- | --- |
| Source/module exports, declarations and checker symbols | Default/named exports, aliases, local barrels, callable objects, class inheritance and effective access. `.d.ts` files supply context only; missing declarations remain a coverage reason. |
| Function-like nodes, parameters and binding patterns | Optional/default parameter presence, record leaves, destructuring defaults and rest collections. Evaluate defaults only when triggered. One rest collection is one slot, not an estimated argument count. |
| New/object/array expressions, class fields | Exact constructor/factory and inherited default constructor bindings; object literals pack fields. Getters, spread iterators and computed properties are effectful unless resolved. `private`/`#private` do not make returned aliases safe. |
| Binary/unary/conditional expressions and optional chains | JS number/BigInt semantics; preserve coercions and short-circuit/nullish edges. User coercion hooks are not primitive arithmetic. Optional property/call evaluation may skip arguments and calls. |
| If/loop/switch/return/throw | Explicit CFG, loop recurrence, switch fall-through, completion payloads. Primitive numeric reduction must produce X; callbacks/iterators require known summaries. |
| Calls, property access, arrow functions | Checker symbol plus finite runtime receiver provenance, captured values and call bindings. An interface type alone is not a closed target set. Unknown getters/proxies/dynamic targets remain partial. |
| Try/catch/finally, async/await | Rejection continuations and finally on normal/rejected completion. Awaited trusted cleanup is required for R; starting cleanup without awaiting it does not prove completion-before-return. Return in finally overrides pending completion. |
| Generators, decorators, eval, dynamic imports | Unsupported runtime rewrites, suspension cleanup and dynamic evaluation remain located partial facts. Preserve unrelated exports. No execution of repository scripts. |

Repeat TypeScript conformance with type-safety metrics enabled and disabled:
SHALLOW inventory, facts and scores must match. Cache compiler context once per
configured unit; no checker reconstruction per boundary or during rendering.

## 7. Executable fixture delivery contract

[Adapter cases](evidence/shallow-v4/adapter-cases.json) provide concrete source inputs
for every language in the common matrix. Each entry has four language variants;
`expected` declares the common assertion, and variant notes limit comparisons where
native error or construction contracts differ. A category-only expectation is not
a waiver of numeric eligibility: required measured variants must produce a complete
numeric assessment, with exact burden/score checked where interfaces are equivalent.

The existing [flow cases](evidence/shallow-v4/flow-cases.json) retain more detailed
flow slices and language-specific edge cases. Both sets are required. The runner
must report each `(case, language, variant)` individually; missing translations or
an unsupported result for a required measured fixture fail. Never count a skipped
fixture as a pass. Use fixture-supplied source/declarations/build selection, with
standard registry provenance; do not fetch dependencies to make fixtures pass.

Add focused adapter-native tests for every table row above. Minimum edge matrix:

| Contract | Go | Java | Rust | TypeScript |
| --- | --- | --- | --- | --- |
| Visibility and alias inventory | Embedding, unexported returned receiver | Nonpublic base, enclosing private class | Restricted export, renamed re-export | Default export, barrel cycle, callable object |
| Creation access | Zero value versus private-field literal | Explicit/implicit/private/record | Public/private field literal versus factory | Default/inherited/private constructor versus factory |
| Conditional evaluation | Short circuit and multiassign | Short circuit and finally overriding return | Match guard and tail/break result | Nullish/optional chain and default side effect |
| Local resolved dispatch | Two local interface implementations | Two known constructed receivers | Two known local trait implementations | Two object-literal receivers |
| Ownership/cleanup | Defer args, named result, panic, acquisition error | Later acquire failure, reverse close, suppressed exception | `?`, move out, guard drop, abort/unwind | Awaited close, rejection, unawaited negative |
| Known alias leak | Returned slice backing storage | Returned private array | Returned mutable reference | Returned private array |
| Required unknown | Open interface, go/select/recover effects | Open virtual call, unavailable generated member | Open trait, custom Drop, macro/cfg/unsafe gaps | Dynamic receiver, proxy/getter, missing declaration |
| Parser resilience | Malformed sibling | Malformed sibling | Malformed sibling | Malformed sibling; declaration file remains context |

For each unknown case assert reason code, affected dimensions, preserved supported
facts and a successful unrelated boundary. For all measurable fixtures assert cold/
warm parity and unchanged results after helper relocation/alias renaming. On the
invalidation fixture, change one helper and one public signature separately: the
first recomputes only consumers, the second also changes frozen contract inventory.

## 8. Acceptance and task handoff

Batch 2 supplies recurrence semantics, registry support for all concrete fixtures
and the common runner. Batches 3a–3d each own all rows in their language section and
column above. A fixture failure is implementation work, not permission to silently
broaden unsupported scope. A genuinely inconsistent requirement needs a documented
spec correction with the counterexample; it must not become an undocumented skip.

Run native adapter tests, the common matrix through emitted flow and shared scorer,
then report/cache/verifier integration and the existing performance gates. Record
per-language passed/failed/unknown-required counts. Completion requires zero missing
variants and zero unexpected unknowns for required measurable fixtures. This is an
implementation plan; the documented tests are not claimed to pass before coding.
