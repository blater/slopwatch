# Independent graded benchmark review baseline

Reviewer: separate engineering-review agent. Prepared before inspecting any scoring output or implementation. This baseline is an interpretation of the user mandate; final approval requires exact source and manifest review.

## Expected meaning

A low score (illustratively 0–25) should follow from appropriate small caller burden and actual discharged responsibility, or a genuinely simple cohesive role. Zero is an allowed endpoint, not the default for every unit without an explicit defect. A carrier's legitimate structure alone cannot excuse a large mutable invariant-bearing surface.

An intermediate score (illustratively 30–65) requires both meaningful remaining caller obligations and some hidden responsibility. Counting branches, lines, or method names cannot establish either. A helper containing complicated arithmetic is not necessarily deep if callers must still orchestrate all protocol details.

A high score (illustratively 70–100) requires substantial caller burden with little responsibility discharged. A lifecycle wrapper that preserves caller acquisition, operation ordering, rollback/cleanup, and close obligations is a direct example. A broad one-to-one forward surface is another when it leaves domain choices and coordination to the caller. Public mutable coupled state exposing an invariant is a third.

## Freeze before implementation

Every source needs its own rationale and expected band. Each matched improvement must preserve its intended useful capability while removing caller obligations; a narrower API that simply deletes capability is not an improved equivalent.

Recommended metamorphic tolerances, chosen without outputs: at most 3 points for semantics-preserving rename, private-helper extraction, and equivalent syntax; at most 10 for dependency visibility alone when the caller contract/local responsibility are unchanged. Require at least 15 points improvement where a matched example genuinely transfers substantial responsibility. Explicit manifest tolerances and final reviewed rationale govern over these tentative bands.

Unknown dependency evidence may reduce confidence but is not evidence of zero hidden responsibility or maximal shallowness. A missing-dependency twin must keep exactly the same target source and interface; only dependency availability should change.

## Rejection checks

- Lifecycle dependency behavior is absent or unconstrained, so obligations are only claimed in comments.
- Improved wrapper lacks cleanup on failure or leaves its protocol equally caller-managed.
- Representation example is merely a safe immutable value rather than exposed invariant responsibility.
- Improved representation still returns mutable internal state.
- Intermediate case has boilerplate volume but no actual caller obligations.
- Private helper is exported or caller-visible API changes in an alleged invariance test.
- Semantic equivalent syntax changes error paths, fallback behavior, or null behavior.
- A role exemption prevents all validated/cohesive examples from receiving any nonzero result.
- Required cases exist only as scorer evidence records rather than runnable real source.
- Real-world corpus bands were derived from analyzer results rather than reviewed responsibilities.


## Exact real-source review — 2026-09-18

Verdict: **approve the corrected real-source manifest as frozen engineering expectations**, not as proof of semantic conformance or corpus representativeness. All 19 snapshot SHA-256 values were verified. The reviewer inspected every target and all supplied codec/workspace support source, without inspecting score outputs or changing implementation. No model outputs informed the corrections below.

