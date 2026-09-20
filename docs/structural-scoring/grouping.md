# Shared grouping design notes

The grouping scorer charges the maximum weighted control-flow severity for each
stable routine and sums those routine groups. It keeps the raw subject values
and all signals in the report while assigning the additive group amount to a
deterministic winning component. Nesting findings are indexed by source path
and routine symbol, then associated with the smallest enclosing routine range;
unresolved findings remain in deterministic limitation buckets.

The type CYCLO/GOD audit keeps distinct signals with a bounded overlap rule.
Type CYCLO is WMC (method cyclomatic totals plus interface methods). Its
projected type contribution subtracts the already charged weighted routine
group contributions for reliably associated methods and retains any
non-method/interface residual. Raw type WMC remains visible for display and
audit, and disabling routine signals therefore restores the type contribution.
GOD remains an additional contribution because WMC is only its gate; its
magnitude comes from foreign access and cohesion, which are not represented by
the routine CYCLO inputs.
The type residual is used only when compact inputs are present; legacy reports
retain their prior contribution and carry a limitation.

Compact projections persist component aggregation, immutable subject severity
and resolved routine/owner keys plus attribution. Formula parameters remain in
the versioned catalog; neither full analyzer evidence nor repeated formula
metadata is needed for reweighting. Attribution stores its winner once and
lists the remaining supporting signals separately. Legacy reports
without the definition retain their existing contributions and carry an
explicit limitation so missing metadata cannot masquerade as zero.

## Grouping checkpoint

`make build` passed, including packaged smoke tests. With unchanged severity
curves, the existing Go, Java and Rust balanced references fall from 20.85427
to 14.85427: the six-point nesting charge is already covered by COG. Raw
measurements are unchanged. See [reference evidence](../evidence/structural-scoring/grouping-reference.json).

The frozen 64-file calibration set retains its old total SCORE, 122.42713,
because its control-flow measurements sit below the old thresholds. Every
recorded raw structural value matches the baseline. This separates the need
for continuous severity from the overlap correction; see
[fixture evidence](../evidence/structural-scoring/grouping-fixture.json).
