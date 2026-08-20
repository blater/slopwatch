# Slopscout TypeScript analyzer

This is the isolated, read-only TypeScript analyzer described by
`docs/multilanguage-architecture-plan.md`. It parses every exact source once in
one invocation-scoped `AnalysisContext`, memoizes the source inventory, and
runs all requested syntax kernels over that context. Typed kernels share one
additional TypeScript compiler `Program`; this second parser/index mode is
reported in the `execution_plan` record.

## Protocol v1 subset

The executable reads one UTF-8 JSON value (a one-record NDJSON stream is the
same thing) from stdin and writes NDJSON to stdout. The accepted request is:

```json
{
  "type": "request",
  "protocol_version": 1,
  "invocation_id": "run-1",
  "workspace": "/absolute/workspace",
  "units": [
    {
      "unit_id": "web",
      "language": "typescript",
      "source_paths": ["src/a.ts", "src/b.ts"],
      "metadata": {}
    }
  ],
  "components": [
    {"component_id": "cognitive_complexity", "definition_version": "pmd-sonar-v1"}
  ],
  "options": {"typescript_types": "auto", "tsconfig": "tsconfig.json"},
  "limits": {}
}
```

Only exact `.ts`, `.tsx`, `.mts`, and `.cts` production files are accepted;
declaration files and paths outside the canonical workspace are rejected.
Discovery and ownership planning belong to the language provider, not this
analyzer. A source may have only one unit owner in an invocation.

The response contains diagnostics, measurements, coverage, execution plans,
and exactly one terminal record. Measurements are raw and
unweighted. NPath values larger than JavaScript's safe integer range use
`{"$integer":"..."}`. Every requested component has explicit per-file
coverage, including successful zero-finding analysis.

`typescript_types` behaves as follows:

- `auto`: keep structural results and mark typed components unavailable when
  TypeScript reports an untrustworthy graph.
- `require`: do the same but fail the affected analysis unit.
- `off`: typed components must not be requested; requesting them is an error
  with unavailable coverage.

Repository ESLint configuration and package scripts are never loaded or run.
The compiler may resolve already-installed declarations but this analyzer never
installs target-project dependencies.

## Measurement contracts

Structural components are `cognitive_complexity/pmd-sonar-v1`,
`cyclomatic_method_complexity/pmd-v1`, `npath_complexity/pmd-v1`, and
`deeply_nested_if/pmd-v1`. The traversal follows the pinned PMD 7.26 control
flow contracts: method entry, control statements, short-circuit paths,
ternaries, abrupt completion, switch alternatives, nesting, else-if treatment,
recursion, and problem depth 3. TypeScript constructs with no Java equivalent
are explicit exceptions: optional chaining and nullish coalescing are linear;
async suspension and generator yields add no decision; TypeScript nested
functions are independent cyclomatic/NPath subjects while cognitive traversal
also treats them as nesting structures.

Typed findings use `typescript-local-sink-v1` and the key
`(component, canonical file, start, end, normalized symbol)`. Candidate
precedence is deterministic (call over member, argument over assignment over
declaration, return over public API declaration). Chained member/call sinks are
collapsed to the outer local sink. This deliberately does not claim
interprocedural taint or origin tracking.

- `explicit_any` finds explicit `any` syntax.
- `unsafe_type_assertion` finds any-crossing, incompatible, and risky non-null
  assertions.
- `unsafe_type_propagation` finds local assignment and argument flow from
  `any` into a non-`any` sink.
- `unsafe_type_use` finds calls and outermost member access on `any`.
- `unsafe_type_boundary` finds unsafe returns and `any` in exported/public API.
- `ambiguous_boolean_expression` finds non-boolean control conditions.
- `non_exhaustive_union` finds switches missing literal/enum union members when
  there is no default.

Compiler errors make all typed components unavailable because their inferred
facts are not trustworthy. The current local model intentionally does not
perform cross-file taint propagation, model conditional/generic `any` deeply,
or decide that an open `string`/`number` switch is exhaustible. A `default`
clause is treated as exhaustive, including the conventional `never` assertion
pattern.

## Development

```sh
npm ci --ignore-scripts
npm test
```

Dependencies and compiler settings are analyzer-local and pinned in
`package-lock.json`.
