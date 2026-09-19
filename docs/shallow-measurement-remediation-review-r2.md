# Review of SHALLOW remediation plan, revision 2

Date: 2026-09-16.

Reviewed the plan and current scoring, report, fix-verification, cache and unit-planning code. This is a design review, not validation of an implemented replacement. The plan was not changed. No project-context.md matched the skill's configured persistent-facts glob; review used the plan, repository code and session requirements.

Revision 2 explicitly addresses the previous review's coverage/comparability concerns. The remaining findings below are missing design decisions or acceptance criteria; they do not claim the proposed replacement is already implemented. Define these before the affected milestone. The findings-only direction and separate scalar validation gate remain sensible.

## Adversarial findings

### docs/shallow-measurement-remediation-plan.md:220

- **Trigger:** Multiple build artifacts exist, but no artifact is explicitly selected.
- **Amendment:** Define artifact discovery, stable identity, default selection and ambiguity reporting in B0; add a multi-artifact monorepo fixture.
- **Consequence:** The same slopmark . invocation can yield different audiences and inventories under different implementations.

### docs/shallow-measurement-remediation-plan.md:210

- **Trigger:** A consumer uses an exported callable value or factory-returned anonymous interface.
- **Amendment:** Include callable exported values and anonymous consumer-facing views with stable access-path identities and fixtures.
- **Consequence:** The named-type/free-function rules omit legitimate APIs or misclassify them as data-only.

### docs/shallow-measurement-remediation-plan.md:253

- **Trigger:** An ordinary project has no hand-authored task cards.
- **Amendment:** Define card provisioning, boundary attachment, allowed recognizer inputs and the no-matching-card result; verify that a card alone cannot prove its asserted obligation.
- **Consequence:** Success on curated fixtures need not demonstrate an operational assessment for real repositories.

### docs/shallow-measurement-remediation-plan.md:402

- **Trigger:** Several workers approach their individual 1 GiB allowances.
- **Amendment:** Add a process-wide memory/concurrency budget, including subprocesses and retained summaries, with a mixed-language acceptance test.
- **Consequence:** Individually compliant workers can collectively exhaust system memory.

### docs/shallow-measurement-remediation-plan.md:402

- **Trigger:** A worker is terminated before emitting a partial result.
- **Amendment:** Require coordinator-generated budget diagnostics, preservation of completed unrelated results and continued analysis; test forced worker termination.
- **Consequence:** One exhausted worker can fail the scan instead of producing the promised partial assessment.

### docs/shallow-measurement-remediation-plan.md:403

- **Trigger:** Transient machine load causes a timed partial result that is cached under unchanged inputs.
- **Amendment:** Define bounded retry/refresh and cache eligibility for transient resource failures, separately from deterministic unsupported constructs.
- **Consequence:** A temporary resource shortfall can persist indefinitely as an unknown assessment.

### docs/shallow-measurement-remediation-plan.md:408

- **Trigger:** Cold/warm benchmarks do not exercise continuous polling and incremental edits in a 30K–80K-file repository.
- **Amendment:** Add 30K/80K no-change, implementation-edit, public-contract-edit and dependency-summary-change scenarios; measure invalidation breadth and work as well as latency.
- **Consequence:** The benchmark gate can pass despite workspace-scale work on each poll or small edit.

### docs/shallow-measurement-remediation-plan.md:402

- **Trigger:** One supported TypeScript project exceeds 10,000 source files.
- **Amendment:** Define oversized-unit support or boundary-preserving partitioning, and test and publish its coverage envelope.
- **Consequence:** A target enterprise workload can remain permanently partial even on a capable machine.

### docs/shallow-measurement-remediation-plan.md:367

- **Trigger:** A findings-only release encounters an existing numeric SHALLOW/deep goal.
- **Amendment:** Choose explicit legacy-profile execution or actionable rejection, and test migration of saved numeric thresholds and fix jobs.
- **Consequence:** Versioning SCORE alone leaves old goals with ambiguous, silently legacy or indefinitely unavailable behavior.

### docs/shallow-measurement-remediation-plan.md:456

- **Trigger:** Many validation pairs belong to the same abstraction family or project.
- **Amendment:** Specify family/project-clustered uncertainty and minimum independent family/project counts in addition to pair counts.
- **Consequence:** One hundred pairs can provide far fewer independent observations than reported uncertainty assumes.

## Edge-case findings

These independently overlap the adversarial findings on anonymous APIs, incremental work and aggregate resource limits. The overlap is retained intentionally.

### docs/shallow-measurement-remediation-plan.md:210

- **Trigger:** An exported object literal or factory exposes operations without a named type.
- **Amendment:** Define anonymous callable-object views and stable export/factory identities, or explicitly mark the inventory partial.
- **Consequence:** Valid APIs disappear from the inventory or become data-only.

