package depth

import "slopslap.dev/structural/internal/facts"

func evaluateNumericRegion(e *controlEvaluation, members []string) {
	p, ok := numericRegionShape(e, members)
	if !ok {
		for _, id := range members {
			for _, input := range e.inputs[id] {
				e.gap(id, "", input.guard, "unsupported_loop_shape")
			}
		}
		return
	}
	incoming := e.inputs[p.header]
	if len(incoming) == 0 {
		return
	}
	frame := e.merge(p.header, incoming)
	e.phis(p.header, frame, incoming, e.function.blocks[p.header].Block.Instructions)
	initial := make(map[string]DomainValue, len(p.slots))
	for _, slot := range p.slots {
		initial[slot.ID], _ = frame.state.Value(slot.ValueID)
	}
	evaluateLoopHeader(e, p.header, frame)
	if e.budget.status != "" {
		return
	}
	e.result.Blocks++
	// Probe the initial condition before introducing symbolic backedge values.
	// A known zero-trip loop never evaluates body-only failures or effects.
	if e.edgeGuard(p.header, frame, p.body) == guardFalse {
		e.route(p.header, frame, []facts.FlowEdge{p.exit})
		delete(e.inputs, p.header)
		return
	}
	for _, slot := range p.slots {
		ref, err := e.arena.RecurrenceRef(slot.Type, slot.ID, nil)
		if err != nil {
			e.gap(p.header, slot.PhiID, frame.guard, reasonCode(err))
			return
		}
		value := initial[slot.ID].Copy()
		value.Recipe = ref
		frame.state.values[slot.ValueID] = value
	}
	evaluateLoopHeader(e, p.header, frame)
	condition := numericLoopCondition(e, p, frame)
	delete(e.inputs, p.header)
	gapStart, effectStart := len(e.result.Gaps), len(e.result.Effects)
	e.route(p.header, frame, []facts.FlowEdge{p.body})
	for _, id := range p.order {
		if !e.budget.charge(1) {
			return
		}
		if id != p.header {
			evaluateControlBlock(e, id)
		}
	}
	if e.budget.status != "" {
		return
	}
	closeNumericRegion(e, p, frame, initial, condition, gapStart, effectStart)
	delete(e.inputs, p.header)
}
func evaluateLoopHeader(e *controlEvaluation, header string, frame *controlFrame) {
	for _, in := range e.function.blocks[header].Block.Instructions {
		if !e.budget.charge(1) {
			return
		}
		if in.Opcode != facts.OpPhi && in.Opcode != facts.OpBranch {
			e.transfer(header, frame, in)
		}
		if e.budget.status != "" {
			return
		}
	}
}
func numericLoopCondition(e *controlEvaluation, p numericRegion, frame *controlFrame) RecipeID {
	value, ok := frame.state.Value(p.body.Guard)
	if !ok || value.Unknown || value.Kind != KindBoolean {
		e.gap(p.header, "", frame.guard, "unknown_loop_condition")
		return UnknownRecipeID
	}
	predicate := e.guards.atom(value.Recipe)
	if p.body.Kind == facts.EdgeFalse {
		predicate = e.guards.not(predicate)
	}
	return e.guards.recipe(predicate)
}