| Case | Reviewed range | Exact-source judgment |
| --- | --- | --- |
| java-result-carrier | 0–15 | Approved after replacing exact zero. `set` and `reset` form an appropriate small carrier; accessors do not claim directory execution. Caller still interprets file capability/durability. This does not justify a blanket carrier exemption. |
| java-codec | 0–25 | Approved. Two encode/decode operations preserve buffer/capacity/descriptor inputs but delegate to supplied owned implementations. Encoder validates arguments, domain/type compatibility and size before publication; decoder checks header/layout, body canonicality and destination capacity before publishing. Body validation covers bitmap, nullability, offsets, UTF-8 and numeric domains. Forwarding syntax does not erase these discharged responsibilities. |
| java-query-pipeline | 30–65 | Approved after correcting rationale: this file does not parse SQL. It coordinates bounded block allocation/reset, validates nested child shape and routes compilation. Its many topology/index getters and append/promote/compile variants retain substantial graph/pipeline obligations. `block` returns a live mutable `SqlCommand`. This supports intermediate rather than low-by-validation or high-by-method-count. Missing compiler/graph implementations limit certainty. |
| java-keyed-path | 0–20 | Approved. Record constructor enforces non-null identity/placement, immutable copied nonempty columns, default origin and explicit-key placement compatibility. These are substantive local invariants behind a compact domain value interface, not merely reconstruction. |
| java-query-result-row | 15–45 | Approved after rejecting original 0–25 value-boundary rationale. The class explicitly lives only until next fetch, returns its backing `Map`, and retains mutable column aliases. It also handles null/absent columns, decimal/time formatting and SQL-to-scalar mapping. Both remaining caller burden and hidden translation are visible. Treating Lombok as the issue would miss the actual tradeoff. |
| go-workspace | 0–25 | Approved. Constructor validates absolute canonical roots, privacy and separation, opens handles and closes acquired handles on failures, validates manifest and allowed paths. Supporting access checks canonical identity, symlinks and allowed write scope. Caller still owns successful lifetime/Close; extensive path responsibility is genuinely hidden. The error accessor cannot dominate this file. |
| go-publisher-contract | 0 or N/A | Approved only as an applicability/control case, excluded from semantic numeric-coverage claims. The snapshot is solely a `Service` struct with a private client field; it contains no methods or claimed operation. |
| typescript-predicates | 0–25 | Approved. Small structural predicates consolidate actual AST choices; traversal skips nested function boundaries and short-circuits. They are appropriate helpers. This judgment does not certify correctness of the `containsThisAssignment` name or assignment-operator semantics. |
| typescript-config | 0–25 | Approved. One exported entrypoint coordinates config discovery, defaults, multiple-project refusal, host/source caching, program creation, diagnostic conversion and final requested-source validation. It retains input configuration choices without exposing the internal sequence. |
| rust-location | 0–20 | Approved. Small span-to-location value adapter owns path copying and column normalization; no extra caller protocol. Small implementation alone is not shallowness. |
| rust-expression | 0–25 | Approved. Recursive boolean/group normalization and visitor-based call/closure extraction discharge AST traversal responsibility. Two exported operations (`expression` and `simple_path`), not the originally claimed single entrypoint. |

Changes were made to expected ranges solely after source review and before implementation/output inspection. The Rust expression and Java KeyedPath rationale wording was also corrected to match actual source.

### Limits of this real set

There is no independently established high real case here. Most cases come from cohesive project helpers deliberately selected for regression coverage. Two mixed cases and multiple low cases provide useful breadth, but this is not a representative distribution sample and must not be advertised as one. Dependencies are intentionally partial. A declaration-only control cannot inflate applicable numeric coverage. Mandatory high mechanisms, paired improvements and metamorphic constraints remain the synthetic benchmark's responsibility and require separate exact review.

## Pre-implementation design review — graded ratio proposal

Reviewed proposal: `100 * E / (1 + E + 2H)`, where `E` measures residual caller burden beyond a reference simple operation and `H` measures responsibilities actually discharged. **Conditionally acceptable direction, not approval of a particular implementation or calibration.** No score outputs were consulted.

The reference `1` can legitimately establish the unit scale for a cohesive small operation; it must not be described as one unit of inferred hidden work. The coefficient `2` is a calibration choice that needs frozen-case validation. This formula mathematically produces intermediate values and approaches high values as residual burden grows; that alone does not establish construct validity.

Conditions:

1. `E` must measure meaningful retained obligations, including legitimate ones, rather than only a catalog of recognized defects. Otherwise every ordinary abstraction again becomes zero until an explicit defect detector fires, violating the requested graded meaning. Operation choices, dependent parameters, cross-call ordering and representation invariants are potential evidence, not mechanically interchangeable tokens. Mere method count is not enough.
2. `H` must reflect distinct duties removed from callers. Validation that safely rejects bad state, owned failure cleanup, invariant-preserving mutation and coordinated domain operations qualify. Arbitrary calls, branches, lines and public forwarding do not. A wrapper forwarding `begin`, `write`, `commit`, `rollback` and `close` has not earned lifecycle credit merely because its dependency performs those methods.
3. Avoid mechanically double-counting the same underlying obligation through public fields, getters, setter parameters and associated state transitions. Conversely, count genuinely independent choices even if one function contains them.
4. Do not use a broad supporting-role exact-zero escape hatch. A support module can impose serious protocol or mutable-state obligations. Demonstrated passive carrier/no-extra-obligation cases may reach zero; a class label or lack of a finding cannot establish that result. The revised real carrier range deliberately permits low nonzero results.
5. Local incomplete-evidence estimation should explain observed caller surface and locally discharged duties, separately mark unknown callee behavior, and stay within the frozen matched-case tolerance when only dependency visibility changes. Unknown behavior is neither zero responsibility nor an arbitrary midpoint.
6. A universal guarantee that removing dependency source can never alter an otherwise identical score meaningfully is impossible for an estimator that sometimes learns real hidden duties from that source. Two implementations with identical interfaces may discharge radically different responsibilities. The defensible claim is bounded sensitivity for the frozen surface-matched perturbations, plus explicit uncertainty; do not claim whole-program semantic invariance.
7. Helper extraction must preserve the unit of responsibility, not add credit for the helper call and again for the helper body. Equivalent local syntax must preserve recognized obligations. These are acceptance properties, not post-hoc reasons to move expectations.