### docs/shallow-measurement-remediation-plan.md:396

- **Trigger:** One source edit invalidates behavioral summaries in a 30K–80K-file monorepo.
- **Amendment:** Require persisted summary dependencies, affected-unit invalidation and idle/single-edit benchmarks forbidding whole-source rescans during polling/rendering.
- **Consequence:** Warm-run tests pass while interactive refresh resolves or scans the whole repository.

### docs/shallow-measurement-remediation-plan.md:402

- **Trigger:** Many analysis units concurrently approach their individual limits.
- **Amendment:** Define unit partitioning, cross-unit summary reuse, bounded concurrency and aggregate process-memory limits.
- **Consequence:** Workers exhaust memory before partial results can be reported.

## Structure findings

| Pass | Location | Change | Reason | Word impact |
| --- | --- | --- | --- | --- |
| structure | docs/shallow-measurement-remediation-plan.md:518 | **MOVE:** Move the delivery-gates table and independence/prototype paragraph immediately after Intended outcome; keep review disposition at the end. | Readers need delivery scope before the detailed evidence and contract. | 0 words |
| structure | docs/shallow-measurement-remediation-plan.md:155 | **CONDENSE:** Keep the archive link, replay command and essential provenance qualification; place complete probe examples in the linked archive. | The reproduction detour interrupts the transition from diagnosis to proposed remedy. | Estimated saving 132–152 words from a 222-word section. |

## Code checks supporting the review

- `go/internal/scoring/metrics.go`: `deep` remains numeric, sum-aggregated and available whenever its component exists. This confirms the diagnosis and makes legacy-goal migration concrete.
- `go/internal/scoring/projection.go`: SCORE sums projected contributions and valid-zero depends on file completeness. Revision 2's explicit coverage rules are needed.
- `go/internal/fixanalysis/nativeadapter/verification.go` and `verification_checks.go`: focused SCORE bypasses component-completeness checks. Revision 2 explicitly covers this; it is not counted as a new finding.
- `go/internal/native/cache_execution.go`: language workers run concurrently; an error cancels other workers and the coordinator returns an error instead of partial results. Forced resource termination needs an explicit acceptance case.
- `go/internal/native/cache_coordinator_flow.go`: matching valid artifacts are reused. The plan must define how newly introduced transient partials participate in this lifecycle; this is a future integration requirement, not a claim that current budget-partial artifacts exist.
- `go/internal/unitplan/typescript_plan.go`: a typed unit contains the files owned by a tsconfig project, making a 10,000-file unit limit relevant to enterprise workloads.
- `go/internal/analysiscache/key.go` and `go/internal/native/cache_fingerprint.go`: dependency fingerprints already support dependency-aware invalidation. Preserve and test that property when adding behavioral summaries.

## Machine-readable findings

