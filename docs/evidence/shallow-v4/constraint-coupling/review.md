# SHALLOW constraint-based coupling correction

Policy r41 → r42. Production weights are unchanged. This report evaluates the structural correction, not calibration robustness or enterprise performance readiness. Frozen expectations, source snapshots and first-evaluation records are preserved. Retained examples have already been observed; these reruns are not fresh blind holdouts.

## Protocol regression

| Scope | Score before → after | Representation burden | Supported burden | Recognized responsibility | Estimated responsibility |
|---|---:|---:|---:|---:|---:|
| isolated | 85 → 0 | 28 → 0 | 28 → 0 | 2 → 2 | 0 → 0 |
| workspace | 85 → 0 | 28 → 0 | 28 → 0 | 2 → 2 | 0 → 0 |

The old finding charged fourteen fields, including derived `Raw`, because metadata assembly referenced them together. The replacement finds no demonstrated caller-maintained constraint in that assembly. Variant/optional-value handling connected to the stored representation retains two transformation responsibility units. The ordinary operation remains visible in surface evidence; after the unchanged residual calculation there is no supported residual burden. The resulting zero is an evidence-based estimate, not a display override, cap or special case.

`decodeProtocolStream` decodes into a record, attaches metadata, checks version/invocation and passes the result to consumers. This workflow does not demonstrate that callers manually coordinate every metadata field. The bounded estimator does not claim a complete proof of protocol variant semantics. Both runs retain estimation metadata and the unresolved `len(record.Attributes)` limitation. See [isolated before](protocol-r41-isolated.json), [isolated after](protocol-r42-isolated.json), [workspace before](protocol-r41-workspace.json) and [workspace after](protocol-r42-workspace.json).

## Implementation and generalization

- Constraints identify exact participating storage and connected indexed access, rejecting relational preconditions or demonstrated phase admission. Aggregation, formatting, arithmetic and adjacent/shared-input writes alone do not establish coupling.
- Caller resolution distinguishes target receivers, lexical shadows, mutable exposure, actual producer writes and copied receivers. Known supported size contracts refine protection; unresolved relationships remain limitations.
- Protection and maintenance must concern the actual relationship and survive relevant writes. Deduplication keeps a supporting unprotected access when one remains. Structured constraint records include storage, caller control, operation, source location and protection, and are exported by the native report.
- Output-connected aggregate/variant work is separated from copying and discarded calculations. Aggregate uncertainty covers possible transformation rather than inventing validation; overlapping known duties are deducted.
- Four-language tests cover constraints versus projection, unrelated metadata/derived outputs, renaming, helper extraction, field identity, Boolean polarity, protection and maintenance. Existing decoy-name, qualification, guard-monotonicity, façade, publication and fix-safety tests remain active.

## Retained holdouts

Complete score, burden, recognized/estimated responsibility and limitation comparisons are in [holdout-comparison-r42.json](holdout-comparison-r42.json). The context runner retains distinct isolated, caller-witness and full live-workspace evaluations.

| Original holdout | Mode | Expected | Score before → after | Representation before → after | After supported burden / known responsibility / estimated responsibility |
|---|---|---:|---:|---:|---:|
| river-bound-access | isolated | 65–100 | 0 → 0 | 0 → 0 | 0 / 0 / 0 |
| river-descriptor-scan-context | isolated | 65–100 | 22 → 22 | 0 → 0 | 2 / 0 / 3 |
| nql-hierarchy-row-context | isolated | 0–25 | 16 → 16 | 0 → 0 | 1.75 / 4 / 0 |
| xmltoaster-sql-row-cursor | isolated | 26–64 | 53 → 53 | 4 → 4 | 12.25 / 3 / 2 |
| river-bound-access | witness_context | 65–100 | 0 → 0 | 0 → 0 | 0 / 0 / 0 |
| river-descriptor-scan-context | witness_context | 65–100 | 67 → 22 | 12 → 0 | 2 / 0 / 3 |
| nql-hierarchy-row-context | witness_context | 0–25 | 16 → 16 | 0 → 0 | 1.75 / 4 / 0 |
| xmltoaster-sql-row-cursor | witness_context | 26–64 | 53 → 53 | 4 → 4 | 12.25 / 3 / 2 |
| river-bound-access | workspace | 65–100 | 86 → 0 | 6 → 0 | 0 / 0 / 0 |
| river-descriptor-scan-context | workspace | 65–100 | 67 → 46 | 12 → 4 | 6 / 0 / 3 |
| nql-hierarchy-row-context | workspace | 0–25 | 16 → 16 | 0 → 0 | 1.75 / 4 / 0 |
| xmltoaster-sql-row-cursor | workspace | 26–64 | 53 → 53 | 4 → 4 | 12.25 / 3 / 2 |

The previous workspace highs are not preserved by reinstating shared-write or selector/argument unions. `SqlBoundAccess` still has a meaningful reviewed discriminator/payload obligation that this bounded analysis does not prove. `SqlDescriptorScanContext` has some connected phase evidence in the full workspace, but its full reviewed lifecycle burden remains under-recognized; the retained witness subset does not supply equivalent admission context. These are explicit missed high cases, not passing holdout results.

