# Superseded by adverse-finding evidence (r33)

The neutral prior described below was rejected. Current contract: see
[Module shallowness](components/module_shallowness.md).

# Balanced incomplete-evidence calibration

Implement now; replaces the rejected r30 zero-penalty gate.

## Calculation and scope

For each meaningful abstraction retain caller burden B and deduplicated recognized
responsibility H. Unknown-call/outcome placeholder units are not recognized H.
Record U: the sum of burden of entry operations whose reachable implementation
has material uncertainty. Count each operation once, regardless of the number
of unresolved calls or diagnostic messages. U is bounded by B.

Use H_estimated = H + 0.5 U, then the existing formula
100 B / (B + 2 H_estimated). The named 0.5 constant is a neutral prior: an
entirely opaque abstraction receives 50, not a worst-case 100 or an exemption 0.
This is a calibration assumption, not discovered responsibility. Resolved source
evidence replaces the allowance. Responsibility credits are deduplicated as before.
No implementation length, call count, filename exceptions or score caps enter it.

Material uncertainty includes unresolved reachable calls, unsupported execution
alternatives, exhausted bounded traversal and missing attribution/source surface.
A gap applies only to the owning entry operation. File-wide truncation or malformed
inventory affects the whole abstraction. Missing semantic types alone do not
invalidate a body understood by source analysis. Independent abstractions retain
their own estimates and the file selects their maximum after adjustment.

## Delivery steps

1. Track uncertain burden with each source projection and abstraction detail.
2. Centralize the formula and use it for selection and report publication.
3. Preserve unadjusted estimates, recognized responsibility, allowance, limitations,
   and precise-analysis reasons. Explain the neutral allowance dynamically in info.
4. Invalidate r30 caches. Preserve passive-role zero and separate automated-fix gates.
5. Run essential four-language paired fixtures: identity, transformation, resolved
   helper extraction, uncertainty without unrelated contamination, partial versus
   entirely unknown surface, and responsibility monotonicity at fixed uncertainty.
6. Run rebuilt CLI on QueryResultRow, KeyedPath and workspace.go and publish actual
   before/after values. Preserve non-SHALLOW scoring formula and measures.

## Acceptance and limits

Unknown-only surfaces remain neutral as interface size grows. More established
responsibility lowers the score at fixed burden and uncertainty; more established
caller burden raises it at fixed responsibility. A known identity remains 100;
passive proven carriers remain 0. Equivalent source-supported helper extraction
and loss of semantic types must not increase penalties.

Losing the implementation itself cannot guarantee an unchanged or lower rating
without knowing what was lost. The neutral prior intentionally does not make that
impossible guarantee: an opaque call may score above a proven deep implementation
or below a proven shallow one. Keep this distinction explicit rather than hiding
it with either extreme. Semantic expansion and enterprise profiling are separate
work; this change adds bounded bookkeeping, not additional project scans.
