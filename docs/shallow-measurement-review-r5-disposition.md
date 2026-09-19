# Independent r5 review: disposition in r6

Status: incorporated into the design; analyzer implementation and runtime validation remain pending.
Date: 2026-09-17.

Finding numbers below are the 1-based positions in the unchanged [review JSON](shallow-measurement-review-r5.json).
The current [specification](shallow-v4-implementation-spec.md) and [overview](shallow-measurement-remediation-plan.md)
replace r5. The [reviewed r5 specification](shallow-v4-implementation-spec-reviewed-r5.md)
and its [scoring cases](evidence/shallow-v4/scoring-cases-reviewed-r5.json) remain available for comparison.

| Finding | Disposition and required regression evidence |
| --- | --- |
| 1. Identity/constant receive hidden credit | Spec §1: U is relevance only, weight zero. Identity, constant and callable no-op have H=0 and score 100; passive creation is N/A. Numeric fixtures fix this explicit heuristic baseline. |
| 2. Shared responsibilities count per public root | Spec §2: obligation identity excludes family/access route; deduplicate across the boundary. Shared state/validation golden cases and shared-state source case cover this. |
| 3. Primitive and substantial pure computation tie | Accepted resolution limit, explicitly documented in plan and spec §2. Paired numeric and source cases require the same X credit for equivalent interfaces. No claim of algorithmic sophistication; no operator/loop counting. |
| 4. Dispatch intersection loses C-versus-X capability | Spec §2: retain bounded alternative sets, union across families before taking minimum weighted burden-hidden credit. Both alternatives retain H=2. |
| 5. Creation syntax changes verdict | Spec §3: one creation family for equivalent explicit/implicit constructors and factories; passive packing has no X. Creation-equivalence and passive-creation fixtures. |
| 6. Optional record and parameter differ | Spec §1: flatten transparent leaf slots, normalize concepts, distinguish A from E. Paired optional parameter/record fixtures have identical terms and scores. |
| 7. Local policy selector missed | Spec §1: bounded Boolean/enum recipe selection over independent payload/resource adds P. Source fixtures distinguish mode selection from Boolean data encoding. |
| 8. File movement changes aggregate | Spec §5: authoritative unique boundary ledger; file projections are non-additive. Freeze boundary IDs for verification. Layout fixture includes anchor movement and duplicate references. |
| 9. No common flow semantics | New [flow contract](shallow-v4-flow-contract.md) §§1–5 defines transfer, bindings, ownership, aliases, exceptions, joins and provenance. [Source/flow cases](evidence/shallow-v4/flow-cases.json) are adapter conformance inputs. |
| 10. Registry policy left to implementers | Concrete [registry](evidence/shallow-v4/builtin-registry.json), supplemented by the [adapter contract](shallow-v4-adapter-contract.md), contains exact symbol/signature guards, alias/effect/failure summaries, policy positions and positive/negative source fixtures. This bounded initial allowlist replaces broad library suggestions; unlisted behavior remains unknown. |
| 11. Unbounded aggregate retention/work | Flow contract §§6–7: aggregate work/payload limits, interned summaries, bounded queue/workers, spill and graph-shape tests. Total process memory is separately benchmarked. |
| 12. Performance has no failure thresholds | Spec §8: fixed reference runner, trial counts, median/p95 startup/warm/edit gates, idle work and process-tree memory caps. These are required acceptance budgets, not measured results. |
| 13. Useful default overload worsens score | Spec §1: minimize usable route cost over an unchanged exposed slot universe; old route remains eligible. Required-policy/default fixture changes 37→28. |
| 14. Bypass borrows validation | Spec §2 and flow contract §5: route alternatives preserve V only where established; validated/bypass family gets X credit, not unconditional V. Numeric and source cases. |
| 15. Conditional and dispatch disagree | Same alternative semantics for both forms. Paired conditional/receiver-dispatch fixtures require the same H and local selector burden. |
| 16. Combining files reduces SCORE | Same correction as #8. Layout fixture explicitly shows projection sums 10, 7 and 13 while unique contribution remains 10 and improvement is false. |
| 17. Warm cache bypasses analysis budget | Flow contract §6: charge logical summary consumption regardless of residency. Cold/warm budget-exhaustion fixtures withhold both scores; scheduling/cache parity required. |
| 18. Recursive substitutions never stabilize | Flow contract §2: finite access paths, aliases, receivers, alternatives, facts, bindings and recipes; context-insensitive parameterized summaries and monotone widening. Recursive-access and SCC scale cases. |
| 19. One update hides large set cost | Flow contract §6 charges member visits for aliases, bindings, obligations and set operations, including restoration. Wide-fact/alias and fan-in scale cases exercise accounting. |
| 20. jname limitation buried | Moved to opening scope in both overview and specification; source-complete numeric fixture still mandatory. |
| 21. Conflicting implementation sequences | Overview groups are delivery outcomes. Spec §9 is the sole execution order; new default follows reporting/verifier integration. |
| 22. Performance mixed with profiles | Spec §7 covers profiles/migration; §8 separately owns caching and performance, linked to finite-domain/resource rules. |

## Validation boundary

JSON shape, golden arithmetic, route selection, obligation-set unions and ledger
invariance can be checked now. Source fixtures describe required adapter behavior;
they are not evidence that an unimplemented flow analyzer already passes. Benchmark
budgets likewise require actual measurements during implementation. No additional
research panel or calibration study is a prerequisite.
