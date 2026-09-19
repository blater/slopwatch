# Structural responsibility correction

## Reproduced baseline and preserved criteria

The preserved r38 packaged executable reproduces the exact Java reports:
unused `open`/`close` locals score 13 while `first`/`last` score 43;
implicit field update scores 13/H2 while `this` qualification scores 8/H4.
These are irrelevant-name and duplicate-behavior defects, not calibration choices.
Production numeric weights and both original frozen manifests remain unchanged.
Prior guard, exact xmltoaster/NQL lookup, numeric publication, uncertainty metadata,
fix-safety and positive-detection acceptance remain binding.

## Holdout investigation before calibration

The previously observed Java holdout is now evaluated in three distinct modes:

1. Exact isolated target snapshots, as originally evaluated.
2. Exact targets plus the retained caller witnesses from the same repository.
3. Full live repository analysis, after verifying target/witness bytes still match
   their frozen hashes. Analyzed source inventories, hashes, command and coverage
   are recorded so context changes cannot be mistaken for model changes.

The r38 baseline produces 0/22/64/80 in all three modes, in the manifest's order.
Thus these runs do not support attributing the misses solely to missing caller
witnesses, although the original isolated result by itself could not establish
full-workspace behavior. No expected labels, relations, snapshots or first-run
artifacts were changed. Contextual re-evaluation of observed cases is diagnostic
regression evidence, not a fresh blind holdout.

Source investigation identifies distinct model gaps:

| Boundary | Structural evidence currently missed |
| --- | --- |
| SqlBoundAccess | Package-visible mutable fields and joint caller-installed representation; separate assignments evade same-statement coupling. |
| SqlDescriptorScanContext | Package audience and admission/open/cleanup phases coordinated in separate callers. |
| HierarchyRowContext | Callback creation, attachment and returned/cached identity under collection contracts. |
| SqlRowCursor | Typed JDBC ownership and independent cleanup attempts, plus row projection through loops/helpers. |

These are not grounds for class-name exceptions, field-count credit or a weight
adjustment. A complete fix requires general audience, caller-effect and callback/
library-contract analysis with adversarial cases. The current correction targets
the demonstrated false resource evidence and equivalent-storage duplication.

## Review required for future weight changes

The retained `calibration-review-baseline.json` has the unchanged production
weights. The Make/CI safeguard rejects a numeric change without a reviewed report
bound to baseline/proposed profile identities and hashed evidence for score deltas,
important ranking changes, mechanism coverage and fresh independently reviewed
examples. Its review fields are attestations, not authenticated approval. Human
review must inspect the evidence; the tool does not declare arbitrary artifacts
semantically adequate. See [calibration policy](../../../../go/internal/sourceestimate/CALIBRATION.md).

Sensitivity observations remain observations. Near-equal rank exchanges are not
automatically defects; large shifts and sparse mechanism coverage still prevent
claims of robust weights. No weights are adjusted in this correction.

## Reproduction commands

- `make test-shallow-regressions`
- `make test-shallow-sensitivity test-shallow-safeguards`
- `python3 tools/shallow_graded_acceptance.py --benchmark docs/evidence/shallow-v4/graded-benchmark.json`
- `python3 tools/shallow_graded_acceptance.py --benchmark docs/evidence/shallow-v4/graded-real-benchmark.json`
- `python3 tools/shallow_holdout_context.py --output build/shallow-context-full.json`
- `make test-shallow-context` evaluates only frozen targets/witnesses for CI.

The Java-only observed holdout does not establish empirical generalization in
Go, TypeScript or Rust. Cross-language mechanism tests serve a different purpose.
Enterprise 30K cold/warm/incremental performance acceptance remains open.

## Final r39 results

Production weights are unchanged. Resource-name decoys now score **43/43**, with
zero resource credit; implicit and explicit field updates score **13/13**, with
hidden responsibility **2/2**. Recognition connects resolved acquisition, observable
use and protected cleanup. Storage effects are normalized before overlapping
responsibility is deduplicated. Cross-language regressions cover renaming, unused
decoys, wrappers, helper extraction, aliases, shadowing and equivalent field syntax.
Resource recognition remains bounded: connected Boolean protocol effects and
same-type Rust captured guards are supported; unresolved external contracts remain
estimated with limitations. This is not general ownership or path verification.