```json
[
  {
    "lens": "adversarial",
    "location": "docs/shallow-measurement-remediation-plan.md:220",
    "trigger_condition": "Multiple build artifacts exist, but no artifact is explicitly selected.",
    "guard_snippet": "Define artifact discovery, stable identity, default selection and ambiguity reporting in B0; add a multi-artifact monorepo fixture.",
    "potential_consequence": "The same slopmark . invocation can yield different audiences and inventories under different implementations."
  },
  {
    "lens": "adversarial",
    "location": "docs/shallow-measurement-remediation-plan.md:210",
    "trigger_condition": "A consumer uses an exported callable value or factory-returned anonymous interface.",
    "guard_snippet": "Include callable exported values and anonymous consumer-facing views with stable access-path identities and fixtures.",
    "potential_consequence": "The named-type/free-function rules omit legitimate APIs or misclassify them as data-only."
  },
  {
    "lens": "adversarial",
    "location": "docs/shallow-measurement-remediation-plan.md:253",
    "trigger_condition": "An ordinary project has no hand-authored task cards.",
    "guard_snippet": "Define card provisioning, boundary attachment, allowed recognizer inputs and the no-matching-card result; verify that a card alone cannot prove its asserted obligation.",
    "potential_consequence": "Success on curated fixtures need not demonstrate an operational assessment for real repositories."
  },
  {
    "lens": "adversarial",
    "location": "docs/shallow-measurement-remediation-plan.md:402",
    "trigger_condition": "Several workers approach their individual 1 GiB allowances.",
    "guard_snippet": "Add a process-wide memory/concurrency budget, including subprocesses and retained summaries, with a mixed-language acceptance test.",
    "potential_consequence": "Individually compliant workers can collectively exhaust system memory."
  },
  {
    "lens": "adversarial",
    "location": "docs/shallow-measurement-remediation-plan.md:402",
    "trigger_condition": "A worker is terminated before emitting a partial result.",
    "guard_snippet": "Require coordinator-generated budget diagnostics, preservation of completed unrelated results and continued analysis; test forced worker termination.",
    "potential_consequence": "One exhausted worker can fail the scan instead of producing the promised partial assessment."
  },
  {
    "lens": "adversarial",
    "location": "docs/shallow-measurement-remediation-plan.md:403",
    "trigger_condition": "Transient machine load causes a timed partial result that is cached under unchanged inputs.",
    "guard_snippet": "Define bounded retry/refresh and cache eligibility for transient resource failures, separately from deterministic unsupported constructs.",
    "potential_consequence": "A temporary resource shortfall can persist indefinitely as an unknown assessment."
  },
  {
    "lens": "adversarial",
    "location": "docs/shallow-measurement-remediation-plan.md:408",
    "trigger_condition": "Cold/warm benchmarks do not exercise continuous polling and incremental edits in a 30K–80K-file repository.",
    "guard_snippet": "Add 30K/80K no-change, implementation-edit, public-contract-edit and dependency-summary-change scenarios; measure invalidation breadth and work as well as latency.",
    "potential_consequence": "The benchmark gate can pass despite workspace-scale work on each poll or small edit."
  },
  {
    "lens": "adversarial",
    "location": "docs/shallow-measurement-remediation-plan.md:402",
    "trigger_condition": "One supported TypeScript project exceeds 10,000 source files.",
    "guard_snippet": "Define oversized-unit support or boundary-preserving partitioning, and test and publish its coverage envelope.",
    "potential_consequence": "A target enterprise workload can remain permanently partial even on a capable machine."
  },
  {
    "lens": "adversarial",
    "location": "docs/shallow-measurement-remediation-plan.md:367",
    "trigger_condition": "A findings-only release encounters an existing numeric SHALLOW/deep goal.",
    "guard_snippet": "Choose explicit legacy-profile execution or actionable rejection, and test migration of saved numeric thresholds and fix jobs.",
    "potential_consequence": "Versioning SCORE alone leaves old goals with ambiguous, silently legacy or indefinitely unavailable behavior."
  },
  {
    "lens": "adversarial",
    "location": "docs/shallow-measurement-remediation-plan.md:456",
    "trigger_condition": "Many validation pairs belong to the same abstraction family or project.",
    "guard_snippet": "Specify family/project-clustered uncertainty and minimum independent family/project counts in addition to pair counts.",
    "potential_consequence": "One hundred pairs can provide far fewer independent observations than reported uncertainty assumes."
  },
  {
    "lens": "edge-case-hunter",
    "location": "docs/shallow-measurement-remediation-plan.md:210",
    "trigger_condition": "An exported object literal or factory exposes operations without a named type.",
    "guard_snippet": "Define anonymous callable-object views and stable export/factory identities, or explicitly mark the inventory partial.",
    "potential_consequence": "Valid APIs disappear from the inventory or become data-only."
  },
  {
    "lens": "edge-case-hunter",
    "location": "docs/shallow-measurement-remediation-plan.md:396",
    "trigger_condition": "One source edit invalidates behavioral summaries in a 30K–80K-file monorepo.",
    "guard_snippet": "Require persisted summary dependencies, affected-unit invalidation and idle/single-edit benchmarks forbidding whole-source rescans during polling/rendering.",
    "potential_consequence": "Warm-run tests pass while interactive refresh resolves or scans the whole repository."
  },
  {
    "lens": "edge-case-hunter",
    "location": "docs/shallow-measurement-remediation-plan.md:402",
    "trigger_condition": "Many analysis units concurrently approach their individual limits.",
    "guard_snippet": "Define unit partitioning, cross-unit summary reuse, bounded concurrency and aggregate process-memory limits.",
    "potential_consequence": "Workers exhaust memory before partial results can be reported."
  },
  {
    "lens": "structure",
    "location": "docs/shallow-measurement-remediation-plan.md:518",
    "action": "MOVE",
    "finding": "Move the delivery-gates table and independence/prototype paragraph immediately after Intended outcome; keep review disposition at the end.",
    "reason": "Readers need delivery scope before the detailed evidence and contract.",
    "word_impact": "0 words"
  },
  {
    "lens": "structure",
    "location": "docs/shallow-measurement-remediation-plan.md:155",
    "action": "CONDENSE",
    "finding": "Keep the archive link, replay command and essential provenance qualification; place complete probe examples in the linked archive.",
    "reason": "The reproduction detour interrupts the transition from diagnosis to proposed remedy.",
    "word_impact": "Estimated saving 132–152 words from a 222-word section."
  }
]
```