A formula with `E=0` always yields zero regardless of `H`. Therefore the implementation must demonstrate that ordinary intermediate examples acquire nonzero *residual caller burden* from their actual contracts without inventing defects. Mandatory high examples must also avoid being diluted by hidden-work credit for the very forwarding under test. Synthetic fixture review and frozen-case results remain necessary before this design can be accepted as implemented.

## Exact synthetic review — initial submission, 2026-09-18

Verdict: **REJECT; not frozen for implementation.** Reviewed input SHA-256: `666e429cba28417fcff9e691fe93b5c41ee2ada34cb804cb64ca5b28de98b76f`. All 48 actual case sources and the comparison/invariance definitions were inspected. No scoring implementation or score output was inspected. Compilation alone cannot validate the claimed responsibilities. The manifest status was changed to rejected; expected ranges were not tuned.

### Blocking findings

1. **The mandatory lifecycle wrapper is absent in every language.** High cases have no dependency or wrapped resource. Their private `active` counter increments/decrements inside a synchronous identity operation. TypeScript cannot observe a nonzero counter through this API; safe Rust's exclusive mutable access prevents concurrent observation. Java/Go contain no valid concurrency protocol and would instead have races under unsynchronized concurrent use. Consequently the high case's claimed exposed active-use accounting is not real caller burden. Compared with cohesive, high adds internal accounting and an extra close guard; it does not establish substantially less hidden responsibility.
2. **Lifecycle cohesive does not take lifecycle sequencing inside.** All callers still invoke open/use/close. Rejecting misuse can hide validation, but the fixture does not own acquisition/use/failure cleanup. The supplied cases cannot establish the required matched lifecycle improvement. Passive lifecycle methods are no-op/identity stubs, so they are not capability-preserving alternatives to a resource-owning abstraction either.
3. **The forwarding fixtures do not forward.** High and intermediate bodies are identity methods, an always-true health predicate and empty close. No callee contract requires sequencing or coordination. Calling the methods start/read/write/flush does not prove those obligations. The cohesive operation validates and returns `v+1`, while high operations return `v`; the claimed matched capability is false. Adding unused independent methods can demonstrate surface width, but this source does not prove the mandatory broad forwarding mechanism or hidden coordination improvement.
4. **Representation comparisons drop capability.** High exposes value, limit, checksum and enabled state. Cohesive only supports value and checksum; it cannot express limit/enablement. An actual improvement must preserve the same domain capabilities while enforcing their invariant internally. Passive representation has only a value and likewise cannot be called a same-capability version. The representation sources do expose real coupled state and are potentially useful standalone cases, but their current matched-pair claim is invalid.
5. **Metamorphic source variants are absent.** The 192 invariant records reference 240 IDs not present as runnable cases. `variant_links` are strings only; there are no variant source bodies or precise executable transformations. In particular, there is no dependency source in any base case, so known-versus-hidden dependency perturbation cannot be verified. This is a plan for pairs, not a frozen source benchmark for pairs.
6. **Several rationales are factually inaccurate.** Go and Rust passive representation fields are publicly mutable, despite the universal immutable-carrier rationale. They may still be legitimate low simple carriers because there is no coupled invariant, but immutability cannot be the reason. Lifecycle “open-after-close misuse” is not rejected: reopening after close is allowed.
7. **The high lifecycle comparison predicts the wrong mechanism.** It rewards the fewer-guards cohesive example as deep and demands high shallowness for extra private safety accounting. A scorer could pass by counting the advertised labels or surface syntax while violating the actual caller-burden construct. This must be fixed in source, not by fitting the scoring formula.

