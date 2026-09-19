package depth

import "slopslap.dev/structural/internal/facts"

func (e *controlEvaluation) values(block, instruction string, guard guardID, state *TransferState, ids []string) []DomainValue {
	out := make([]DomainValue, 0, len(ids))
	for _, id := range ids {
		value, ok := state.Value(id)
		if !ok {
			value = UnknownValue(UnknownRecipeID, "", "missing_completion_value")
			e.gap(block, instruction, guard, "missing_completion_value")
		}
		if !e.budget.charge(valueWork(value)) {
			return nil
		}
		if value.Unknown {
			e.gap(block, instruction, guard, firstReason(value.Reasons, "unknown_completion_value"))
		}
		out = append(out, value)
	}
	return out
}
func (e *controlEvaluation) completeInstruction(block string, frame *controlFrame, in facts.Instruction) {
	kind := facts.EdgeNormal
	if in.Opcode == facts.OpReturn && in.CompletionKind == facts.EdgeReturnError {
		kind = facts.EdgeReturnError
	}
	if in.Opcode == facts.OpThrow {
		kind = facts.EdgeThrow
	}
	values := e.values(block, in.ID, frame.guard, frame.state, in.Operands)
	if e.budget.status != "" {
		return
	}
	if e.completeErrorReturn(block, frame, in, values) {
		return
	}
	frame.pending = []pendingCompletion{{guard: frame.guard, kind: kind, origin: in.ID, values: values}}
}
func (e *controlEvaluation) emitCompletion(block string, frame *controlFrame) {
	pending := append([]pendingCompletion(nil), frame.pending...)
	covered := guardFalse
	for _, p := range pending {
		covered = e.guards.or(covered, p.guard)
	}
	normal := e.guards.and(frame.guard, e.guards.not(covered))
	if normal != guardFalse {
		pending = append(pending, pendingCompletion{guard: normal, kind: facts.EdgeNormal, origin: block})
	}
	for _, p := range pending {
		if !e.budget.charge(1) {
			return
		}
		values := make([]DomainValue, 0, len(p.values))
		for _, value := range p.values {
			if !e.budget.charge(valueWork(value)) {
				return
			}
			values = append(values, effectiveValue(frame.state, value))
		}
		guard := e.guards.recipe(p.guard)
		if e.budget.status != "" {
			return
		}
		e.result.Completions = append(e.result.Completions, FlowCompletion{Guard: guard, Kind: p.kind, ErrorTag: p.tag, Values: values, Origin: p.origin, Block: block})
	}
}
func (e *controlEvaluation) edgePending(block string, frame *controlFrame, edge facts.FlowEdge, guard guardID) []pendingCompletion {
	switch edge.Kind {
	case facts.EdgeThrow, facts.EdgePanic, facts.EdgeReturnError:
		ids := []string{}
		if edge.Payload != "" {
			ids = append(ids, edge.Payload)
		}
		values := e.values(block, "", guard, frame.state, ids)
		return []pendingCompletion{{guard: guard, kind: edge.Kind, tag: edge.ErrorTag, origin: block + ":" + edge.To + ":" + string(edge.Kind), values: values}}
	default:
		return frame.pending
	}
}
