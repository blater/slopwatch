package depth

// Escape follows current containment, never other possibilities in a may-target
// set. Ownership changes are staged and apply only to escaped path prefixes.
func transferEscape(t *transferStep) {
	value, ok := t.operand(firstOperand(t.in))
	if !ok {
		t.partial(ReasonUnknownOperand)
		t.delta.unknownMemory = true
		return
	}
	roots, complete := reachableRoots(t, value.Aliases)
	if t.budget.status != "" {
		return
	}
	for _, root := range roots {
		t.delta.escapes = append(t.delta.escapes, root)
		t.effect("escape", root, "")
	}
	if !complete {
		t.partial("unknown_escape")
		t.delta.unknownMemory = true
	}
}
func reachableRoots(t *transferStep, aliases AliasSet) ([]AliasRoot, bool) {
	if aliases.IsUnknown() {
		return aliases.SupportingRoots(), false
	}
	queue := aliases.Roots()
	seen := map[string]bool{}
	var out []AliasRoot
	for len(queue) > 0 {
		if !t.budget.charge(1) {
			return out, false
		}
		root := queue[0]
		queue = queue[1:]
		key := aliasKey(root)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, root)
		children, complete := storedChildren(t, root)
		queue = append(queue, children...)
		if !complete {
			return append(out, children...), false
		}
	}
	return out, true
}
func storedChildren(t *transferStep, root AliasRoot) ([]AliasRoot, bool) {
	locations := map[string]bool{}
	var children []AliasRoot
	writes, complete := t.state.memory.locations(root, t.budget.charge)
	if !complete {
		return nil, false
	}
	for _, write := range writes {
		if !t.budget.charge(1 + len(write.Root.Path)) {
			return children, false
		}
		key := aliasKey(write.Root)
		if locations[key] || !pathPrefixOverlap(root.Path, write.Root.Path) {
			continue
		}
		locations[key] = true
		fallback := func(AliasRoot, []PathSegment) DomainValue {
			return typedUnknown(write.Value.Type, write.Value.Kind, "prior_storage")
		}
		value, _, complete := t.state.memory.readBounded(write.Root, nil, fallback, t.budget.charge)
		if !complete {
			return children, false
		}
		if !t.budget.charge(valueWork(value)) {
			return children, false
		}
		children = append(children, value.Aliases.SupportingRoots()...)
		if value.Aliases.IsUnknown() {
			return children, false
		}
	}
	return children, true
}
func pathPrefixOverlap(prefix, path []PathSegment) bool {
	if len(prefix) > len(path) {
		return false
	}
	return pathsOverlap(prefix, path[:len(prefix)])
}