### Required source repairs before approval

- Model an actual resource dependency whose calls have documented and implemented setup/use/failure-cleanup obligations. High forwards those calls one-to-one. Cohesive offers the same useful work with the sequence and cleanup inside; intermediate owns only part of the sequence and preserves some meaningful caller duty.
- For representation, use the same logical state/capabilities in high and cohesive. For example, high exposes coupled fields while cohesive retains value/limit/enabled operations and synchronizes checksum/rejects invalid transitions. Include observable output demonstrating the preserved capability.
- For forwarding, supply actual underlying operations and a clear domain workflow. High presents the broad one-to-one surface; cohesive coordinates those operations into the same intended outcome. Do not replace capability with an unrelated scalar increment.
- Store actual helper, rename, syntax and known/hidden dependency sources, or store deterministic fully specified transformations with materialized hashes. Review the resulting bodies before freeze. Unknown twins must have identical target source and differ only in dependency visibility.
- Passive low role examples can stand independently. Do not falsely require passive carriers to preserve a resource service's whole capability. Relative score comparisons may compare roles, but only matched improvements may be claimed to preserve capability.
- Preserve predetermined numerical tolerances and evaluate concrete repaired examples by engineering judgment before seeing outputs. No distribution quota is required.

The real-source manifest's earlier approval remains valid. This rejection is limited to the synthetic source/relationship validity and blocks claiming that the full graded release benchmark has been frozen.

## Required real regression added — 2026-09-18

The directive explicitly requires both StoredTableRowCodec and SqlDerivedReferenceValidator. Codec was already present; the validator was missing. **Approved and froze `java-derived-reference-validator` at 0–25**, based on direct inspection of the original validator plus its predicate-reference and column-resolution helpers, before any score output inspection.

`validate(blockIndex, allowUnusedComputed)` combines projection/aggregate references, predicates and ordering, then optionally rejects unreachable computed outputs. Its helpers walk scalar/predicate programs, resolve aliases and duplicate output names, and detect invalid references. StatusCode and boolean validation are real responsibility even though no exceptions are thrown. The caller retains ownership of a valid query, valid index and policy choice, while the implementation hides multiple SQL reference rules. The range is low because the two-input operation consolidates those rules; it is not forced to zero because those caller preconditions remain.

Three new exact snapshots and SHA-256 values are stored in `graded-real/java-derived-reference-validator/` and the real manifest. Other project types remain unavailable by design and must be represented as incomplete evidence. There are now 12 real cases and 22 snapshot files. The real manifest remains approved; synthetic rejection remains unaffected.

## Exact synthetic review — v2 repair, 2026-09-18

Verdict: **REJECT pending the following concrete repairs.** The new Driver and Pipeline bodies establish actual work; representation families now preserve the intended value/limit/enabled capability. Passive controls are correctly separate. These are substantive improvements, but the exact v2 sources still fail the release gate:

- TypeScript's `Example.d` and `Example.p` are public, so even low callers receive the entire mutable dependency surface. Make these private. Java package-visible fields should likewise be private to express the intended class boundary.
- Rust lifecycle/forwarding `Example` has private fields and no constructor, so external callers cannot obtain it through the advertised API. Supply the same public construction path to all matched cases.
- Rust helper variant defines `append_all` but never calls it; instead it changes the loop to special first-element handling and directly resets dependency state. This is not helper extraction.
- Rust rename/comment variants omit the baseline's Drop guard, materially removing cleanup during panic unwinding. Preserve the exact guard and alter only the claimed property. A valid helper can be a private free function taking `&mut Driver`, called through the existing guard.
- Rename/comment variants in other languages also start from helper-extracted source rather than the baseline. Though mostly equivalent, this confounds what each test measures. Derive each variant directly from its declared baseline.
- Hidden-dependency variants retain the full inline dependency. Target selection cannot hide a declaration in the same file. Use a separate source file and physically omit it in the analysis workspace; missing-dependency compilation failure is intentional, not a reason to retain source.
- Lifecycle intermediate only batches append calls while leaving the same acquire/finish/release lifecycle duties. A mandatory 15-point improvement over high is not supported by moving a loop. A principled intermediate is `acquire(config)` followed by `complete(values)` that owns append/finish/finally-release, leaving the caller responsible for acquisition but removing subsequent cleanup sequencing.
- Dependencies sharing the wrapper target file can distort the rating under review, especially exported Go Driver/Pipeline. Separate supporting dependency files and evaluate the wrapper target consistently.

