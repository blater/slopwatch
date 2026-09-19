# SHALLOW remediation revision 5 review

Date: 2026-09-17. Reviewed the remediation plan and its normative v4 specification against the current facts model, SHALLOW implementation, scoring projection, cache preparation and unit dependency infrastructure. Lenses: adversarial, edge-case, structure. No project-context.md was found. This is a design review, not an implementation benchmark.

## Verdict

The direction is substantially more faithful to the metric's intent than the current signature-derived implementation. It is feasible to build a bounded version that remains performant, but the fixed rules and release contract are not yet ready for implementation as stated. Evidence plumbing and a narrow extraction prototype can start; parallel adapter implementation and default-profile rollout should follow the policy corrections below.

Keep usable boundaries, independent implementation evidence, source-resolved delegation, explicit unknowns, source links, profile migration, shared summaries and incremental invalidation. Do not expand into general program verification. The needed changes are primarily accounting rules, precise finite analysis semantics and targeted regression cases.

## What the numbers establish

All 13 numeric golden fixtures reproduce the specified integer formula. The other three fixtures encode partial/inapplicable/unavailable states; their intended semantic propagation was not tested by this arithmetic check. There are no source extraction fixtures in the supplied shallow-v4 evidence directory. Arithmetic consistency does not validate the chosen facts or category weights.

A supported normalized counterexample is a required policy API with H=3, O=1,T=2,A=2,P=1,S=L=0: SHALLOW=37. Add an equivalent one-argument overload supplying the policy default: H remains 3, O=2,T=2,A=3,P=0, and SHALLOW=38. This is a policy-level calculation, not a claim that an implemented extractor currently produces these facts.

## Fidelity and implementation recommendation

Represent bounded obligation identities separately from public entry routes. Deduplicate shared responsibilities at the boundary; track which routes discharge them and which leave caller obligations. Preserve outcome evidence without automatically equating every returned value with hidden responsibility. Decide explicitly how much pure-computation depth this coarse recognizer can distinguish. Avoid rewarding extra state, locks or checks solely because more categories appear.

Before parallel adapter work, settle optional/defaulted input accounting, constructors versus factories, local policy selection, branch/dispatch joins and file-independent aggregate contributions. Supply the normalized flow contract and concrete built-in entries. Add source-level equivalence and negative fixtures for each decision. These are finite engineering design tasks; no statistical calibration study is required.

## Performance recommendation

The existing unit graph and Merkle invalidation are useful foundations. The current normalized expression model does not already provide the def-use, ownership and exit semantics this proposal needs. Cached unit analysis also does not by itself prove that idle refresh performs no scans: cache preparation currently gathers and hashes relevant paths.

Use bounded context-insensitive summaries per function/SCC, parameterized bindings, interned obligation IDs and shared evidence references. Specify finite alias/access-path domains and conservative unknowns at caps. Bound total retained summary bytes, binding expansion and artifact-wide work as well as per-boundary work. Make coverage limits logical and deterministic so cache warmth and worker scheduling cannot change eligibility.

Extend the 30K benchmark with high fan-in, diamonds, recursion, a large single unit and large public inventories. Count hashing/source reads, fact extraction, joins and binding substitutions, not just analyzer invocations. Specify benchmark-environment budgets for startup, warm/edit latency and process-tree memory. No timings were measured in this review, so performance is feasible by design but remains unproven.

## Findings

Overlaps are retained because independent lenses identified the same risks: file regrouping affects aggregate scores; summary bounds leave cardinality costs open; route and dispatch joins affect responsibility credit.

### adversarial

**1. Identity and constant returns earn U without establishing a hidden obligation.**

Location: `shallow-v4-implementation-spec.md:118`.

Fix: Separate outcome/relevance evidence from hidden-responsibility credit; explicitly decide the intended identity/constant-return baseline.

Consequence: The identity case gets H=1 and scores 47 despite hiding no recognized computation, state, policy or lifecycle obligation.

**2. Distinct public roots share the same underlying responsibility.**

Location: `shallow-v4-implementation-spec.md:95-105`.

Fix: Deduplicate obligation identities by boundary, governed root/outcome and category; retain families as access routes. Add shared-state source fixtures.

Consequence: The same validation, state or synchronization obligation earns credit per public root, contrary to the overview at lines 110-112.

**3. A primitive arithmetic wrapper and a substantial pure computation have the same category vector.**

Location: `shallow-v4-implementation-spec.md:41-45,123`.

Fix: Add contrasting pure-computation source fixtures. Either accept and prominently document the resolution limit, or recognize a small capped set of independent transformation obligations; do not count operators or loops.

Consequence: Both receive U+2X and score 23 for the example signature, while state and synchronization earn additional credit. This limits fidelity to useful hidden functionality.

**4. Fully resolved dispatch alternatives hide different responsibility categories.**

Location: `shallow-v4-implementation-spec.md:151-153`.

Fix: Retain bounded alternative vectors or derive a supported minimum H across alternatives; specify deduplication and conservative fallback at the alternative cap.

Consequence: One U+C implementation and one U+X implementation each have H=3, but category intersection yields H=1 and changes an otherwise identical score from 23 to 47.

**5. Equivalent creation APIs use explicit constructors, implicit constructors or factories.**

Location: `shallow-v4-implementation-spec.md:30,91-93,213`.

Fix: Define language-neutral creation facts and fixtures covering all three; consistently distinguish allocation from meaningful transformation.

Consequence: Explicit constructors add O, implicit constructors lack a rule, and factory record construction can earn U/X while constructor-only objects are inapplicable.

**6. Equivalent optional inputs are parameters or record fields.**

Location: `shallow-v4-implementation-spec.md:32,66-71`.

