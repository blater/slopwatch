# SHALLOW v4 usage and current coverage

SHALLOW v4 is the selected default for the interim release. Semantic coverage is
still being expanded. This is not the release acceptance record.
See the [interim release boundary and capability backlog](shallow-v4-interim-release.md)
for what is available, the River coverage limitation and planned C/Y/semantic work.

```sh
build/slopmark --score-profile=responsibility-v4 .
build/slopwatch --score-profile=responsibility-v4 .
```

Use `--score-profile=legacy-signature-v3` to reproduce the previous metric for
comparison. Its catalog and cache identity remain separate from v4.
`--shallow-profile` remains an alias for profile selection. If both flags are
supplied, they must name the same profile. Unsupported v4 measurements never
silently fall back to legacy.

The profile measures interface burden against evidenced hidden responsibility.
It uses the shared evaluator across Go, Java, Rust and TypeScript. Other metric
definitions, weights and the aggregate SCORE formula remain unchanged. Changing
SHALLOW can change aggregate SCORE through its existing contribution.

A measured or estimated applicable boundary has a numeric SHALLOW value. `N/A`
requires absence of an applicable implementation, not unsupported semantic
analysis. Source-based estimates preserve numeric sorting, exports and SCORE;
their details distinguish observed and estimated responsibility. Every zero
identifies its role, lowest-range assessment or conservative uncertainty reason.
See the [r36 graded rubric](components/module_shallowness.md) and
[complete benchmark and repository results](evidence/shallow-v4/graded-delivery-results.md).

The current Java supporting-contract rule recognises internal providers using
resolved contract implementations, production bindings and actual contract use.
It does not use class names or suffixes. Public exposure or incomplete exposure
analysis prevents the exemption. The River `HeapPendingRowChunkAllocator` example
has been checked against its `PendingRowArena` binding and use.

Profile and policy identities separate cached analysis. TypeScript v4 also
fingerprints the shared evaluator it invokes. Warm-cache correctness, edit
invalidation, resource limits and River performance remain release acceptance
requirements. The full implementation and remaining language/recognizer scope are
specified in [the implementation spec](shallow-v4-implementation-spec.md) and
[the role backlog](shallow-v4-legitimate-roles-backlog.md).