These are source/contract judgments, not scoring-result calibration. The underlying high/low lifecycle, pipeline, and representation ideas are now usable once the listed concrete defects and material metamorphic differences are repaired.

## Final synthetic approval — 2026-09-18

Verdict: **APPROVE and freeze the repaired 60-case synthetic benchmark before scoring implementation.** This supersedes the two earlier synthetic rejections for the final sources only. All source bodies were reviewed without consulting scoring outputs. Expected ranges and comparison deltas were not changed to fit implementation.

The final set has 40 base cases (three mechanism families with low/intermediate/high in four languages, plus four passive controls), 12 real source metamorphoses, and 8 physical missing-dependency variants. All 52 complete cases compiled successfully under the language compiler; all 8 intentionally missing-dependency cases failed compilation as expected. Evidence is persisted in `graded-fixture-compilation.json`. Every embedded file now has a source SHA-256, and the final manifest hash is recorded in `graded-benchmark.sha256`.

Final reviewer repairs were limited to executing the TypeScript extracted helper instead of leaving it dead, correcting Java's renamed target path, exposing Rust support modules consistently as independent APIs, replacing comment-only syntax variants with equivalent argument grouping, and correcting the stale uncertainty-policy text. None changed expected ranges. Renames and grouping now derive directly from the baseline; cleanup guards remain intact. Missing-dependency target contents were checked byte-for-byte against their known counterpart and contain only that target, with support physically absent.

### Exact family judgments

| Family | Low: 0–25 | Intermediate: 30–60 | High: 65–95 |
| --- | --- | --- | --- |
| Lifecycle, all four languages | `process` owns acquire, append iteration, finish and cleanup via finally/defer/Drop. | Caller must acquire first and subsequently complete; complete owns append/finish/cleanup. The gap between acquire and complete remains caller-managed. | Four direct operations preserve the independently available Driver's ordering and release duties. Useful callee validation does not mean the wrapper has owned the lifecycle. |
| Representation, all four languages | Private value/checksum with setter validation and synchronized update, plus enablement. | Caller mutates value and must invoke refresh; refresh hides validation/checksum calculation but preserves an exposed synchronization step. | Public value/checksum/enablement requires caller to keep the coupled state consistent; read detects misuse but does not maintain the invariant. |
| Forwarding, all four languages | One process operation owns normalization, validation, transformation and publication order. | Caller sequences load, prepare and finish; the latter two consolidate actual operations. | Six public forwards preserve the full underlying pipeline sequence, leaving normalization/validation/publication coordination outside. |

Matched improvements preserve the intended successful-use capabilities: batch checksum, enabled bounded-value read, and normalized/transformed saved string. They deliberately remove access to invalid/intermediate states rather than promising identical behavior under misuse. The source is a bounded teaching model, not a production concurrency, numeric-overflow or panic-abort guarantee. The public dependency APIs alone do not justify the high scores: the decisive evidence is caller-managed cross-operation state/workflow. Codec-style independent completed outcomes must continue receiving credit for responsibilities actually hidden in owned helpers.

The 15-point directionality checks apply to these actual transfers of caller obligations. Helper/rename/grouping tolerance remains 3, missing-dependency tolerance remains 10, with the predeclared no-increase checks. High missing-dependency cases make collapse-to-zero observable; low cases alone would not have done so. These tests establish bounded sensitivity for concrete examples, not universal semantic monotonicity under source loss.

The real set remains independently approved, including both required River regressions. Approval of benchmark validity is not a claim that the current analyzer passes it or that either P0 has shipped.

## Implementation calibration review — input uncertainty and duty attribution

