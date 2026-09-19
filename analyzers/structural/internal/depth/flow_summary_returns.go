package depth

import "slopslap.dev/structural/internal/facts"

// Coalesce pure normal returns into one guarded value relation. This preserves
// data encodings such as bool→0/1, rather than mistaking each literal branch for
// an independent constant service. Policy alternatives are split later.
func coalesceCoveredReturns(arena *RecipeArena, result FlowEvaluation, charge func(int) bool, coverage RecipeID) (FlowCompletion, string) {
	if len(result.Gaps) != 0 {
		return FlowCompletion{}, result.Gaps[0].Reason
	}
	if result.Status != TransferOK || len(result.Effects) != 0 {
		return FlowCompletion{}, "unsupported_helper_effects"
	}
	if len(result.Completions) == 0 {
		return FlowCompletion{}, "missing_normal_behavior"
	}
	guards := newControlGuards(arena, charge)
	covered := guardFalse
	out := FlowCompletion{Kind: facts.EdgeNormal}
	for index, completion := range result.Completions {
		if !charge(1 + len(completion.Values)) {
			return out, "work_limit"
		}
		if completion.Kind != facts.EdgeNormal {
			return out, "unsupported_helper_completion"
		}
		guard := guards.atom(completion.Guard)
		if guards.and(covered, guard) != guardFalse {
			return out, "overlapping_helper_completions"
		}
		covered = guards.or(covered, guard)
		if index == 0 {
			out.Values = append([]DomainValue(nil), completion.Values...)
			continue
		}
		if len(completion.Values) != len(out.Values) {
			return out, "inconsistent_return_arity"
		}
		for position, value := range completion.Values {
			previous := out.Values[position]
			if value.Type != previous.Type || value.Kind != previous.Kind || !summaryResultKind(value.Kind) {
				return out, "inconsistent_return_type"
			}
			recipe, err := guardedSummaryValue(guards, guard, value.Type, value.Recipe, previous.Recipe, map[guardID]RecipeID{})
			if err != nil {
				return out, reasonCode(err)
			}
			merged := combineLoopMetadata(previous, value)
			if value.Kind == KindError {
				aligned := previous.Copy()
				aligned.Recipe = value.Recipe
				merged = aligned.Join(value)
			}
			merged.Recipe = recipe
			out.Values[position] = merged
		}
	}
	if covered != guards.atom(coverage) {
		return out, "conditional_helper_completion"
	}
	out.Guard = guards.recipe(covered)
	return out, ""
}

func guardedSummaryValue(g *controlGuards, guard guardID, typ string, yes, no RecipeID, memo map[guardID]RecipeID) (RecipeID, error) {
	if !g.charge(1) {
		return UnknownRecipeID, &RecipeError{Code: "work_limit"}
	}
	if guard == guardTrue {
		return yes, nil
	}
	if guard == guardFalse {
		return no, nil
	}
	if result, ok := memo[guard]; ok {
		return result, nil
	}
	node := g.nodes[guard]
	left, err := guardedSummaryValue(g, node.yes, typ, yes, no, memo)
	if err != nil {
		return UnknownRecipeID, err
	}
	right, err := guardedSummaryValue(g, node.no, typ, yes, no, memo)
	if err != nil {
		return UnknownRecipeID, err
	}
	result, err := g.arena.Select(typ, node.predicate, left, right)
	if err == nil {
		memo[guard] = result
	}
	return result, err
}
