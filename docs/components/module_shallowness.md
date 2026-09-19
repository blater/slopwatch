# Module shallowness

The default is `module_shallowness/responsibility-burden-v4`. Policy r41 publishes
a graded 0–100 assessment of remaining caller obligations relative to hidden
responsibility; higher means shallower. It does not require an individual defect
rule to fire. Incomplete evidence retains estimation metadata and specific
limitations. A proven absence of an applicable abstraction is N/A. Details retain the boundary
identity, proof rules and reasons. See [usage and current coverage](../shallow-v4-preview.md)
and the [interim release backlog](../shallow-v4-interim-release.md).

Select `--score-profile=legacy-signature-v3` to use the previous measure for
comparison. Its `module_shallowness/ousterhout-v3` definition is a file-scoped,
0–100 penalty measuring
useful functional capability per caller-visible interface cost. Its numerator
is COSMIC-inspired functional capability, not LOC or implementation
complexity; its denominator includes API surface, type cost, exposed state,
and Rising–Calliss information-hiding cost. The legacy implementation uses
the explicitly reported static approximation while adapters are extended with
full signature, data-group, persistence, and information-hiding facts. Its
depth reference is selected by the versioned `role-shape-v2` policy from
caller-visible role evidence. See
[`../depth-design.md`](../depth-design.md) and the
[`implementation plan`](../depth-implementation-plan.md).

The value is deliberately bounded and is an indicator for triage, not a claim
that a source file is itself a module in every architectural sense.

### Go file attribution

Go analysis resolves helpers across the package, but file ratings do not inherit
an aggregate package score. Exported free functions belong to their declaring
file. Each file implementing methods of a receiver type assesses that type's full
exported method surface, including methods declared in other files. Thus splitting
a type across implementation files preserves its abstraction rating. A file with
multiple independent abstractions reports their maximum SHALLOW. Private-only
implementation files assess their local callable abstractions without adding
those private operations to a public caller's burden.

Declaration-only contract files (types, interfaces and constants, without functions
or variable initialization) receive file SHALLOW 0 and no SHALLOW contribution to
SCORE. Their role explanation distinguishes this from exceptional functional depth.
Unrelated sibling functions and types do not enter a file's burden or responsibility;
reachable private helpers can contribute hidden work, but not additional exposed
operations. Resolution and traversal remain bounded, with uncertainty recorded
separately. JSON scope is `file`; tables show the numeric file rating without a
package suffix.

Private supporting error representations do not displace a file's behavioral
entry points. The bounded Go rule requires a private receiver whose only method
implements `Error() string` by directly returning a declared string field.
Additional methods or executable getter behavior prevent this exemption. Role
metadata records the exclusion independently of the behavioral estimate; a file
containing only a proven supporting representation receives zero penalty.

### Audience and supporting-role attribution

The bounded source projection applies these rules across Go, Java, TypeScript
and Rust. Exported entry points and public trait implementations have an external
audience even when their concrete receiver is private. Java package-visible
services and functions called from sibling source have a package/module audience.
A non-exported function with no resolved local caller is retained as a
`local-unresolved` entry point, explicitly marking the inferred audience.
Same-file private helpers are followed from their callers and add no independent
caller burden. Go methods belonging to the same receiver retain the full receiver
surface across files.

Proven field-only data/error representations are recorded separately. Their
getters and pure construction cannot displace service behavior in the same file.
Executable defaults, transformations and meaningful state transitions prevent the
passive proof. Independent behavioral abstractions retain separate projections;
the file maximum keeps a shallow independent service visible alongside deeper
work. The info details include the considered abstractions, audiences, burden and
hidden responsibility, with source estimates distinguished from precise evidence.
Helper lookup uses shared indexes and bounded traversal, including explicit
relative TypeScript imports and locally constructed Go receiver bindings.

Java record value-object recognition permits a resolved static
`java.util.List.copyOf` applied directly to the matching component parameter in
compact or canonical constructors. This preserves the data-role penalty of zero.
Other constructor calls, computed copy arguments and same-named methods on other
types do not establish this role. Source fallback evaluates compact-constructor
bodies separately from record headers and preserves component assignment effects;
unresolved calls retain estimation metadata rather than becoming zero responsibility.