All **60 synthetic and 12 real acceptance cases pass**, with frozen expectations
and hashes unchanged. The guard remains 10→10; the actual xmltoaster conditional/
newValue case and distinct NQL switch/getScalarKind case retain separate passing
coverage. Source-estimator, native, report, scoring, follow and fix native-adapter
package tests pass. Packaged build, ten Python safeguard tests, calibration identity
and unchanged-weight checks pass. Two independent structural review passes exposed
additional path/alias counterexamples; these received regression tests and fixes,
and the final targeted blind review was clean. Command-level harness tests also
verify actual witness inclusion and retained failures.

The original Java holdout remains **0/22/64/80 in all three modes**, failing four
bands and two of three rankings, including both high cases. Adding the retained
witnesses or full workspace did not repair these misses. The context and holdout
commands intentionally return failure; passing regression checks do not override
these results. See the [before context record](structural-holdout-context-before.json)
and [after context record](structural-holdout-context-after.json).

A separately selected and frozen Go/TypeScript/Rust corpus passes **5/6 bands and
2/3 rankings**. Rust surface collection scores 0 against its unchanged 26–64 band,
and ranks below the parser. No model changes followed this first evaluation.
The corpus contains no high-band cases, so it does not establish cross-language
real-source positive detection. Selection was independent of scores; its disclosed
incidental exposure to legacy TypeScript scorer source is recorded in
[selection notes](structural-fresh-holdout/selection.md). The immutable
[first evaluation](structural-fresh-holdout/first-evaluation.json) retains failures.

### Broader deltas and sensitivity

On identical workspace source inventories, River has 264 changed file ratings:
18 increases and 246 decreases. NQL has two decreases; xmltoaster has none.
Examples include RelationalDescriptorDropPublications 80→24,
SqlUniversalJoinMetrics 88→45 and SqlDescriptorPointInsertExecution 60→23.
Read-only inspection finds implicit owned counter increments/decrements in all
three, consistent with the newly normalized storage recognition. This explains a
plausible mechanism, not independent approval of every changed rating. The full
per-file deltas are retained in the [results record](structural-responsibility-results.json).
No claim that every real-world rating improved follows from these changes.

The final 66-model × 72-case sensitivity run records 49 range escapes,
**23 invariance violations**, 232 pairwise rank reversals and maximum absolute
score change 31; direction and guard violations are zero. The prior report had
one invariance violation, so that diagnostic worsened despite default acceptance
passing. These observations remain unresolved calibration concerns. The passing
sensitivity command does **not** establish robust weights. See
[full sensitivity results](structural-sensitivity-results.json). Future weight
changes require the reviewed change report described above and fresh examples.

### Numeric publication and remaining acceptance

| Analyzed source | Numeric / analyzed | Not applicable | Missing applicable |
| --- | ---: | ---: | ---: |
| River | 2,153 / 2,207 | 54 | 0 |
| NQL | 276 / 280 | 4 | 0 |
| Xmltoaster | 84 / 84 | 0 | 0 |
| Current Slopwatch workspace | 649 / 649 | 0 | 0 |

Slopwatch includes benchmark/witness snapshots and added test files; its count is
not an identical-input comparison with the earlier 634-file sample. Its current
language counts are Go 519/519, TypeScript 23/23, Java 91/91 and Rust 16/16.
Estimation remains explicitly reported; numeric coverage does not imply complete
semantic evidence. River's 54 not-applicable files remain visible.

The structural corrections and investigation are delivered; the original holdout,
fresh Rust miss, perturbation failures, missing fresh high-band evidence and
enterprise 30K cold/warm/incremental acceptance remain open. A known pre-existing
analysiscache nil/empty Depth-map round-trip failure is outside this correction;
there is no claim that the entire repository test suite passes.