| Other retained case | Expected | Isolated before → after |
|---|---:|---:|
| go-response-controller | 65–100 | 28 → 28 |
| go-tempfile | 0–25 | 3 → 3 |
| typescript-tracker-write-registry | 65–100 | 20 → 20 |
| typescript-safe-write | 0–25 | 20 → 20 |
| rust-manually-drop | 65–100 | 83 → 83 |
| rust-vec-drain | 0–25 | 50 → 50 |
| go-condition-variable | 65–100 | 79 → 64 |
| go-once-initializer | 0–25 | 0 → 0 |
| typescript-finalize-coordinator | 26–64 | 50 → 50 |
| typescript-recent-log-buffer | 0–25 | 0 → 0 |
| rust-maybe-uninit | 65–100 | 91 → 91 |
| rust-once-lock | 0–25 | 69 → 69 |
| go-userdata-root | 0–25 | 0 → 0 |
| go-fix-subscription | 26–64 | 50 → 50 |
| typescript-source-context | 26–64 | 30 → 30 |
| typescript-public-operations | 0–25 | 0 → 0 |
| rust-program-parser | 0–25 | 20 → 20 |
| rust-surface-collector | 26–64 | 26 → 26 |

`ResponseController` and the TypeScript tracker remain missed highs. `VecDrain` and `OnceLock` remain false positives against their retained low-band reviews. `Cond` loses the former unsupported coupled-field allowance and falls below its reviewed high band; this is reported as a newly exposed missed high, not repaired through weights or a name exception. `ManuallyDrop` and `MaybeUninit` retain real high detection. Structural-fresh has no high-band examples and therefore cannot validate high detection across languages.

The standard-library connected corpora were evaluated in isolated and retained-witness modes (`--frozen-only`); no complete standard-library workspace evaluation is claimed. The original Java corpus also used the live River and Xmltoaster workspaces, whose revisions/source hashes and limitations are retained by the runner.

| Corpus / context | Band checks before → after | Ranking checks before → after |
|---|---:|---:|
| calibration / isolated | 2/4 → 2/4 | 1/3 → 1/3 |
| calibration / witness_context | 3/4 → 2/4 | 2/3 → 1/3 |
| calibration / workspace | 4/4 → 2/4 | 3/3 → 2/3 |
| connected-high / isolated | 3/6 → 3/6 | 2/3 → 2/3 |
| connected-high / witness_context | 3/6 → 3/6 | 2/3 → 2/3 |
| connected-confirmation / isolated | 5/6 → 4/6 | 2/3 → 2/3 |
| connected-confirmation / witness_context | 5/6 → 4/6 | 2/3 → 2/3 |
| structural-fresh / isolated | 6/6 → 6/6 | 3/3 → 3/3 |

## Broader distributions and publication

| Repository sample | Numeric before → after | Analyzed before → after | N/A after | High band before → after | Zero before → after |
|---|---:|---:|---:|---:|---:|
| slopwatch | 684 → 687 | 684 → 687 | 0 | 33 → 19 | 177 → 185 |
| xmltoaster | 84 → 84 | 84 → 84 | 0 | 7 → 8 | 28 → 28 |
| river | 2153 → 2153 | 2207 → 2207 | 54 | 272 → 302 | 217 → 219 |
| typescript | 128 → 128 | 128 → 128 | 0 | 1 → 1 | 117 → 117 |
| rust | 40 → 40 | 40 → 40 | 0 | 10 → 5 | 11 → 13 |

The complete bins, changed-source exclusions and unchanged-source per-file score/burden/responsibility deltas are in [distribution-comparison-r42.json](distribution-comparison-r42.json). Own-repository counts include benchmark snapshots; the three added analyzed files implement constraint recognition and caller/consumer resolution. River’s 54 `not_applicable` files remain distinct from numeric ratings. The TypeScript sample here is `career-ops` with the current ignore policy, not the earlier 486-file sample.

Mixed movement across repositories rules out blanket score suppression as an explanation, but does not establish every changed rating is correct. Larger new high candidates, including `TransactionProgramStorage`, `SqlAggregateAccumulatorSet` and `ParameterParser`, need independent semantic review. The storage examples have real indexed/caller-visible state and useful internal growth/projection work; neither their old nor new score is assumed correct merely because it is lower or higher.

## Verification and limitations

The [validation record](validation.json) contains commands and logs. Focused estimator/native/report/scoring/follow/fix-adapter suites, four-package race checks and calibration safeguards pass. All 60 synthetic scores are unchanged; all 12 real benchmark bands pass, with only `java-query-pipeline` changing from 58 to 60. Both retained QueryResultRow forms remain covered (the real benchmark and the separate actual-source estimator regression). The 60 synthetic cases and 12 frozen real benchmark cases remain required checks. The separate holdout failures above are retained rather than relabeled. The full Go suite has the pre-existing `analysiscache.TestProjectionArtifactRoundTrip` nil-versus-empty `Depth` map failure; focused regression and race results are reported separately.

The production calibration file SHA-256 remains `5b03a577e9b9fdbb04dda8be9e9ffd208906c91243f63853c13f90e487368b08` (`graded-r38-v1`). No weight tuning occurred. Passing calibration safeguards is not a claim that the estimate weights are robust.

Remaining limits include bounded call/alias resolution, incomplete discriminator and general branch-result semantics, supported size-contract/protection forms, and unresolved external implementations. Public writes can bypass an internally maintained update. Source and limitation metadata remain separate from finite numeric publication; automated-fix safety gates are unchanged.

Three independent review layers and their corrections are documented in [review-triage.md](review-triage.md). No controlled 30K-file cold, warm and incremental measurements were performed, so enterprise performance readiness is not established.

The [task-scoped implementation diff](implementation.diff) compares the pre-task working files, excluding unrelated prior changes. [identity-r42.json](identity-r42.json) records the final binary/source hashes and confirms all 143 pre-existing retained evidence files are unchanged.
