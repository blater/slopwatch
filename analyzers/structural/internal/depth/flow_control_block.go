package depth

import "slopslap.dev/structural/internal/facts"

func (e *controlEvaluation) block(id string, frame *controlFrame, incoming []controlInput) {
	block := e.function.blocks[id].Block
	e.phis(id, frame, incoming, block.Instructions)
	for _, in := range block.Instructions[:activeInstructionCount(block)] {
		if frame.guard == guardFalse {
			return
		}
		if !e.budget.charge(1) {
			return
		}
		switch in.Opcode {
		case facts.OpPhi, facts.OpBranch:
			continue
		case facts.OpReturn, facts.OpThrow:
			e.completeInstruction(id, frame, in)
		default:
			e.transfer(id, frame, in)
		}
		if e.budget.status != "" {
			return
		}
	}
	edges := e.function.Successors(id)
	if len(edges) == 0 {
		e.emitCompletion(id, frame)
		return
	}
	e.route(id, frame, edges)
}
func (e *controlEvaluation) transfer(block string, frame *controlFrame, in facts.Instruction) {
	frame.state.work = e.budget.state.work
	result := Apply(e.arena, frame.state, in, e.options)
	e.budget.state.work = frame.state.work
	if result.Status == TransferLimit || result.Status == TransferCancelled {
		e.budget.status = result.Status
		return
	}
	for _, reason := range result.Reasons {
		e.gap(block, in.ID, frame.guard, reason)
	}
	invocationGuard := frame.guard
	e.callCompletions(block, frame, in, result)
	for _, effect := range result.Effects {
		if !e.budget.charge(1 + len(effect.Path) + len(effect.Operands)) {
			return
		}
		e.result.Effects = append(e.result.Effects, GuardedEffect{e.guards.recipe(frame.guard), block, effect.Copy()})
	}
	for _, witness := range result.ResourceWitnesses {
		if !e.budget.charge(1 + len(witness.Evidence)) {
			return
		}
		if witness.Guard == "" {
			e.gap(block, in.ID, invocationGuard, "resource_witness_guard_unknown")
			continue
		}
		bound := e.guards.atom(witness.Guard)
		guard := e.guards.and(invocationGuard, bound)
		if guard == guardFalse {
			continue
		}
		witness.Guard = e.guards.recipe(guard)
		witness.Evidence = append([]string(nil), witness.Evidence...)
		e.result.ResourceWitnesses = append(e.result.ResourceWitnesses, witness)
	}
}
func (e *controlEvaluation) phis(block string, frame *controlFrame, incoming []controlInput, instructions []facts.Instruction) {
	pending := map[string]DomainValue{}
	for _, in := range instructions {
		if in.Opcode != facts.OpPhi {
			continue
		}
		var value DomainValue
		for i, input := range incoming {
			if !e.budget.charge(1) {
				return
			}
			source := e.function.phiInputs[in.ID][input.predecessor]
			next, ok := input.state.Value(source)
			if !ok {
				next = typedUnknown(in.Type, suppliedKind(in.ValueKind), "missing_phi_input")
				e.gap(block, in.ID, input.guard, "missing_phi_input")
			}
			if !e.budget.charge(valueWork(next) + valueWork(value)) {
				return
			}
			if i == 0 {
				value = next
			} else {
				value = selectControlRecord(e.arena, frame.state.records, e.guards.recipe(input.guard), next, value, e.budget.charge)
			}
		}
		for _, id := range in.Results {
			pending[id] = value
		}
	}
	if e.budget.status != "" {
		return
	}
	for id, value := range pending {
		frame.state.values[id] = value
	}
}