### Graded caller responsibility (r40)

The independently reviewed benchmark was frozen before implementation. It contains
low, mixed and high abstractions in all four languages, positive detections,
directional pairs and source-visibility/refactoring twins. See the
[complete delivery results](../evidence/shallow-v4/graded-delivery-results.md).

The source rubric assigns one unit to a callable operation, a quarter unit to a
simple accessor/predicate, and a quarter unit per required input. Constructor
overloads contribute the largest required setup-input set once. A single ordinary
operation with one input is the reference surface, so residual surface burden is
`max(0, operations + inputs - 1.25)`. Exposed coupled mutable fields and retained
protocol stages add representation/sequencing burden. Mere method brevity does
not establish a high rating. Supported validation, transformation, state
consistency, resource management and useful coordination contribute hidden duties;
private helper calls do not add another exposed operation.

With residual caller burden E, observed responsibility H and separately reported
estimated responsibility U, the published rating is
`round(100 * E / (1 + E + 2 * (H + U)))`. The denominator's reference unit sets
scale; it is not a fictitious hidden duty. These rubric units are calibration
choices, not physical measurements. No fixed-50 prior or blanket score cap is used.
Existing precise burden/obligation facts and their descriptive ratio remain in the
ledger, separately from this customer-facing calibration and fix eligibility.

A transparent relay of an independently available multi-stage protocol does not
claim the underlying service's implementation duties: callers still manage its
stages. Composition inside the wrapper receives coordination/cleanup credit when
supported by owned delegate flow. Actual input validation remains credited on
composed paths, independent of combined-versus-split guard spelling.

Unresolved input-to-result delegation admits a bounded *possible duty envelope*:
input-domain validation (one unit) and result transformation (two units), less
already observed duties in those categories. This allowance is recorded as U,
not observed H, and applies once to the connected result, not once per unknown
call. Composed owned protocols can similarly retain an unobserved input-admission
duty. Discarded unrelated calls do not earn this allowance. This makes supported
helper-loss examples stable without pretending to know arbitrary missing bodies.
Adding recognized validation replaces its estimated category unit; it must not
remove the remaining result-transformation allowance. Observed and estimated
units therefore stay separate without double credit. Eligibility for this
allowance is independent of strict transparent-protocol attribution.

The bounded recognizer accepts a final top-level returned call, a Rust tail or
expression-bodied arrow, or a call assigned to an immediately returned local.
Parenthesized arguments/results, TypeScript `await`, and simple qualified-name
or array casts/type assertions do not change eligibility. Local aliases must
be introduced by a supported declaration. Complex type wrappers remain outside
this bounded check. Intervening assignments,
discarded calls and nested result flows do not establish this allowance. It does
not perform general alias/control-flow analysis. Where validation is itself not
recognized (including some Rust early-return/tail combinations), its unit stays
estimated rather than becoming observed. It is a bounded heuristic assumption,
not a universal semantic equivalence proof.

The same bounded envelope also covers unresolved input-dependent results flowing
through local lookup values into returned receiver/argument calls or conditional
expressions. It is applied once per abstraction, not once per unresolved call or
branch. Recognized validation and transformation-family duties replace overlapping
estimated units. Predicate-only results receive the possible validation unit,
not a second transformation allowance for the same admission test.
Disconnected calls, overwritten values and unexecuted closures
do not establish a returned-result allowance. General control-flow and alias
resolution remain outside this bounded analysis.

`supported_burden` records the observed caller surface, not a claim that all
implementation responsibility is known to be absent. Unresolved connected result
work is represented separately as estimated responsibility in the denominator.
The actual xmltoaster QueryResultRow's conditional formatting and `newValue()` are
tested separately from the frozen NQL switch/decimal/`getScalarKind()` snapshot;
neither namesake validates the other. See the
[actual-source regression evidence](../evidence/shallow-v4/lookup-uncertainty-results.md).

The numeric calibration is declared in
[`calibration_default.json`](../../go/internal/sourceestimate/calibration_default.json).
Its digest participates in cache/report identity. The
[calibration safeguards](../../go/internal/sourceestimate/CALIBRATION.md) distinguish
frozen calibration fit, parameter sensitivity and a separately frozen real-source
holdout with predeclared high-band cases.

