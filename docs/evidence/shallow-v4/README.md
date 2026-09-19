# SHALLOW v4 r6 design fixtures

These are normative implementation inputs for the [specification](../../shallow-v4-implementation-spec.md).

Run `make test-shallow-adapters` from the repository root to execute every
isolated source variant through the packaged CLI. The command checks the
normative state, burden, responsibilities, score and conditional-validation
assertions and returns nonzero for any mismatch. It writes
`build/shallow-adapter-acceptance.json`, with separate numeric-coverage and
conformance totals; unsupported cases are failures, not skipped tests.
Use `python3 tools/shallow_adapter_acceptance.py --languages go,rust
--cases default_route,validation_bypass` for a focused run against an existing
build. Recursive-path cases also compare uncached, cold-cache and warm-cache
results. Creation assertions use the CLI's `raw.family_hidden` per-family
responsibility totals, rather than inferring creation credit from aggregate H.

- `scoring-cases.json`: 36 numeric/state cases, including route selection and boundary-wide obligation alternatives; one ledger case covers three file layouts.
- `flow-cases.json`: 13 source scenario families and expected normalized flow/fact slices. Language-specific failures may add edges; they must not erase required evidence.
- `adapter-cases.json`: 13 common cases with concrete Go, Java, Rust and TypeScript variants; exact scores where interfaces match and explicit category/state assertions elsewhere.
- `builtin-registry.json`: 40 initial built-in summaries with guarded symbol/signature matching and positive/negative source fixtures.
- `scoring-cases-reviewed-r5.json`: historical reviewer baseline, not the current policy.

On 2026-09-17, a local independent arithmetic check recomputed B8, alternative-set
union/minimum H, integer rounding and route choices for the supplied scoring cases.
It also checked unique boundary totals versus all three projection layouts, unique
registry/flow IDs, required registry fixture fields and current document links.
These checks passed. They do not execute source fixtures through adapters or
establish benchmark performance; those remain implementation acceptance work.

The [adapter contract](../../shallow-v4-adapter-contract.md) is the per-language
lowering and edge-case checklist. Recurrence handling and concrete error/buffer
summaries close gaps exposed when translating the source cases. Source compilation
checks validate fixture inputs only; they are not SHALLOW conformance results.

Adapter-source validation on 2026-09-17 passed for all 59 variants:
14 Go source units (`go test` on isolated files, no test functions),
15 Java source units (`javac -proc:none`),
14 Rust source units (`rustc --edition=2021 --crate-type=lib --emit=metadata`),
and 16 TypeScript source units (local TypeScript 5.9.2 compiler,
strict, ES2022, no emit). Temporary compiler output stayed outside the repository.
JSON parsing, four-language coverage, unique registry IDs, document links and
whitespace checks also passed. No runtime fixture functions were invoked.
