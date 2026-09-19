package depth

import "slopslap.dev/structural/internal/facts"

func (e *controlEvaluation) edgeGuard(block string, frame *controlFrame, edge facts.FlowEdge) guardID {
	if edge.Kind != facts.EdgeTrue && edge.Kind != facts.EdgeFalse {
		return frame.guard
	}
	value, ok := frame.state.Value(edge.Guard)
	if !ok || value.Kind != KindBoolean || value.Unknown {
		e.gap(block, "", frame.guard, "unknown_branch_condition")
		// Preserve shared true/false correlation even when its value is unresolved.
		value.Recipe, _ = e.arena.runtime.builder.unknownResult("branch_condition", e.function.function.ID+":"+block+":"+edge.Guard)
	}
	if edge.GuardPolarity == "" {
		e.gap(block, "", frame.guard, "missing_guard_polarity")
	}
	predicate := e.guards.atom(value.Recipe)
	if edge.Kind == facts.EdgeFalse {
		predicate = e.guards.not(predicate)
	}
	return e.guards.and(frame.guard, predicate)
}
func (e *controlEvaluation) route(block string, frame *controlFrame, edges []facts.FlowEdge) {
	unconditional := 0
	for _, edge := range edges {
		if edge.Kind != facts.EdgeTrue && edge.Kind != facts.EdgeFalse {
			unconditional++
		}
	}
	residual := frame.guard
	for index, edge := range edges {
		if !e.budget.charge(1) {
			return
		}
		guard := e.edgeGuard(block, frame, edge)
		if unconditional > 1 && edge.Kind != facts.EdgeTrue && edge.Kind != facts.EdgeFalse {
			// The graph supplies possible continuations but no condition choosing
			// between them. Retain all alternatives under an explicit unknown choice.
			e.gap(block, "", frame.guard, "unknown_continuation_choice")
			predicate, _ := e.arena.runtime.builder.unknownResult("continuation_choice", e.function.function.ID+":"+block+":"+flowEdgeIdentity(edge))
			atom := e.guards.atom(predicate)
			guard = e.guards.and(residual, atom)
			if index == len(edges)-1 {
				guard = residual
			} else {
				residual = e.guards.and(residual, e.guards.not(atom))
			}
		}
		edgeRecipe := e.guards.recipe(guard)
		e.result.Edges = append(e.result.Edges, GuardedEdge{Guard: edgeRecipe, From: edge.From, To: edge.To, Kind: edge.Kind})
		if guard == guardFalse {
			continue
		}
		pending := e.edgePending(block, frame, edge, guard)
		if e.budget.status != "" {
			return
		}
		e.inputs[edge.To] = append(e.inputs[edge.To], controlInput{predecessor: block, edge: edge, guard: guard, state: frame.state, pending: pending})
	}
}
