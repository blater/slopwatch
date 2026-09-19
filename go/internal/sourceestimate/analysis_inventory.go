package sourceestimate

// unitInventory belongs to one analysis unit. Tokens and cached inventories are
// read-only after parsing; caller preparation replaces this cache when annotation
// context changes. Units are value-passed, so lazy cache fills share this pointer.
// Analysis passes are sequential; no cache is shared between analysis calls.
type unitInventory struct {
	fields      map[string]map[string]gradedSurfaceField
	constraints map[string][]gradedConstraint
}

// Only full-unit roots in the prepared annotation context may use this wrapper.
// Selected roots and contextual receiver copies must use gradedOwnerConstraints.
// Returned constraints (including their nested slices) are read-only.
func gradedFullOwnerConstraints(u unit, owner string) []gradedConstraint {
	if u.inventory == nil {
		return gradedOwnerConstraints(u, owner, u.ops)
	}
	if constraints, ok := u.inventory.constraints[owner]; ok {
		return constraints
	}
	constraints := gradedOwnerConstraints(u, owner, u.ops)
	if u.inventory.constraints == nil {
		u.inventory.constraints = make(map[string][]gradedConstraint)
	}
	u.inventory.constraints[owner] = constraints
	return constraints
}

// operationBodies is copied with contextual operations. Slice identity and length
// prevent a replaced, shortened or substituted body from reusing original results.
// Normalization depends only on immutable tokens and language, never owner/ID.
type operationBodies struct {
	source            *token
	length            int
	language          string
	pruned, eager     []token
	ready, eagerReady bool
	isNil             bool
}

// normalizedPrunedBody and normalizedEagerBody return read-only capped slices.
// Token mutation belongs to parsing (locateTokens) or cloned parameter marking;
// consumers of normalized bodies only inspect, slice, or append to fresh buffers.
func normalizedPrunedBody(op *operation) []token {
	var source *token
	if len(op.body) > 0 {
		source = &op.body[0]
	}
	c := &op.normalized
	if !c.ready || c.source != source || c.length != len(op.body) || c.language != op.language || c.isNil != (op.body == nil) {
		*c = operationBodies{source: source, length: len(op.body), language: op.language,
			pruned: pruneDeadFalseBranches(op.body), ready: true, isNil: op.body == nil}
	}
	return c.pruned
}

func normalizedEagerBody(op *operation) []token {
	body := normalizedPrunedBody(op)
	if !op.normalized.eagerReady {
		op.normalized.eager = gradedEagerBody(body, op.language)
		op.normalized.eagerReady = true
	}
	return op.normalized.eager
}