Read-only review of `graded_evidence.go` during implementation; no benchmark expectations changed. **Reject the generic material-uncertainty input discount.** Required inputs remain visible caller burden even when their necessity cannot be proven. Subtracting every input unit from the numerator while retaining it in the denominator can make a multi-input single operation score zero solely because dependencies are missing. Restoring only unused-parameter findings recreates a defect-detector gate rather than the requested graded assessment.

A defensible bounded-source rule keeps observed `E`, attributes locally demonstrated owned duties to `H` even when dependency bodies are unavailable, and separately labels unknown scope. For example, ownership of a matched cleanup path, actual coordination of state transitions and invariant-preserving publication can be visible at the wrapper. An unknown callee is not assigned fictitious fixed responsibility; known `H` is a lower bound on discharged duties, not a claim that total unknown responsibility is zero. The resulting number must be described as a source-context estimate. Arbitrary unresolved services do not admit a universal narrow confidence interval or monotonicity guarantee without additional assumptions.

Three implementation hazards were reported to the parent:

1. `publicStatefulProtocol` includes constructors when looking for cross-operation writes/reads. A constructor setting a field and a getter reading it therefore marks an immutable value as a protocol. Constructor initialization cannot by itself establish caller-managed sequencing.
2. Once a type is marked protocol, the current evidence loop discards every category from it, including real per-action validation/transformation, and may remove its limitations. Suppress only responsibility whose ownership remains demonstrably external; public/stateful is insufficient. A protocol can still hide useful duties inside each action.
3. The implementation currently grants coordination from two method names on one receiver and grants resource credit from any finally/defer/drop token. These are opportunities to inspect, not proof that coordination or cleanup responsibility moved inside. Match state/output flow and cleanup ownership before claiming those duties.

These findings concern construct validity beyond the frozen example set. Passing selected fixtures cannot turn broad category suppression or syntax-count credit into a semantic guarantee.

## Follow-up implementation review — scoped protocol suppression

The next inspected revision removes the generic uncertainty input discount and excludes constructors from cross-operation protocol discovery. These address the reported zero-score and immutable-constructor issues.

Remaining exact-source findings reported before release assessment:

- `protocolCallerDuty(validation)` treats any guard referencing any declared field as a protocol precondition. A validated mutable value with `setValue(v)` checking `v > limit` and a separate read method therefore loses actual bounds-validation credit, even when `limit` is immutable constructor configuration rather than a caller sequencing state. Suppression must use the identified cross-entrypoint prerequisite fields, not all fields.
- Evidence is currently categorized per operation. One protocol-state guard plus a separate independent input guard can suppress all validation for that body. Preserve the independent input duty, or explicitly retain uncertainty rather than claiming the whole validation category stays outside.
- Field reads/writes are recognized by token spelling with parameter exclusions but no general local-shadow binding. A local variable sharing a field name can establish a false protocol or invariant. Explicit receiver access is available in Go/TypeScript/Rust; Java implicit fields need at least local-shadow exclusion.

The parent reports frozen synthetic passes and ongoing real-case fixes, but this reviewer has not yet independently inspected final packaged-CLI results. Passing reports cannot be inferred from these source-level improvements.

## Adversarial implementation probes — beyond frozen examples

Two read-only Go overlay probes were run against the in-progress source estimator without editing repository code or frozen expectations:

1. An unchanged Java four-stage wrapper scored **89** when its Driver's acquire validation was `if (active || config.isEmpty()) throw ...`, but **74** for the equivalent two consecutive guards. Caller burden stayed 8.5; hidden validation changed from 0 to 1. This demonstrates semantic grouping sensitivity in per-operation protocol-validation suppression. A defensible fix can attribute no *additional wrapper duty* on demonstrably transparent relay edges that retain an independently public protocol unchanged; it must not blanket-exclude every public stateful delegate, including composed outcomes.
2. An unchanged Java `Example.process(a,b,c)` delegates to package-private `Worker.process`, which rejects negative inputs then returns their sum. It scored **14** with the helper and **33** when that helper source was physically removed. Residual burden stayed 0.5; observed hidden validation fell from 1 to 0. This confirms a general dependency-loss upward-jump gap that the frozen public-protocol pairs do not exercise.

