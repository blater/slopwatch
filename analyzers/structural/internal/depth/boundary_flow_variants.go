package depth

import (
	"slopslap.dev/structural/internal/facts"
)

func validationFlowVariants(arena *RecipeArena, input FlowEvaluation, charge func(int) bool) ([]FlowEvaluation, []int, string) {
	selectors, reason := validationSelectors(arena, input, charge)
	if reason != "" || len(selectors) == 0 {
		return []FlowEvaluation{input}, nil, reason
	}
	policies, reason := normalFlowPolicies(arena, input, charge)
	if reason != "" {
		return nil, nil, reason
	}
	variants := []FlowEvaluation{input}
	for _, index := range selectors {
		next := []FlowEvaluation{}
		seen := map[string]bool{}
		for _, variant := range variants {
			for _, literal := range []string{"false", "true"} {
				bound, reason := bindFlowBoolean(arena, variant, index, literal, charge)
				if reason != "" {
					return nil, nil, reason
				}
				key, ok := flowVariantKey(bound, charge)
				if !ok {
					return nil, nil, "work_limit"
				}
				if !seen[string(key)] {
					seen[string(key)] = true
					next = append(next, bound)
				}
				if len(next) > 32 {
					return nil, nil, "alternative_limit"
				}
			}
		}
		variants = next
	}
	return variants, policies, ""
}

func bindFlowBoolean(arena *RecipeArena, input FlowEvaluation, index int, literal string, charge func(int) bool) (FlowEvaluation, string) {
	replacements := map[RecipeID]RecipeID{}
	for _, typ := range []string{"bool", "boolean"} {
		formal, _ := arena.Formal(index, typ)
		replacements[formal] = arena.Constant(typ, literal)
	}
	w := recipeRewriter{builder: arena.runtime.builder, replacements: replacements, memo: map[RecipeID]RecipeID{}, charge: charge}
	out := input
	out.Completions = nil
	g := newControlGuards(arena, charge)
	for _, original := range input.Completions {
		guard, err := w.rewrite(original.Guard)
		if err != nil {
			return out, reasonCode(err)
		}
		if g.atom(guard) == guardFalse {
			continue
		}
		completion := original
		completion.Guard = guard
		completion.Values = nil
		for _, value := range original.Values {
			recipe, err := w.rewrite(value.Recipe)
			if err != nil {
				return out, reasonCode(err)
			}
			value.Recipe = recipe
			completion.Values = append(completion.Values, value)
		}
		out.Completions = append(out.Completions, completion)
	}
	if !charge(0) {
		return out, "work_limit"
	}
	return out, ""
}

func normalFlowPolicies(arena *RecipeArena, input FlowEvaluation, charge func(int) bool) ([]int, string) {
	normal := input
	normal.Completions = nil
	g := newControlGuards(arena, charge)
	coverage := guardFalse
	for _, completion := range input.Completions {
		if completion.Kind == facts.EdgeNormal {
			normal.Completions = append(normal.Completions, completion)
			coverage = g.or(coverage, g.atom(completion.Guard))
		}
	}
	normal = withoutNormalErrorChannel(normal)
	completion, reason := coalesceCoveredReturns(arena, normal, charge, g.recipe(coverage))
	if reason != "" {
		return nil, reason
	}
	return outcomePolicyInputs(arena, completion.Values, charge)
}

func flowVariantKey(evaluation FlowEvaluation, charge func(int) bool) ([]byte, bool) {
	var key []byte
	for _, completion := range evaluation.Completions {
		if !charge(3 + len(completion.ErrorTag) + len(completion.Values)) {
			return nil, false
		}
		key = appendID(key, RecipeID(completion.Kind))
		key = appendID(key, completion.Guard)
		key = appendID(key, RecipeID(completion.ErrorTag))
		for _, value := range completion.Values {
			key = appendID(key, value.Recipe)
		}
		key = appendID(key, "")
	}
	return key, true
}
