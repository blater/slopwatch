# Fresh structural source holdout

Independent source/caller review using P0 directive and holdout rubric. No score execution or results inspection. Prior manifests used to exclude origins/hashes. Bounded historical scorer-source exposure is disclosed in limitations.

Frozen before evaluation. Three low and three mixed expectations; no high cases are asserted. Higher SHALLOW means more caller burden relative to hidden useful responsibility. Ranges follow the existing independent holdout bands.

| Case | Language | Band | Range |
| --- | --- | --- | --- |
| go-userdata-root | go | low | 0–25 |
| go-fix-subscription | go | mixed | 26–64 |
| typescript-source-context | typescript | mixed | 26–64 |
| typescript-public-operations | typescript | low | 0–25 |
| rust-program-parser | rust | low | 0–25 |
| rust-surface-collector | rust | mixed | 26–64 |

## go-userdata-root

Hidden responsibilities: Resolves home and platform policy, validates absolute usable paths, normalizes paths and applies the Linux XDG fallback behind one parameterless operation.

Caller burden: Caller handles an error, chooses a child directory and creates it; Root deliberately does not own creation.

analysiscache/store.go DefaultRoot calls Root then joins analysis; NewStore separately creates and protects the cache. The boundary hides platform and environment policy, not filesystem lifecycle.

## go-fix-subscription

Hidden responsibilities: Synchronizes notification capture, advances the captured channel after a notification, handles cancellation/closure and makes Close idempotent.

Caller burden: Caller owns subscribe/wait/read snapshot/rearm/close ordering and recovery after errors. Notification does not deliver a snapshot atomically.

follow/fix_subscription.go wait calls Wait then service.Jobs; next rearms, close closes, retry closes/resubscribes and refreshes. Useful synchronization is hidden, but observable refresh and recovery phases remain outside.

## typescript-source-context

Hidden responsibilities: Validates source containment and language, deduplicates ownership, loads and parses sources, records inventory failures and sorts sources; delegates typed program creation.

Caller burden: Callers separately create syntax and typed contexts, reconcile inventory failures, inspect typed availability and diagnostics, and update exposed mutable parse-count/program-created bookkeeping through typed-context.ts.

analyzer.ts creates AnalysisContext and checks inventoryIssues before createTypedContext; typed-context.ts writes owner.typedParseCounts and owner.typedProgramCreated. This is an operational context with useful admission duties but split phases and shared bookkeeping, not merely a passive result.

## typescript-public-operations

Hidden responsibilities: Walks exported declarations, dispatches classes/interfaces/variables, filters public members, recognizes accessor representation and accumulates coherent public-surface facts.

Caller burden: Caller supplies a parsed SourceFile and consumes the returned aggregate; helper functions and accumulator updates are private. Reexported role classification is separate.

structural.ts moduleShallowness calls publicOperations(entry.sourceFile) once and consumes its result. The returned mutable result has no required repair operation or coupled caller-maintained invariant; its shape alone is not burden.

## rust-program-parser

Hidden responsibilities: Canonicalizes workspace and source paths, blocks escapes, deduplicates requests, reads/parses inputs, retains per-file failures, orchestrates cross-file declarations and functions, normalizes and sorts results.

Caller burden: Caller supplies workspace, source paths and include-tests/depth policy, handles outer error and per-file failure metadata. Caller does not manage file handles or coordinate declaration/function passes.

main.rs response invokes parse_program once and wraps its Result. Useful file admission, failure partitioning and ordered analysis orchestration are hidden by the entry point. Size and branching are not the reason for the low expectation.

## rust-surface-collector

Hidden responsibilities: Traverses declarations and nested inline modules, extracts public function/method/trait signatures, recursively constructs type shapes, and records public representation fields.

Caller burden: Caller parses syntax, supplies matching path, owns and passes two mutable output vectors, coordinates accumulation over files and sorts public operations afterward. Collect appends without resetting or normalizing caller state.

parser.rs invokes surface::collect with program.public_operations and program.representation during its file loop, then sorts public_operations. Extraction is substantive; residual two-output accumulation and normalization coordination justify mixed rather than high.

## Relationships

- go-fix-subscription > go-userdata-root by at least 5: The notification caller retains wait/read/rearm/close and recovery phases; root resolution hides its platform policy in one call.
- typescript-source-context > typescript-public-operations by at least 5: The context exposes split creation phases and mutable bookkeeping; the collector owns traversal and aggregate construction.
- rust-surface-collector > rust-program-parser by at least 5: The collector requires caller-owned output accumulation and normalization; parse_program owns those passes and source admission.

## Limits

- Purposive six-file sample from one repository: two cases each in Go, TypeScript and Rust. No fresh high examples; existing Java high-detection requirements remain unchanged.
- Ranges judge file/module boundaries and caller burden, not overall component quality. Some files depend on out-of-snapshot helpers and third-party AST APIs; incomplete evidence must remain visible.
- Source-only review did not invoke scoring or inspect scored results or sourceestimate implementation. Caller tracing exposed structural.ts lines 875-905, the beginning of legacy moduleShallowness feature aggregation; strict blindness to all historical scorer source is therefore not claimed.
- Prior manifest exclusion inspection accidentally printed embedded synthetic source alongside names/hashes; no expected ranges or scores were printed in that inspection. No prior targets are reused.
- Witnesses are review evidence only, not scored target inputs. Their inclusion does not establish complete compilation or semantic context.
- After first evaluation, failures remain failures and ranges/relations remain frozen. Tuning against this sample consumes its holdout status.