The second counterexample cannot be solved solely by changing public-protocol attribution. A source-context model needs stable evidence of discharged duties at call sites or verified retained dependency summaries where available. Where the only evidence truly disappears, there is an identifiability limit: the same unknown signature could hide an identity function or a full validator. No general no-upward guarantee follows from the current ratio or the finite frozen cases. Release claims must reflect this unresolved scope honestly.

Probe source was kept outside the repository at `/tmp/graded-independent-probe_test.go` and injected with Go's overlay option. The probes are adversarial evidence, not post-hoc revisions to benchmark expectations.

## Packaged result inspection and final adversarial follow-up

Inspected `build/shallow-graded-after.json` and `build/shallow-graded-real-after.json`: all 60 synthetic and 12 real cases report passing. Actual real ratings include StoredTableRowCodec **17**, SqlDerivedReferenceValidator **7**, SqlQuery **63**, and QueryResultRow **36**. These are genuinely graded outcomes; synthetic mechanism highs are 67–94 and intermediates 30–55 in the inspected report. The data still represents the frozen sample, not general semantic proof or enterprise capacity.

Re-ran the earlier probes against the next implementation: combined/split public-protocol guards now both score **89**; the private helper loss case changes **14 → 7**, with the missing-side **3 estimated responsibility units explicitly separated from observed H**. The new uncertainty rule is a modeling allowance for a returned input-to-result duty envelope, not proof that an unknown callee validates or transforms. Its scope and constants require honest documentation.

Two additional concrete defects were then reproduced:

- Unresolved `Worker.process(a,b,c)` scores **7**, while the equivalent `Worker.process((a),b,c)` scores **33**. Balanced parentheses fail the transparent-argument identity check and erase the allowance. This is a blocking harmless-syntax instability rather than a calibration disagreement.
- Go `defer println("log"); e.d.Set()` is classified as guaranteed cleanup when `Set` merely mutates delegate state. The defer recognizer scans the whole remainder of the body, attributing the later ordinary call to defer. The scope must stop at the actual deferred call or closure.

Both were reported for repair. Therefore the passing packaged artifacts, at this review point, do **not yet warrant an unconditional release approval**. Frozen expectations were unchanged throughout.

## Final independent graded-gate verdict

**APPROVE the bounded graded-scoring implementation gate for the reviewed sources and regression scope.** The two last confirmed implementation blockers are repaired and independently rechecked. Coverage reporting must still honor the declaration-only control exclusion; that accounting repair is tracked separately by the benchmark runner.

Final independent rerun results:

| Probe | Final result |
| --- | --- |
| Combined versus split protocol guards | 89 / 89 |
| Validation helper present versus missing | 14 / 7; missing-side estimated duty explicitly distinguished |
| Unknown call with plain versus parenthesized argument | 7 / 7 |
| Logging defer followed by ordinary mutation | Not classified as cleanup |
| Arithmetic helper present versus missing | 9 / 7 |
| Four-language private-helper unavailability regression | Pass |
| Defer scope and protocol guard regressions | Pass |

The final review also verified the synthetic manifest SHA remains `ac9a39a3e761201e3a9f8d8078b9adf7976c2b905c92e33840a38aa784c330df`, all embedded/snapshot source hashes still match, and the inspected packaged-CLI reports contain 60/60 synthetic plus 12/12 real passing results with no failures. No frozen expected range or directionality threshold was altered after implementation.

This approval is scoped. Estimated responsibility allowances remain bounded source-context modeling, separately reported from observed responsibilities; they are not proof of arbitrary unknown callee behavior. Structural coordination/cleanup attribution and finite parser coverage remain heuristic limitations. The reviewed real set is small and not a representative enterprise distribution. This verdict does not certify 30,000-file performance, complete whole-program semantics, or automated-fix eligibility, and does not by itself close the overall SHALLOW release/P0 directives.

Integration follow-up (implementing agent): the runner now honors
`exclude_from_semantic_numeric_coverage`. The final real report records Go total
numeric coverage 2/2, semantic-applicable numeric coverage 1/1, and one excluded
declaration-only control. Both final packaged reports were rerun after the fixes;
they retain zero failures. Full results are in [graded-delivery-results.md](graded-delivery-results.md).
