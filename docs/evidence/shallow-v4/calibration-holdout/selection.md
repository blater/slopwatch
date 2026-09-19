# Frozen independent real-source holdout

Independent source-only reviewer read rubric/P0 directive and live source. Reviewer did not invoke scorer or read scores, grading reports, old expected cases or frozen snapshots. Parent checked selected names absent from prior manifests without sending scores.

Ranges are source-review judgments fixed before scoring. They are not computed outputs. Keep misses visible and never rewrite the freeze after evaluation. Witness files establish caller evidence and are not scored targets.

| Case | Expected band | Range |
| --- | --- | --- |
| river-bound-access | high | 65–100 |
| river-descriptor-scan-context | high | 65–100 |
| nql-hierarchy-row-context | low | 0–25 |
| xmltoaster-sql-row-cursor | mixed | 26–64 |

## river-bound-access

SqlAccessEdgeSelector.publish/selectRange write the inherited fields together. Reset is executable state transition, not pure construction or a declaration-only contract. Brevity is not the evidence: caller-owned representation is.

Hidden duties: Reset defaults for a query access edge.

Caller burden: Package callers write seven coupled mutable fields: predicate/column/comparison/value/lower/upper/text-column; they must install coherent comparison and endpoint combinations.

## river-descriptor-scan-context

SqlDescriptorScanOpen checks matched/pin and sets active; SqlDescriptorScanCleanup closes resources and resets flags only on success. Construction deserves wiring credit but does not own lifecycle. This judges the context boundary, not the complete scan service.

Hidden duties: Constructs and wires retained helpers; selects bound versus unbound predicate evaluation.

Caller burden: Callers coordinate mutable resources and five directly writable flags: materialized/scalarAggregate/forUpdate/matched/active. Admission, opening and cleanup are caller-owned phases.

## nql-hierarchy-row-context

HierarchyPathResolverEngine calls both operations during traversal. Two entry points hide cache lookup, creation and attachment. The computeIfAbsent closures own useful consistency duties.

Hidden duties: Deduplicates nodes by parent identity and path; creates and attaches missing children in one cached operation; keeps cache private.

Caller burden: Caller supplies parent/path, selects named or anonymous operation, and scopes context to a result row.

## xmltoaster-sql-row-cursor

RunQuery owns try-with-resources and next/row iteration. Useful row projection and cleanup deserve depth while exposed indexed access and retained phases impose moderate burden.

Hidden duties: Builds named row metadata and refreshes values on advancement; tracks row validity; independently closes both JDBC resources and translates failures.

Caller burden: Caller supplies matching Statement/ResultSet, sequences next/read/close and selects column indexes/access variants.

## Limits

- Purposive sample of four Java boundaries across three local repositories; not random sampling or four-language coverage.
- High examples test package audience and executable coupled mutable representation; not the quality of the entire containing service.
- Snapshots omit complete workspaces; uncertainty must remain visible.
- After first evaluation failures stay failures, expectations stay frozen. Any tuning consumes this holdout; a fresh blind sample is needed for another generalization claim.
