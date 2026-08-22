# Multi-language Slopscout architecture plan

Status: proposed implementation plan  
Audience: senior engineers implementing or reviewing Slopscout  
Initial languages: Java, TypeScript, Rust

## 1. Decision summary

Slopscout will become a language-neutral analysis and scoring orchestrator with
isolated language analyzer packs. It will retain a PMD-aligned structural-debt
axis and add separately visible language-specific risk axes.

The weighting mix is configurable per language. A profile supplies shared
defaults, and each language may override component enablement, threshold,
weight, formula, and cap. Scope, raw-measurement aggregation, and deduplication
are fixed by the versioned component definition because changing them changes
the meaning of the metric. The effective configuration is resolved for each
language before analysis and is recorded in the report.

The principal decisions are:

1. Preserve PMD's documented metric semantics as the specification for shared
   structural measurements where the target language has equivalent constructs.
2. Report a common structural score and language-specific supplemental scores,
   as well as their sum. Do not hide language-specific risk inside a metric with
   a misleading PMD name.
3. Separate measurement collection from scoring policy. Analyzer packs emit raw,
   normalized measurements; the core applies configurable scoring profiles.
4. Put language integration behind first-party language providers and
   external-process adapters. The core has no dependency on PMD, Node,
   TypeScript, Cargo, or Clippy APIs.
5. Distribute toolchains as Slopscout-owned, versioned analyzer packs. One
   Slopscout installation can install and invoke any combination of language
   packs without modifying analyzed projects.
6. Make configuration declarative and source-controlled. The local UI is a
   client of the same profile schema and never becomes the source of truth.
7. Version measurement definitions, score profiles, protocols, and report
   schemas independently. Reports contain enough provenance to determine
   whether two scores are comparable.
8. Build each analyzer pack around one invocation-scoped `AnalysisContext` and
   reusable discovery, parsing, indexing, and metric kernels. A pack analyzes an
   analysis unit as a batch instead of rediscovering or reparsing it for every
   component.
9. Give every component an evidence posture and an explicit support level per
   language. These fields drive validation, documentation, UI presentation, and
   default enablement and severity.
10. Support hierarchical enablement and severity policy plus bounded, expiring
    waivers, without allowing either mechanism to hide raw measurements or alter
    the remediation ranking.
11. Treat configuration discovery, generation, schema inspection, capability
    inspection, and installation diagnostics as first-class CLI use cases.

## 2. Goals and non-goals

### Goals

- Rank production files for refactoring across Java, TypeScript, and Rust.
- Preserve existing Java behavior during the architectural migration.
- Keep Java's default score compatible with the current Slopscout score until a
  deliberately versioned profile changes it.
- Make structurally equivalent Java and TypeScript constructs score as closely
  as their language semantics permit.
- Give TypeScript unsafe or ambiguous type flows meaningful additional weight.
- Allow different component mixes and weights for every supported language.
- Analyze mixed-language monorepositories in one invocation.
- Provide deterministic, inspectable results in local use, MCP, and CI.
- Permit future first-party language providers without edits to the central
  scoring engine.

### Non-goals

- Replacing a project's normal linter or compiler quality gate.
- Turning every PMD, ESLint, or Clippy diagnostic into score points.
- Claiming that every language has equivalents for God Class, CBO, or NPath.
- Executing repository-owned ESLint configuration or installing packages into an
  analyzed repository.
- Making scores produced by different ProfileSets or incompatible component
  definitions directly comparable.
- Maintaining a historical score database inside Slopscout.

## 3. Score model and per-language configuration

### 3.1 Score axes

Each file has one common axis and zero or more language-specific axes:

```text
total_score = structural_core + structural_language
              + sum(language_specific_scores)
```

Initial axes are:

- `structural_core`: the named cross-language comparable set containing only
  components with shared, conformant definitions. Version 1 contains cognitive
  complexity, method cyclomatic complexity, NPath, and deep nesting.
- `structural_language`: structural components that remain valuable but are not
  in the shared comparable set, such as Java class cyclomatic complexity, CBO,
  and God Class or language-specific class/module cohesion.
- `typescript_type_safety`: unsafe type sources, propagation, uses, boundaries,
  ambiguous control conditions, and incomplete union handling.
- `rust_design`: curated Rust-specific structural or semantic smells that have no
  honest PMD equivalent.

Reports must expose the axes, component coverage, and component contributions.
The `structural_core` axis is suitable for direct cross-language metric
comparison. The total is a policy-calibrated remediation-priority score: it is
useful for mixed-language cleanup ranking under one calibrated profile set, but
is not claimed to be a language-neutral measurement of complexity.

### 3.2 Contribution formulas

Continuous threshold measurements use the current Slopscout behavior, stated
explicitly as:

```text
contribution(value, threshold, weight) =
  0,                                             value < threshold
  weight * (1 + log2(value / threshold)),        value >= threshold
```

Thus `weight` is the number of points assigned to one threshold-level finding.

`linear-over-threshold` is defined as:

```text
contribution(value, threshold, weight) =
  0,                                             value < threshold
  weight * (value / threshold),                  value >= threshold
```

Counts use one of the following profile-selected formulas:

```text
binary:      weight when count > 0, otherwise 0
count:       weight * count
log-count:   weight * log2(1 + count)
```

An optional component cap is applied after contribution aggregation. Compound
built-in components, such as PMD God Class, have a named and versioned formula
rather than user-supplied executable code.

The initial supported formula vocabulary is:

- `binary`
- `linear-over-threshold`
- `log-ratio`
- `count`
- `log-count`
- a registry of versioned built-in compound formulas

Arbitrary code in a profile is not supported.

Scoring follows this normative order:

```text
validate observations
→ deduplicate using the component definition
→ group by canonical file and component
→ for continuous measurements: apply the formula per defined subject, then
  aggregate contributions using the definition-owned aggregator
→ for counted findings: count distinct findings, then apply the count formula
  once
→ for compound measurements: invoke the named, versioned built-in evaluator
→ apply the component cap
→ sum components into axes
→ sum axes into the total
→ preserve the raw total for ranking
→ evaluate valid waivers against definition-owned subjects
→ recompute affected component contributions for gate policy
→ sum gate contributions into gate_score
→ evaluate gates
```