Fix: Separate required supplied slots from optional exposed choices and apply the distinction consistently across both representations.

Consequence: All declared parameters count toward A; optional record fields do not. Packaging alone changes caller burden.

**7. A source-local enum or Boolean selects policy without a callback or registry policy position.**

Location: `shallow-v4-implementation-spec.md:79-87`.

Fix: Specify a bounded recognizer for resolved public-input-controlled behavior selection, with negative data-transformation controls.

Consequence: Caller policy decisions are omitted depending on implementation syntax.

**8. Unchanged boundaries move between files or acquire a different namespace anchor.**

Location: `shallow-v4-implementation-spec.md:206-211,303-305`.

Fix: Compute aggregate SHALLOW contribution using stable boundary identities; project maxima onto files only for navigation. Add regrouping and anchor-move tests.

Consequence: File maxima collapse contributions when boundaries share a file, allowing SCORE improvement from a navigation-only edit.

**9. Adapters begin implementation without a shared flow semantics contract.**

Location: `shallow-v4-implementation-spec.md:111-114,348-353`.

Fix: Specify normalized alias roots, bindings, exceptional exits, joins, ownership transfer and provenance; provide source-to-flow-to-fact fixtures before independent adapter work.

Consequence: Scorer fixtures can pass while equivalent source produces incompatible facts in different languages.

**10. Registry implementers must decide overload effects, aliasing, exceptional exits and policy positions.**

Location: `shallow-v4-implementation-spec.md:160-179`.

Fix: Supply concrete initial registry entries and positive/negative source fixtures as policy artifacts.

Consequence: The allegedly fixed policy still leaves scoring and eligibility decisions to implementation.

**11. Many boundaries share large helper graphs or recursive summaries accumulate bindings.**

Location: `shallow-v4-implementation-spec.md:324-338,373-379`.

Fix: Bound summary cardinality, retained bytes, instantiated bindings and artifact-wide work; intern shared evidence and benchmark fan-in, diamonds, deep chains and SCCs.

Consequence: Per-boundary limits and two workers do not bound aggregate work or memory; independent-package smoke data misses these costs.

**12. Benchmark timings or memory regress without causing whole-workspace reanalysis.**

Location: `shallow-v4-implementation-spec.md:373-379`.

Fix: Define explicit startup, warm/edit latency and peak-memory acceptance budgets for a stated benchmark environment, separate from deterministic unit tests.

Consequence: Recording performance alone permits an arbitrarily expensive implementation to satisfy completion criteria.

**13. A convenience overload supplies a previously mandatory policy default.**

Location: `shallow-v4-implementation-spec.md:85-86`.

Fix: Add a monotonicity source fixture and define route-aware burden: distinguish required decisions on a usable route from optional surface choices.

Consequence: With H=3, O=1,T=2,A=2,P=1 scores 37; adding a one-argument default overload yields O=2,T=2,A=3,P=0 and scores 38. Removing a caller obligation worsens SHALLOW. This overlaps the parameter-packaging issue but is a distinct counterexample.

### edge-case-hunter

**1. Validated and unvalidated public routes share a delegated service family.**

Location: `shallow-v4-implementation-spec.md:95-105`.

Fix: family.V = all(public_routes.establish_validation); otherwise conditional

Consequence: A validated route can credit responsibility to an unvalidated route.

**2. Equivalent behavior uses an explicit conditional instead of finite receiver dispatch.**

Location: `shallow-v4-implementation-spec.md:151-158`.

Fix: Specify category-specific CFG joins and assert equivalence to receiver joins.

Consequence: Equivalent control flow can receive different responsibility facts.

**3. Two unchanged boundaries move from separate files into one file.**

Location: `shallow-v4-implementation-spec.md:206-211`.

Fix: Aggregate by stable boundary identity before projecting onto files.

Consequence: Maximum-per-file aggregation removes a contribution without changing the contracts.

**4. Cached shared summaries exceed a boundary’s cold-analysis work budget.**

Location: `shallow-v4-implementation-spec.md:324-338`.

Fix: Charge deterministic logical costs independently of cache residency and scheduling.

Consequence: Warm and cold runs may disagree on numeric eligibility.

**5. Recursive substitution grows access paths or calling contexts.**

Location: `shallow-v4-implementation-spec.md:155-158`.

Fix: Define a finite abstract domain, bounded paths/contexts, join order and widening to unknown.

Consequence: Fixed-point results depend on implementation-specific truncation or budget exhaustion.

**6. One worklist update processes a large alias or binding set.**

Location: `shallow-v4-implementation-spec.md:332-338`.

Fix: Charge element visits; cap summary cardinality and evidence payload size.

Consequence: Update-count limits allow large CPU and memory consumption.

### structure

| Pass | Original text | Revised text | Changes |
|---|---|---|---|
| structure | Specification §6 jname check within acceptance cases | MOVE the real-project coverage limitation to the opening scope/status block; retain the source-complete fixture requirement in §6. | The motivating repository may remain unknown due to Lombok and injected dispatch. Front-load this qualification. Word impact: 0; relocation only. |
| structure | Plan: four implementation stages; specification §8: seven ordered batches | QUESTION: label plan stages as outcome groups and specify §8 as the sole execution order. | The plan puts comparison safety first; the specification puts it fifth. Clarify work ordering. Word impact: approximately +15. |
| structure | Specification §7 Profiles, comparisons and caching | MOVE cache invalidation and limits into a dedicated performance section. | Make performance requirements directly addressable from tasks and acceptance criteria. Word impact: approximately +5 heading words. |

Machine-readable findings: [review JSON](shallow-measurement-review-r5.json).
