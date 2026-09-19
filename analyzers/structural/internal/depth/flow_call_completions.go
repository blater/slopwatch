package depth

import "slopslap.dev/structural/internal/facts"

func instantiateCallCompletions(t *transferStep, result FlowEvaluation, function *CompiledFunction, actuals []DomainValue) (FlowCompletion, []FlowCompletion, []ResourceLifecycleWitness, string) {
	if result.Status != TransferOK || len(result.Gaps) != 0 {
		return FlowCompletion{}, nil, nil, "unsupported_helper_effects"
	}
	witnesses, reason := instantiateResourceWitnesses(t, result, function, actuals)
	if reason != "" {
		return FlowCompletion{}, nil, nil, reason
	}
	normal := FlowEvaluation{Status: TransferOK}
	var rejections []FlowCompletion
	g := newControlGuards(t.arena, t.budget.charge)
	covered, accepted := guardFalse, guardFalse
	for _, original := range result.Completions {
		guardValue := ScalarValue(original.Guard, "bool", KindBoolean)
		bound, reason := instantiateCallValue(t, guardValue, function, actuals)
		if reason != "" {
			return FlowCompletion{}, nil, nil, reason
		}
		guard := g.atom(bound.Recipe)
		if guard == guardFalse {
			continue
		}
		if g.and(covered, guard) != guardFalse {
			return FlowCompletion{}, nil, nil, "overlapping_helper_completions"
		}
		covered = g.or(covered, guard)
		completion, reason := bindCompletionValues(t, original, function, actuals)
		if reason != "" {
			return FlowCompletion{}, nil, nil, reason
		}
		completion.Guard = bound.Recipe
		if completion.Kind == facts.EdgeReturnError {
			if len(completion.Values) == 0 || completion.Values[len(completion.Values)-1].Kind != KindError {
				return FlowCompletion{}, nil, nil, "missing_error_result"
			}
			completion.Kind = facts.EdgeNormal
		}
		switch completion.Kind {
		case facts.EdgeNormal:
			accepted = g.or(accepted, guard)
			normal.Completions = append(normal.Completions, completion)
		case facts.EdgeThrow, facts.EdgePanic:
			rejections = append(rejections, completion)
		default:
			return FlowCompletion{}, nil, nil, "unsupported_helper_completion"
		}
	}
	if !t.budget.charge(0) {
		return FlowCompletion{}, nil, nil, "work_limit"
	}
	if covered != guardTrue {
		return FlowCompletion{}, nil, nil, "conditional_helper_completion"
	}
	coverage := g.recipe(accepted)
	if accepted == guardFalse {
		return FlowCompletion{Guard: coverage}, rejections, witnesses, ""
	}
	completion, reason := coalesceCoveredReturns(t.arena, normal, t.budget.charge, coverage)
	return completion, rejections, witnesses, reason
}

func instantiateResourceWitnesses(t *transferStep, result FlowEvaluation, function *CompiledFunction, actuals []DomainValue) ([]ResourceLifecycleWitness, string) {
	if len(result.Effects) != 0 && !result.ResourceEffectsProven {
		return nil, "unsupported_helper_effects"
	}
	if len(result.Effects) == 0 && len(result.ResourceWitnesses) == 0 {
		return nil, ""
	}
	for _, formal := range function.FormalsSnapshot() {
		if formal.Path != "" {
			return nil, "unsupported_helper_policy_resource"
		}
	}
	out := make([]ResourceLifecycleWitness, 0, len(result.ResourceWitnesses))
	for _, original := range result.ResourceWitnesses {
		if !t.budget.charge(1 + len(original.Evidence)) {
			return nil, "work_limit"
		}
		if original.Governed == "" || original.Rule == "" || original.Guard == "" {
			return nil, "invalid_resource_witness"
		}
		guardValue := ScalarValue(original.Guard, "bool", KindBoolean)
		bound, reason := instantiateCallValue(t, guardValue, function, actuals)
		if reason != "" {
			return nil, reason
		}
		if bound.Recipe == t.arena.Constant("bool", "false") {
			continue
		}
		original.Guard = bound.Recipe
		original.Evidence = append([]string(nil), original.Evidence...)
		out = append(out, original)
	}
	return out, ""
}

func bindCompletionValues(t *transferStep, original FlowCompletion, function *CompiledFunction, actuals []DomainValue) (FlowCompletion, string) {
	out := original
	out.Values = nil
	for _, value := range original.Values {
		bound, reason := instantiateCallValue(t, value, function, actuals)
		if reason != "" {
			return out, reason
		}
		out.Values = append(out.Values, bound)
	}
	return out, ""
}