The initial definition-owned aggregators are `sum` and `max`; `top_n_sum` is
deferred until a component requires it. Component descriptors declare the
measurement kind, fixed axis, taxonomy path, supported languages and scopes,
numeric type, deduplication key, aggregator, default formula, allowed formula
overrides, threshold/value constraints, required analyzer capability, default
severity, evidence posture, per-language support level, documentation reference,
and definition version. PMD-aligned definitions use the same aggregator in every
conforming language.

Evidence posture is one of:

- `oracle_aligned`: the definition and implementation are frozen against the
  named oracle and conformance corpus;
- `established`: a stable implementation of a well-established metric, without
  an external oracle-equivalence claim;
- `supported`: a deterministic proxy with documented evidence and limitations;
- `experimental`: an opt-in signal whose calibration or transferability is not
  yet strong enough for default enablement or score contribution.

Language support level is separate from run-time coverage and is one of
`conformant`, `supported`, `best_effort`, `not_applicable`, or `unsupported`.
For example, TypeScript may be `conformant` for a shared cyclomatic definition,
`supported` for an explicitly TypeScript-specific unsafe-boundary definition,
and `unsupported` for PMD God Class. `best_effort` components use a distinct
non-PMD identity and are disabled or zero-weight by default. Coverage states in
section 4 say what happened in one invocation; support level says what the
product promises.

The standard Java v2 definitions preserve the current aggregation where it is
well-defined: cognitive, NPath, and method cyclomatic contributions are summed
per function; deep nesting counts distinct findings; class cyclomatic and CBO
take the maximum contributing type in a file. God Class is evaluated per type
and summed, correcting the legacy single-value limitation for files containing
multiple qualifying types. Schema-v1 compatibility uses a frozen
`java-legacy-v1` profile; the corrected behavior belongs to the deliberately
versioned schema-v2 standard profile.

### 3.3 Profile resolution and language overrides

Profiles are resolved in this order, with later values winning:

1. Built-in profile defaults.
2. Repository-wide component-group `enabled` and `severity` policy.
3. Repository-wide concrete component configuration.
4. Language-specific component-group `enabled` and `severity` policy.
5. Language-specific concrete component configuration.
6. Explicit command-line overrides, when a corresponding CLI option exists.

Component descriptors provide a stable taxonomy path such as
`structural.complexity` or `typescript.type_safety`. A group applies to every
component below its taxonomy path; a more specific group wins. Only `enabled`
and `severity` inherit through groups. Thresholds, weights, formulas, caps, and
gates are legal only on a concrete component or language gate. This prevents a
numeric value from being reused accidentally across metrics with different
scales. Severity is one of `error`, `warning`, or `info` and controls finding
presentation and advisory counts only. It does not change raw measurement, score
contribution, `gate_score`, or the total-score gate. Any future
component-specific gate must be explicit and cannot infer failure solely from
severity. Enablement and the concrete component's weight determine collection
and scoring.

There is no inheritance between languages. A TypeScript override cannot affect
Java or Rust. Repository-wide `[components.X]` settings apply only to languages
whose provider supports component `X`. An explicit
`[languages.L.components.X]` entry is an error when language `L` does not
support `X`. Unknown component IDs, invalid formulas, invalid ranges, and
disallowed formula overrides are configuration errors.

Schema 1 discovers the nearest `.slopscout.toml` by walking upward from the
invocation directory and stopping at the selected workspace or version-control
root. An explicit `--config` wins. Exactly one file is resolved; nested package
policies are not merged. The file may use `extends` to name exactly one built-in
profile but may not recursively extend another file. Omitted fields inherit,
`enabled = false` disables collection and scoring, and there is no implicit
null/reset value. CLI overrides may target a language and concrete component;
they are applied before hashing and appear in report provenance.

Per-language mix control is expressed through component weights; schema 1 has no
second axis multiplier. Multiplying every component weight in an axis has the
same effect and avoids two interacting levels of weighting.

Example `.slopscout.toml`:

```toml
schema = 1
extends = "slopscout-balanced-v1"

[component_groups.structural]
severity = "error"

[languages.typescript.component_groups."typescript.type_safety"]
severity = "warning"

[components.cognitive_complexity]
enabled = true
threshold = 15
weight = 10
formula = "log-ratio"

[components.cyclomatic_method_complexity]
enabled = true
threshold = 10
weight = 5
formula = "log-ratio"

# Class cyclomatic is separately configurable because PMD uses threshold 80.
[languages.java.components.cyclomatic_class_complexity]
enabled = true
threshold = 80
weight = 5
formula = "log-ratio"

# Java retains the standard structural mix but makes coupling more important.
[languages.java.components.coupling_between_objects]
weight = 14

# TypeScript reduces NPath's influence and emphasizes unsafe boundaries.
[languages.typescript.components.npath_complexity]
weight = 5

[languages.typescript.components.unsafe_type_boundary]
enabled = true
weight = 10
formula = "log-count"
cap = 40

[languages.typescript.components.explicit_any]
enabled = true
weight = 3
formula = "log-count"
cap = 15

[languages.rust.components.type_complexity]
enabled = true
weight = 7
formula = "log-count"
cap = 28
```

The effective profile is materialized independently for each language. A
`ProfileSet` is the complete resolved policy for every selected language in one
invocation. The report includes:

- built-in profile name;
- normalized repository policy hash;
- canonical profile-set hash;
- effective language-profile hash;
- component-definition versions;
- analyzer-pack names and versions.

Hashes cover the fully materialized component policy, including inherited
defaults, language overrides, CLI overrides, fixed component-definition
versions, group policies, formulas, thresholds, weights, caps, waivers, gates,
and completeness policy; they are not hashes of the user-authored TOML text.

Files in one invocation may be operationally ranked together because one
resolved ProfileSet governs the run. Cross-run mixed-language policy rankings
require the same profile-set hash and compatible measurement-definition sets.
Same-language historical comparison additionally uses the effective language
profile hash. Only a calibrated built-in ProfileSet may claim standard
cross-language policy comparability; customized ProfileSets remain valid for
ranking within their own run but lose that calibration claim. Direct metric
comparison uses the `structural_core` axis and requires matching core definition
and scoring fingerprints.