Every zero identifies a recognized exempt role, an assessment in the lowest
supported range, or a conservative estimate under material uncertainty. The last
category is excluded from successful semantic coverage. Positive ratings enter
the normal file maximum, sorting, exports and SCORE calibration. Small positive
ratings can have zero SCORE contribution under the existing contribution curve.

### Structural responsibility evidence (r39)

Identifier spelling alone does not establish resource or state responsibility.
Resource evidence requires connected lifecycle effects on the same storage or
captured resource and a cleanup construct protecting relevant exits. Resolved
local protocols can establish those effects without relying on method names;
unresolved connected cleanup remains explicitly estimated. Unsupported cleanup
paths and library contracts remain limitations rather than invented proof.

Equivalent field syntax is normalized before assigning storage effects. A
computed update already attributed to a transformation duty does not receive a
second state duty for that same update; distinct uncovered state effects remain
eligible. Tests cover decoy identifiers, protocol/field renaming, alias/shadowing
and cross-language syntax. See [structural correction evidence](../evidence/shallow-v4/structural-responsibility-results.md).

The [calibration policy](../../go/internal/sourceestimate/CALIBRATION.md) requires
a reviewed, evidence-backed report before another numeric weight adjustment.
Observed holdout cases are evaluated separately with isolated, frozen-witness
and live-workspace context; they do not become fresh validation after tuning.

### Connected caller and lifecycle evidence (r40)

Boolean lifecycle recognition relates acquisition, admitted use and restoration
on the same storage, independently of which literal denotes the active state.
An intervening release or replacement invalidates the earlier acquisition.
Scoped caller evidence can expose package-visible representation obligations;
missing caller declarations or inheritance remain limitations rather than inferred
coupling based on field names. Restricted Rust visibility retains its audience
when same-file test code calls the boundary.

Typed library contracts support retained Map factory values and independent JDBC
cleanup attempts. Output projection with unresolved implementations remains an
estimated duty and shares the transformation envelope rather than adding duplicate
credit. These are bounded source rules, not general ownership proofs. The
[connected-obligations evidence](../evidence/shallow-v4/connected-obligations-results.md)
records observed holdout/context results and separately frozen real high-band
examples in Go, TypeScript and Rust. Production numeric weights remain unchanged.

### Separate adverse findings and analysis metadata

Required, unused inputs provide concrete excess-interface evidence in Go,
Java, TypeScript and Rust. Exposed operations can support a source-inferred
finding when another input is used; observed callers provide separate support.
Internal Go services require a sibling caller. Private helpers do not become
independent caller-facing surfaces. Contract implementations, callback bindings,
optional/default inputs, known compatibility stubs and dynamic argument access
are excluded where recognized. These are bounded heuristics, not proof that every
possible consumer or contract has been found.

The finding records its operation, parameter, caller witnesses (when observed),
and avoidable input burden. Finding support is separate from the graded rating;
absence of one of these specific findings does not force SHALLOW to zero.

The ordinary analysis-mode label is **Estimated**. It is not a warning and does
not determine finding support. Finding support separately says **observed caller
burden** or **inferred from source**. Specific unresolved delegated behavior is
shown against its affected abstraction. Generic semantic-completeness reasons
remain in the exported ledger, not the ordinary explanation. Automated-fix safety
checks and inventory requirements remain independent and unchanged.

Calls and their effects are collected before value normalization can discard an
assignment. Nested argument calls remain visible even if the callee ignores that
argument. In particular, processGroupAlive retains syscall.Kill uncertainty.


### Analysis diagnostic presentation (r41)

Completed analysis diagnostics do not open the follow-mode error overlay. Actual
failed file coverage is marked `!`; the file info view retains its error details.
Incomplete semantic proof and incidental compiler warnings do not create that
marker or appear in file diagnostics. Raw structured diagnostics remain available.
TypeScript TS6059 is an emit-layout constraint: with no-emit analysis it is retained
as log-only information and does not discard usable typed analysis. Syntax errors,
semantic failures and unusable project configuration keep their real coverage
states. The policy revision invalidates cached pre-correction projections.
