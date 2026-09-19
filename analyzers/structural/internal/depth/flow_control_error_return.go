package depth

import "slopslap.dev/structural/internal/facts"

func (e *controlEvaluation) completeErrorReturn(block string, frame *controlFrame, in facts.Instruction, values []DomainValue) bool {
	if in.Opcode != facts.OpReturn || len(values) == 0 || values[len(values)-1].Kind != KindError {
		return false
	}
	presence, err := errorPresence(e.arena, values[len(values)-1].Recipe, e.budget.charge, map[RecipeID]RecipeID{})
	if err != nil {
		e.gap(block, in.ID, frame.guard, reasonCode(err))
		return false
	}
	failed := e.guards.atom(presence)
	frame.pending = nil
	for _, outcome := range []struct {
		guard guardID
		kind  facts.EdgeKind
	}{
		{e.guards.and(frame.guard, failed), facts.EdgeReturnError},
		{e.guards.and(frame.guard, e.guards.not(failed)), facts.EdgeNormal},
	} {
		if outcome.guard != guardFalse {
			frame.pending = append(frame.pending, pendingCompletion{guard: outcome.guard, kind: outcome.kind, origin: in.ID, values: values})
		}
	}
	return true
}