### 3.4 Gates in mixed-language repositories

The default `--max-score N` applies to every selected language. Configuration
may supply language-specific gates:

```toml
[gates]
max_score = 100

[languages.typescript.gates]
max_score = 90

[languages.rust.gates]
max_score = 110
```

Language gates override the common gate. Gate evaluation always uses
`gate_score` and all analyzed files, regardless of output limits; when no waiver
applies, `gate_score` equals the raw total score. Ranking and remediation output
always use the raw total. Future axis-specific gates may be added without
changing raw total-score semantics.

### 3.5 Bounded waivers

A waiver is a source-controlled, reviewable exception to gate policy, not an
exclusion from analysis. Every waiver declares a stable ID, canonical path glob,
language when the path could be ambiguous, concrete component ID, reason, and an
optional expiry date. Numeric components require `allow_up_to`; counted findings
use the deduplicated count as that value. Compound component definitions declare
their waiver subject and comparison value.

```toml
[[waivers]]
id = "generated-parser-npath"
path = "src/parser/**"
language = "typescript"
component = "npath_complexity"
allow_up_to = 1200
reason = "Parser branches mirror generated grammar alternatives."
expires = "2027-01-31"
```

The scoring engine always computes and reports the unmodified raw component
contributions, axes, total score, and ranking. It then derives a separate
`gate_score`. For each affected component, the evaluator removes only
definition-owned subjects covered by a currently valid waiver whose normalized
value is at or below its ceiling, then reapplies the same formula, aggregator,
and cap to the remaining subjects. This recomputation is required for `max` and
compound aggregators; subtracting a waived raw contribution is not equivalent.
An expired waiver or a value above its ceiling has no gate effect. Ranking always
uses the raw total; the total-score gate uses `gate_score` and reports both
values plus every applied or rejected waiver. Waivers cannot convert failed or
unavailable coverage into complete coverage, and they cannot waive analyzer or
configuration errors.

Waivers are resolved and validated with the ProfileSet, included in the
normalized repository-policy and profile-set hashes, and retained in report
provenance. Duplicate IDs, unsupported component/language combinations, invalid
dates, missing reasons, negative ceilings, and path selectors outside the
workspace are configuration errors.

## 4. Measurement model

Analyzer packs return raw measurements and findings, not weighted scores.

```python
@dataclass(frozen=True)
class Measurement:
  component_id: str
  definition_version: str
  language: str
  path: str
  scope: Scope                 # project, file, type, function, expression
  value: JsonNumber            # integer or finite decimal; never binary float-only
  subject: Subject | None      # name and stable source location
  attributes: Mapping[str, JsonValue]
  provenance: Provenance       # analyzer, rule, and analyzer version
```

Measurements are immutable. The scoring engine groups them by file, component,
and axis, applies the effective language profile, and produces contributions.
Integers outside JSON's interoperable safe range are encoded as tagged decimal
strings in the analyzer protocol so NPath remains exact. Conversion, rounding,
overflow, and non-finite-value rejection are part of the protocol and scoring
specification.

A finding count must be deduplicated before scoring. The component definition
owns its deduplication key and aggregation semantics. This is essential for
TypeScript, where one `any` origin can otherwise generate assignment, call,
member-access, and return diagnostics.

Project-level findings, such as TypeScript `strict` being disabled, are reported
in project diagnostics or project gates. They are not repeated as a penalty on
every file because that would corrupt file-ranking value.

Every requested component on every analysis unit has one coverage state:

- `complete`;
- `unavailable`;
- `failed`;
- `not_requested`.

Unavailable, failed, and omitted analysis are never represented as a zero
measurement. A file is zero-score only after every enabled component required by
its effective profile completed successfully and contributed zero. Gates fail
closed when a required component is incomplete unless the invocation explicitly
selects an allow-partial policy; that policy and all incomplete coverage are
recorded in provenance.

## 5. PMD alignment specification

Shared structural components use versioned Slopscout definitions derived from
the pinned PMD 7.26.0 implementation and its documented semantics:

- `cognitive_complexity/pmd-sonar-v1`
- `cyclomatic_method_complexity/pmd-v1`
- `cyclomatic_class_complexity/pmd-v1`
- `npath_complexity/pmd-v1`
- `deeply_nested_if/pmd-v1`
- `coupling_between_objects/pmd-v1`
- `god_class/pmd-v1`

The definition, not the underlying analyzer, determines the component identity.
The conformance corpus freezes explicit expected values rather than only
comparing two live analyzers. Updating the PMD oracle requires proving identical
corpus output or creating a new definition version. Any other change to counting
semantics also requires a new definition version.

### 5.1 Cross-language conformance suite

PMD is the oracle for Java. The repository will contain paired Java and
TypeScript fixtures expressing equivalent control flow.
The suite covers at least:

- method/function entry;
- `if`, `else if`, and `else`;
- loops;
- switch cases and defaults;
- ternaries;
- `try`/`catch`;
- short-circuit Boolean sequences;
- early `return` and `throw`;
- `break` and `continue`;
- nesting;
- lambdas and nested functions;
- direct and mutual recursion;
- sequential decision multiplication for NPath;
- Java class WMC, ATFD, TCC, and unique referenced types.

Equivalent constructs must produce equal raw measurements unless an approved
language-semantic exception is documented beside the fixture. TypeScript-only
constructs such as optional chaining and nullish coalescing receive explicit
rules and tests; their behavior must not be inherited accidentally from ESLint.

### 5.2 Java collection

The first migration preserves the PMD CLI adapter and existing Java score. Before
unrestricted threshold customization is exposed, the Java analyzer pack exposes
structured raw metric values via a small bridge over PMD's API. This removes
regex parsing of human-readable rule descriptions and ensures analyzers emit all
requested observations, including values below the configured scoring threshold.

The bridge is part of the Java analyzer pack, not the Python core. It emits the
same normalized measurement protocol as every other pack.

### 5.3 TypeScript collection

The TypeScript analyzer pack is a pinned Node distribution containing:

- TypeScript compiler APIs for project and type information;
- a TypeScript AST traversal implementing the PMD-aligned structural metrics;
- selected typescript-eslint analysis for type-aware findings;
- a Slopscout JSON-protocol entry point.

ESLint's built-in complexity value is not the canonical structural measurement
because its decision-counting semantics are not identical to PMD's. Existing
rules may be used as detectors or cross-checks, but the pack emits measurements
under a PMD-aligned ID only when it implements the Slopscout definition.

TypeScript v1 emits PMD-aligned IDs for cognitive complexity, method cyclomatic
complexity, NPath, and deep nesting. It does not emit PMD CBO, class cyclomatic,
or God Class IDs until a normative TypeScript mapping and conformance appendix
defines structural types, aliases, inferred types, arrow-function fields,
accessors, overloads, declaration types, and module-level code. Useful
TypeScript class/module cohesion signals use separate versioned IDs on the
`structural_language` axis.

Initial TypeScript type-safety components are:

- `explicit_any`;
- `unsafe_type_assertion`;
- `unsafe_type_propagation` (assignment and argument flow);
- `unsafe_type_use` (call and member access);
- `unsafe_type_boundary` (return or exported/public API);
- `ambiguous_boolean_expression`;
- `non_exhaustive_union`.

Version 1 uses deterministic local sink deduplication, not interprocedural taint
or origin tracking. Its key is `(component, canonical file, start, end,
normalized symbol)`, followed by a versioned precedence table for overlapping
findings. Caps limit downstream cascades. Exported/public boundaries carry their
own component rather than a hidden multiplier, keeping policy visible. Each
component definition includes fixture-backed contracts for unsafe assertions,
ambiguous booleans, exported/public boundaries, and exhaustiveness involving
defaults, `never`, enums, unions, and compiler errors.

Structural TypeScript analysis uses the bundled parser and does not require
project dependencies. Type-safety analysis uses the bundled, pinned TypeScript
compiler and requires a trustworthy complete type graph, including resolvable
project declarations. It does not load repository ESLint configuration, execute
package scripts, or install project dependencies. Missing dependencies,
resolution failures, unsupported syntax, or unusable project configuration mark
the type-safety axis `unavailable`, never zero.

The setting `typescript_types` has three values:

- `auto` (default): retain complete structural results, mark typed components
  unavailable with diagnostics, and make a gate fail closed if the active
  profile requires those components;
- `require`: fail the analysis unit when a trustworthy typed program cannot be
  constructed;
- `off`: disable type-aware components and require a compatible structural-only
  effective profile.

The pack's compiler version and supported TypeScript syntax range are reported.

### 5.4 Rust collection

The Rust pack has two modes:

1. Safe structural mode uses a bundled parser/metrics binary and does not build
   the repository. This is the default.
2. Compile-aware enrichment invokes the repository's selected Cargo/Clippy
   toolchain and is explicitly enabled.

Safe mode uses a pinned Cargo metadata capability bundled in the structural pack
and invokes it with `--offline --no-deps`. It does not use a system Cargo, resolve
or download dependencies, build targets, or execute build scripts and procedural
macros. Metadata failure is diagnosed rather than replaced with a partial source
inventory. Compile-aware mode may use the repository-selected toolchain.
Structural metrics use PMD-aligned IDs only where the Rust implementation
satisfies the shared definition and conformance tests. Rust-only smells use the
`rust_design` axis.

Safe structural metrics operate on source syntax without macro expansion.
Macro-generated control flow is not measured and is reported as a known coverage
limitation.

Clippy findings are curated rather than counted wholesale. Compile-aware mode is
marked as executing project code because Cargo builds may run build scripts and
procedural macros. It must never be implied by a read-only MCP annotation.

### 5.5 Go collection

Go is the first adapter for the shared structural analyzer described in section
6.3.1. Its native frontend uses `go/ast`, `go/types`, and `go/packages` to emit
normalized control-flow, receiver-type, field-access, and type-reference facts.
Repository loading is read-only and offline: it must not build packages, run
generators, execute Cgo, or download missing modules. Components that require
unavailable type information report incomplete coverage rather than a zero.

The initial Go target is parity with all PMD-backed measurements currently used
for Java: cognitive complexity, method cyclomatic complexity, NPath, deep
nesting, type WMC, CBO/fan-out, and God Class inputs WMC/ATFD/TCC. God Class maps
to a named Go receiver type. A package- or module-level God metric is a separate
future component because it is not equivalent to PMD God Class.

Go files ending in `_test.go` are excluded by default and included only by the
explicit test-source option. `gocognit`, `gocyclo`, and similar tools are
cross-checks, not canonical sources: the adapter emits every raw measurement,
including values below linter thresholds.

## 6. Core architecture

### 6.1 Modules

```text
slopscout/
  domain/
    measurement.py
    profile.py
    score.py
    waiver.py
    analysis.py
  application/
    orchestrator.py
    analysis_planner.py
    profile_service.py
    runtime_service.py
    report_service.py
  providers/
    base.py
    registry.py
    java.py
    typescript.py
    go.py
    rust.py
  analyzers/
    protocol.py
    process_adapter.py
    pmd_legacy.py
  presentation/
    cli.py
    text_report.py
    json_report.py
    mcp.py
    config_server.py
```

Analyzer-pack source and build tooling may live in sibling top-level directories
such as `analyzers/java`, `analyzers/typescript`, and `analyzers/structural`.
Each pack uses the following conceptual internal layering; exact package names are an
implementation choice:

```text
entry point and protocol
  -> kernel plan
    -> reusable metric kernels
      -> AnalysisContext
        -> discovery, parser, symbol/type index, git/dependency substrates
```

### 6.2 Responsibilities and patterns

`AnalysisOrchestrator` performs the use case:

1. Resolve requested paths and languages.
2. Ask registered language providers to create analysis units.
3. Resolve the effective profile for each unit's language.
4. Determine required measurement components.
5. Build a capability-checked, deduplicated analysis plan.
6. Ask the runtime service for compatible analyzer packs.
7. Collect normalized measurements and diagnostics.
8. Score, rank, apply waivers, gate, and report.

Use Adapter for external-process protocol translation:

- PMD/Java bridge to normalized measurements;
- TypeScript analyzer process to normalized measurements;
- Rust metrics or Clippy process to normalized measurements.

Use Strategy for behavior that is deliberately replaceable:

- project detection and source selection;
- contribution formula;
- component-defined measurement aggregation and deduplication;
- gate policy.

Use configuration data, not subclasses, for thresholds, weights, enablement,
caps, and ordinary formula selection.

First-party language providers register capabilities and strategies with a
registry. Version 1 does not support third-party in-process providers; adding a
provider requires a Slopscout core release, while its analyzer pack may be
versioned independently. The orchestrator must not contain
`if language == ...` branches. Exact-file dispatch
uses the registry's extension claims, with ambiguity treated as a configuration
error.

Component descriptors and scoring definitions are provider/core metadata and
are available for profile validation before a runtime pack is installed. Signed
pack manifests declare the `(component_id, definition_version)` pairs they can
produce. Runtime availability never determines whether a profile is syntactically
valid.

### 6.3 Analyzer-internal context and kernels

Every analyzer request covers all requested components for an analysis unit.
The analyzer constructs one invocation-scoped `AnalysisContext` containing the
canonical source inventory, parsed-source cache, language/project metadata,
diagnostics, and any optional symbol, type, git-history, or dependency indexes
required by the plan. Context contents are private to the pack and are not part
of the cross-process protocol.

Metric kernels consume the context and emit raw measurements or lower-level
facts. A kernel does not perform policy scoring, read `.slopscout.toml`, format a
report, or decide whether a finding fails a gate. Derived kernels may depend on
declared lower-level facts through an acyclic dependency graph; the pack's
kernel planner topologically schedules and memoizes those facts. Kernels never
invoke a higher layer or another top-level rule wrapper implicitly.

For one parser/index configuration, a source file is discovered and parsed at
most once per analyzer invocation. A second parse is permitted only when a
component explicitly requires a materially different parser mode, such as a
TypeScript compiler program in addition to syntax-only traversal; the analysis
pack's execution-plan diagnostics expose that requirement. Components must not
independently rescan the workspace. Shared facts such as per-function decision
counts can feed cyclomatic complexity, WMC, hotspots, or later advisory
components without repeating traversal.

This batching contract applies to in-process libraries used inside a pack as
well as native or managed analyzers. It is verified with instrumentation tests,
not inferred from timing.

### 6.3.1 Shared structural analyzer and implementation language

The PMD-aligned structural analyzer is a language-neutral host with replaceable
language adapters and metric strategies. The host and metric engine are
implemented in **Go**. Go provides the strongest first adapter through its native
analysis APIs, straightforward cross-platform static binaries, unbounded NPath
values through `math/big`, and a small protocol runtime.

The host owns the normalized fact model, PMD-compatible metric strategies,
strategy registration by component ID and definition version, coverage and
diagnostics, and the Slopslap analyzer-protocol bridge. An adapter owns parsing,
optional symbol/type resolution, and translation of native constructs into
normalized facts; it does not implement scoring or duplicate metric formulas.

The Go adapter is linked into the host. The Rust adapter is implemented in Rust
so it can use native Rust parsing and rust-analyzer/compiler facilities, and it
exchanges a versioned normalized fact stream with the Go host. Future adapters
may use the same subprocess fact protocol. Version 1 deliberately avoids Go
dynamic plugins: static registration and isolated versioned processes are more
portable and already provide the required extensibility boundary.

```text
Go structural host
  -> adapter registry
       -> built-in Go adapter
       -> native Rust fact-adapter process
       -> future external fact adapters
  -> normalized ProgramFacts
  -> PMD-compatible metric strategy registry
  -> normalized Slopslap measurements and coverage
```

### 6.4 Analyzer process protocol

All analyzer packs implement a versioned stdin/stdout protocol. Requests include:

- protocol version and invocation ID;
- workspace and analysis units;
- exact source files;
- requested component IDs and definition versions;
- language-specific analyzer options;
- resource and timeout limits.

Responses are NDJSON records containing:

- pack metadata and capabilities;
- measurements;
- diagnostics;
- per-file and per-component coverage;
- requested, successfully analyzed, skipped, and failed file IDs;
- one terminal success, failure, cancellation, or timeout status.

Protocol output is the only supported integration surface. Analyzer logs go to
standard error. Partial measurements may be returned for diagnostics but are not
scoreable unless an explicit allow-partial policy accepts their coverage state.

The signed runtime manifest, not analyzer stdout, is authoritative for pack
selection. It declares platform/architecture, entry point, digest, protocol
minimum/maximum, language, capabilities, and definition versions. The protocol
specification fixes UTF-8 encoding, workspace-relative canonical paths, symlink
and outside-workspace policy, tagged large-integer encoding, maximum record and
stream size, duplicate-observation handling, and cancellation semantics. The
adapter rejects unknown record types, mismatched invocation IDs, duplicate
terminal records, records after termination, and measurements for paths outside
the requested canonical source set.

## 7. Discovery and source scope

The common scope resolver establishes canonical absolute requested paths and the
workspace. Language providers then construct language-specific analysis units.

- Java recognizes Maven and Gradle modules and conventional production roots.
- TypeScript recognizes package manifests, tsconfig project references, package
  workspaces, `.ts`, `.tsx`, `.mts`, and `.cts`. It excludes declarations,
  generated output, tests, fixtures, stories, coverage, and dependency
  directories by default.
- Rust safe mode uses the bundled Cargo metadata capability and includes library
  and binary production sources by default. Tests, examples, benches, generated
  output, and `target` are excluded.

Every analysis unit has a stable ID. Each `(language, canonical source path)` has
exactly one authoritative scoring context per run. Providers return non-overlapping
ownership or apply a documented deterministic nearest/specific-project rule.
Equally authoritative candidates fail with an actionable diagnostic and require
an explicit project/config override. Canonicalization and symlink policy run
before ownership assignment.

Explicit files bypass conventional-root narrowing but retain nearest-project
configuration inference. Explicit unsupported extensions fail rather than being
silently ignored, and exact files use the same ownership rule.

Mixed-language auto-detection is the default for a repository scope. A repeatable
`--language` option restricts detection:

```sh
slopscout .
slopscout . --language typescript
slopscout . --language java --language typescript
slopscout --file src/a.ts --file crates/core/src/lib.rs
```

## 8. Runtime packaging

### 8.1 Runtime packs

The Python core remains small. Language dependencies are installed as isolated,
Slopscout-owned runtime packs:

```text
java-pmd-<version>-<platform>
  minimal JRE
  pinned PMD
  structured metrics bridge

typescript-<version>-<platform>
  private Node runtime
  pinned TypeScript and typescript-eslint dependencies
  Slopscout TypeScript analyzer

structural-<version>-<platform>
  native Go structural host and built-in Go adapter
  PMD-compatible metric strategies and normalized fact schema
  native Rust fact adapter
  pinned offline Cargo metadata capability and required runtime support
```

Packs are content-addressed, verified against a signed release manifest, and
installed atomically outside analyzed repositories. They never add dependencies
to `package.json`, modify a Cargo
workspace, or rely on a system-wide PMD installation.

The runtime manager provides:

```sh
slopscout runtime install java typescript go rust
slopscout runtime list
slopscout runtime doctor
slopscout runtime update typescript
```

Normal analysis may offer to install a missing pack in an interactive CLI, but
CI and MCP fail with an actionable message rather than initiating an implicit
network download.

### 8.2 Reproducibility

Each Slopscout release has a default runtime manifest. A repository may commit a
lock file pinning pack and component-definition versions. The report records the
resolved manifest and pack digests.

Core and pack manifests declare compatible protocol ranges and definition
versions. Pack versions install side by side. `runtime update` may download a new
pack, but it never changes the pack selected by a repository lock. Installation
rejects archive traversal and symlink escapes, handles interrupted or concurrent
installs without exposing partial packs, verifies before atomic activation, and
supports offline archive import and rollback.

Release channels are:

- lightweight core plus on-demand packs;
- a full platform archive with all safe structural packs;
- an OCI image containing the core and all safe structural packs for hermetic CI.

Clippy enrichment is not included in the safe structural guarantee. It uses an
explicitly selected project or Slopscout-managed Rust toolchain and has separate
cache and execution policy.

The TypeScript pack encapsulates analyzer dependencies, not the target project's
semantic declarations. Type-aware analysis may therefore require the checkout's
already-installed dependencies; Slopscout never installs them and reports
incomplete coverage when they are absent.

Release engineering publishes the supported OS/architecture matrix, SBOMs, and
license notices for the JRE, PMD, Node, TypeScript/ESLint ecosystem, and Rust
crates. The initial required matrix is macOS arm64/x64, Linux x64/arm64, and
Windows x64. An unavailable platform fails at runtime resolution rather than
falling back to an unverified system executable.

## 9. Reports, MCP, CI, and UI

### 9.1 Report schema v2

Schema v2 adds:

- language on every file and analysis unit;
- raw score axes and component contributions, plus the separately derived
  `gate_score` when waivers apply;
- component coverage and analysis completeness;
- raw normalized measurements when requested;
- profile-set and effective language-profile hashes;
- analyzer-pack and component-definition provenance;
- component evidence posture and per-language support level;
- applied, rejected, and expired waiver details;
- structured analysis diagnostics;
- generic unit metadata rather than `java_version` at the common layer.

Multi-language behavior is a declared major-version transition. In that major
version, `slopscout .` performs safe multi-language auto-detection and JSON
defaults to schema v2. Java-only schema-v1 output is retained for one major
compatibility cycle behind `--schema 1`; it rejects non-Java scopes. The
composite action retains its Java-only default for the compatibility release and
switches to safe auto-detection in the new major version.

### 9.2 MCP

Add generic read-only tools:

```text
rank_files(directories=[], languages=["auto"], limit=10, profile=None)
score_files(files=[...], profile=None)
```

Keep `rank_java_files` and `score_java_files`, including their schema-v1 response
contract, as deprecated wrappers for one major compatibility cycle. Safe
structural analyzers remain read-only. A future
compile-aware Rust tool must be separately named and annotated because it can
execute build scripts and procedural macros.

### 9.3 GitHub Actions

The action accepts languages or uses safe auto-detection, provisions the pinned
packs, and runs one mixed-language gate. It does not install dependencies in the
target repository. Runtime and analyzer caches are keyed by the runtime manifest
and lock hash.

### 9.4 Configuration UI

`slopscout configure` launches a loopback-only local web application after the
profile CLI and schema are complete. The UI is
generated from component descriptors and profile JSON Schema. It supports:

- component enablement by language;
- hierarchical enablement and severity inspection;
- shared and per-language thresholds and weights;
- formula and cap selection;
- bounded waiver creation, validation, and expiry display;
- evidence posture and language-support explanations;
- effective-profile inspection;
- live score/contribution preview from an in-memory measurement set;
- profile-set and structural-core comparability status;
- saving canonical `.slopscout.toml`.

The CLI profile commands and config validation are implemented before the UI.
The UI does not introduce an alternative persistence format or daemon.
It binds a random loopback port, requires a one-time bearer token and valid
Origin, restricts saves to the selected workspace configuration, and never
installs packs or starts compile-aware analysis implicitly.

### 9.5 Configuration and capability CLI

The non-UI configuration surface is complete enough for local use, CI, and
automation before `slopscout configure` ships:

```sh
slopscout init default
slopscout init strict
slopscout init legacy
slopscout schema --format json
slopscout components --language typescript
slopscout capabilities --format table
slopscout config explain
slopscout config diff --profile slopscout-balanced-v1
slopscout doctor
```

`init` writes a canonical starter file and refuses to overwrite an existing
configuration without an explicit flag. `schema` emits the versioned profile
JSON Schema used by the UI. `components` and `capabilities` are generated from
the component catalogue and provider registry, including definition version,
taxonomy, axis, evidence posture, support level, required capability, default
policy, and installed-pack availability; JSON output is also supported.

`config explain` shows discovery, inheritance, language overrides, CLI
overrides, waivers, and hashes for the fully materialized ProfileSet. `config
diff` compares resolved policy rather than TOML text. `doctor` is read-only: it
checks core/pack compatibility, manifests, digests, protocol ranges, configured
component availability, and optional project prerequisites, but never downloads
a pack or modifies the analyzed repository.

