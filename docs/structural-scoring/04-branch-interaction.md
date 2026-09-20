# Plan 4: Bounded branch-interaction evidence

Status: experiment in progress · Parent: [epic](epic.md) · Dependency: [plan 3](03-continuous-severity.md)

## Outcome

Distinguish independent alternatives from decisions coupled through nesting or
mutable state when the evidence supports that distinction.

## Work

1. Use remaining benchmark failures to select the smallest useful evidence:
   terminating guards, independent dispatch, or reads dependent on earlier
   branch writes. Do not introduce a parser exemption or name-based heuristic.
2. Prototype with existing AST walks and indexes. Bound time and evidence size
   against source size and dependency edges; avoid path enumeration and repeated
   body scans. Record unresolved aliases and unsupported flow explicitly.
3. Evaluate how the evidence adjusts the shared control-flow contribution.
   Avoid introducing another additive penalty for the same branches. Missing
   evidence retains the prior scoring path with limitations, not a discount.
4. Compare against plan 3 on frozen held-out cases across all four languages.
   Measure cold, warm, incremental and idle behavior with the existing 30,000-file
   workload harness, recording runtime, peak memory and evidence size.
5. Ship only if predeclared ranking and performance criteria pass. Otherwise
   retain plan 3, remove unused prototype code and record the failed hypothesis.

## Acceptance and tests

- Reuse analyzer fixtures for dispatch, guards and shared-state mutations;
  add only missing cases. Preserve existing raw-metric conformance assertions.
- Reuse the shared scoring benchmark and current scaling tests. Avoid broad
  snapshot updates or a separate experimental test framework.
- For shipped evidence, version analyzer/policy identities and verify cached
  results, reports and reweighting through existing integration coverage.
- `make build` passes. Record either the validated improvement or a concise
  no-ship decision; neither outcome waives the P0 performance requirements.
