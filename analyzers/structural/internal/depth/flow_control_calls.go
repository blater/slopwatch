package depth

import "slopslap.dev/structural/internal/facts"

func (e *controlEvaluation) callCompletions(block string, frame *controlFrame, in facts.Instruction, result TransferResult) {
	if result.NormalGuard == "" {
		return
	}
	if len(result.Rejections) != 0 && !e.supportsCallRejection() {
		e.gap(block, in.ID, frame.guard, "unsupported_caller_rejection_continuation")
		return
	}
	for _, rejection := range result.Rejections {
		if !e.budget.charge(1 + len(rejection.Values)) {
			return
		}
		guard := e.guards.and(frame.guard, e.guards.atom(rejection.Guard))
		if guard == guardFalse {
			continue
		}
		rejection.Guard = e.guards.recipe(guard)
		rejection.Origin, rejection.Block = in.ID, block
		e.result.Completions = append(e.result.Completions, rejection)
	}
	frame.guard = e.guards.and(frame.guard, e.guards.atom(result.NormalGuard))
}

// Until exceptional continuations participate in cleanup and recurrence closure,
// only acyclic callers with ordinary branch edges can forward a callee rejection.
func (e *controlEvaluation) supportsCallRejection() bool {
	if e.callRejectionSupport != 0 {
		return e.callRejectionSupport == 1
	}
	e.callRejectionSupport = 2
	if inspectCallRejectionSupport(e) {
		e.callRejectionSupport = 1
	}
	return e.callRejectionSupport == 1
}

func inspectCallRejectionSupport(e *controlEvaluation) bool {
	_, acyclic := topologicalControlOrder(e.function, e.budget.charge)
	if !acyclic {
		return false
	}
	for _, id := range e.function.reachable {
		for _, edge := range e.function.blocks[id].Block.Edges {
			if !e.budget.charge(1) {
				return false
			}
			switch edge.Kind {
			case facts.EdgeNormal, facts.EdgeTrue, facts.EdgeFalse:
			default:
				return false
			}
		}
	}
	return true
}