## 10. Errors, security, and operational policy

- Missing packs, unsupported component versions, invalid profiles or waivers,
  analyzer crashes, parser failures, and project-configuration failures are
  distinct structured errors.
- A requested file is never reported as a zero-score file when its analysis
  failed. Zero means successful analysis with no contributing measurements.
- Analyzer processes receive explicit paths and timeouts and run without shell
  interpolation. Safe packs receive a sanitized environment and a
  Slopscout-owned temporary work/cache directory and cannot write to the analyzed
  repository.
- Repository ESLint configuration and package scripts are not executed.
- Safe Rust structural analysis does not invoke Cargo builds.
- Compile-aware analyzers disclose that they may execute repository code.
- Packs are verified before execution and cannot be resolved from the target
  repository by executable name alone.
- Analyzer and configuration diagnostics do not contribute to file scores.
- If a detected/requested language pack or required component fails, the default
  mixed analysis exits nonzero. It may still emit partial results, explicitly
  marked incomplete and excluded from gates and zero-score inventory.

## 11. Delivery plan

### Phase 1: characterize and extract the core

- Add golden characterization tests for current Java text, JSON, MCP, discovery,
  exact-file ordering, gates, and action behavior.
- Introduce the package structure and domain models.
- Move scoring and reporting behind language-neutral services.
- Implement the Java language provider through the legacy PMD adapter.
- Preserve the existing Java CLI and MCP wrappers.

Exit criterion: existing Java behavior and scores are unchanged.

### Phase 2: profiles and report schema

- Define component descriptors, evidence posture, per-language support levels,
  formulas, fixed aggregation, and profile resolution.
- Implement the structured Java metrics bridge so every configurable component
  has complete raw observations below and above its scoring threshold.
- Implement upward `.slopscout.toml` discovery, hierarchical enablement and
  severity, concrete component policy, bounded waivers, profile validation,
  hashing, explain, and diff.
- Implement per-language overrides and mixed-language gates.
- Add report schema v2 with provenance, support/evidence metadata, raw score
  axes, `gate_score`, and waiver disposition.
- Add `init`, `schema`, `components`, and `capabilities` commands generated from
  the same catalogue used by validation and reporting.
- Add generic CLI and MCP entry points while retaining Java compatibility
  wrappers.

Exit criterion: Java can be rescored with shared or Java-specific weights and
thresholds, and the effective policy and raw measurement coverage are completely
represented in output. Hierarchical policy and waivers round-trip without
changing the raw remediation ranking.

### Phase 3: analyzer protocol and runtime manager

- Define and test the analyzer-pack protocol.
- Define the analyzer-internal `AnalysisContext`, kernel dependency DAG, and
  batch analysis-plan contract.
- Implement runtime manifests, verification, install, list, and doctor.
- Package the structured Java bridge behind the isolated runtime protocol.
- Publish one full local/CI distribution path.

Exit criterion: Java analysis runs through an isolated, pinned runtime pack and
does not depend on a system PMD executable. Instrumentation proves that one
parser/index configuration discovers and parses each Java source at most once
per analyzer invocation.

### Phase 4: TypeScript structural analysis

- Implement TypeScript discovery and exact-file planning.
- Implement the `structural_core-v1` PMD-aligned cognitive, method cyclomatic,
  NPath, and nesting definitions.
- Implement those kernels over one TypeScript analysis context and reuse shared
  syntax facts across components.
- Add separately named TypeScript class/module structural measurements only when
  their contracts and fixtures are defined.
- Build the paired Java/TypeScript conformance suite.
- Document every approved semantic exception.

Exit criterion: the shared structural axes pass the conformance corpus and a
TypeScript-only project can be ranked without loading its ESLint configuration.

### Phase 5: TypeScript type-safety axis

- Add type-aware unsafe and ambiguous-type components.
- Implement deterministic local sink deduplication and public-boundary
  classification contracts.
- Establish default weights from a representative calibration corpus.
- Add performance and monorepo fixtures for tsconfig project references.

Exit criterion: findings are stable, explainable, non-cascading, and independently
configurable from structural metrics.

### Phase 6: mixed-language surfaces and UI

- Update the GitHub Action for mixed-language safe analysis.
- Implement the configuration UI over the completed profile APIs, including
  group policy, evidence/support explanations, and bounded waiver editing.

Exit criterion: one invocation and one installation analyze Java and TypeScript,
with per-language policy visible and editable.

### Phase 7: shared structural analyzer and Go

- Freeze the normalized fact schema and adapter capability contract.
- Implement the Go host, metric strategy registry, protocol bridge, and built-in
  Go adapter.
- Port all seven PMD-backed measurements to shared strategies.
- Add Go discovery, installation, test-source policy, and mixed-language output.

This phase is accepted when these seven criteria pass:

1. Frozen Java/Go fixtures have exact raw expected values for cognitive,
   cyclomatic, NPath, deep nesting, WMC, CBO, ATFD, and TCC; every semantic
   exception is named beside its fixture.
2. Strategy tests consume hand-built normalized facts without invoking a parser,
   while adapter tests verify native syntax-to-fact translation without
   reimplementing metric formulas.
3. Valid Go files report complete coverage for all seven enabled PMD-backed
   components; missing required type information is `unavailable`, never zero.
4. Multi-file receiver fixtures prove WMC, CBO, ATFD, and TCC behavior, and NPath
   tests prove arbitrary-size integer results and exact threshold boundaries.
5. `_test.go` files are absent by default and present with the explicit
   test-source option, including when supplied as exact targets.
6. Repeated analysis is byte-for-byte deterministic, parses each source once per
   parser mode, rejects outside-workspace input, and performs no repository
   build, generator execution, Cgo execution, or module download.
7. A mixed Java/TypeScript/Go fixture passes protocol and report tests without
   changing existing Java or TypeScript raw measurements or scores.

### Phase 8: Rust

- Implement the native Rust fact adapter against the shared fact protocol.
- Add bundled offline Cargo metadata discovery to the structural pack.
- Reuse PMD-aligned strategies only where conformance is demonstrated.
- Add curated Rust-specific components and optional compile-aware enrichment.
- Extend mixed-language and runtime-pack test matrices.

This phase is accepted when these five criteria pass:

1. Rust syntax fixtures translate to the same normalized facts as equivalent Go
   and Java fixtures, and therefore receive the same shared-strategy values.
2. Multi-file types and `impl` blocks have frozen expected WMC, CBO, ATFD, and
   TCC results wherever the adapter declares those facts supported; unavailable
   semantic facts are explicit rather than approximated or zeroed.
3. Safe mode is deterministic and performs no build, dependency download, build
   script, procedural macro, or repository-code execution; unexpanded macros
   produce documented component coverage.
4. Rust test trees and `#[cfg(test)]` items are excluded by default and restored
   by the explicit test-source option.
5. One installation analyzes Java, TypeScript, Go, and Rust through protocol and
   report integration tests without changing existing-language results.

## 12. Verification and acceptance criteria

The implementation is accepted when:

1. `java-legacy-v1` results match the current implementation exactly on the
   golden corpus. The schema-v2 standard Java profile may differ only in
   documented, deliberately versioned cases such as multi-God-Class aggregation.
2. A language override changes only that language's effective profile and score.
3. Profile hashing is stable across formatting and key-order differences.
4. Profile-set and effective language hashes, structural-core compatibility, and
   calibration status are always present; incompatible cross-run comparison is
   never silent.
5. Java/TypeScript equivalent fixtures match frozen expected values and their
   documented semantic exceptions.
6. TypeScript unsafe-flow cascades are deduplicated according to the component
   contract.
7. Zero-score inventory excludes failed or skipped files.
8. A mixed repository can rank and gate all installed languages in one run.
9. Missing analyzer packs produce deterministic actionable errors.
10. The full distribution analyzes all safe supported languages without system
    PMD, Node, npm, Cargo, or Clippy dependencies.
11. Compile-aware Rust execution is explicit and cannot be selected through the
    safe read-only MCP tools.
12. Custom profiles and waivers round-trip through config parsing, canonical
    output, CLI, report provenance, and the UI schema.
13. Method and class cyclomatic thresholds are independently configurable and
    current Java parity covers exact thresholds, multiple classes, and compound
    God Class findings.
14. Tests distinguish disabled, unavailable, failed, unsupported, and complete
    components; lowering a threshold uses raw observations not present in the
    legacy PMD violation report.
15. Scoring covers huge NPath integers, caps, deterministic rounding, and rejects
    negative, overflowed, NaN, and infinite configuration or measurements.
16. TypeScript tests cover missing dependencies, unresolved aliases, project
    reference cycles, incompatible syntax, `.mts`/`.cts`, and one file included
    by multiple tsconfig projects.
17. Protocol tests cover crashes, truncation, malformed or oversized records,
    timeouts, duplicates, unexpected paths, and partial completion.
18. Runtime tests cover tampering, wrong platforms, interrupted/concurrent
    installs, offline import, rollback, and lock mismatch.
19. Rust tests cover workspace inheritance, explicit targets, malformed manifests,
    and macro-heavy source with documented coverage.
20. Symlinked/outside-workspace sources and duplicate analysis-unit ownership
    follow the canonical path and ownership policy.
21. Every component descriptor has a valid taxonomy path, evidence posture, and
    support level for each registered language; generated capability output,
    profile validation, reports, and UI schema agree with the catalogue.
22. Hierarchical configuration tests prove that only `enabled` and `severity`
    inherit, the most specific group wins, numeric policy remains concrete, and
    one language's groups cannot affect another language.
23. Waiver tests cover matching, expiry, ceilings, deduplicated counts, compound
    subjects, malformed configuration, and incomplete coverage. Applied waivers
    may change `gate_score` and gate disposition but never raw measurements,
    total score, or rank.
24. Analyzer instrumentation proves that each source is discovered and parsed at
    most once per parser/index configuration and that shared facts are memoized;
    declared multi-parser cases remain visible in analyzer execution-plan
    diagnostics.
25. `init`, `schema`, `components`, `capabilities`, `config explain`, `config
    diff`, and `doctor` work without the UI, have deterministic JSON where
    applicable, and derive their answers from the same registries used by the
    orchestrator.

## 13. Implementation discretion

The plan deliberately leaves ordinary engineering choices to the implementing
engineer, including internal class names, exact Python package granularity,
serialization library, UI framework, process supervision library, archive
format, and build tooling. These choices must preserve the domain boundaries,
protocol behavior, security policy, configuration precedence, and acceptance
criteria above.

## 14. Independent review disposition

The plan received two independent implementation-oriented reviews on
20 August 2026:

- A senior static-analysis engineering review assessed PMD alignment, metric
  identity, TypeScript type analysis, Rust coverage, scoring semantics,
  conformance testing, and analyzer packaging.
- A senior architecture review assessed domain boundaries, Adapter/Strategy
  usage, ProfileSet resolution, process protocols, runtime distribution,
  compatibility, security, UI separation, and delivery ordering.

Both reviewers approved this revision with no remaining implementation blocker.
Their blocker-level findings were incorporated into the normative decisions and
acceptance criteria above. Ordinary library, framework, and code-organization
choices remain implementation discretion as intended.

## 15. Primary technical references

- [PMD Java design-rule definitions](https://docs.pmd-code.org/latest/pmd_rules_java_design.html)
- [PMD 7.26 Java metric API and counting semantics](https://docs.pmd-code.org/apidocs/pmd-java/7.26.0/net/sourceforge/pmd/lang/java/metrics/JavaMetrics.html)
- [PMD JavaScript and TypeScript support](https://pmd.github.io/pmd/pmd_languages_js_ts.html)
- [typescript-eslint typed analysis](https://typescript-eslint.io/getting-started/typed-linting/)
- [typescript-eslint rule catalogue](https://typescript-eslint.io/rules/)
- [Cargo metadata command](https://doc.rust-lang.org/cargo/commands/cargo-metadata.html)
- [Clippy lint catalogue](https://rust-lang.github.io/rust-clippy/stable/index.html)
