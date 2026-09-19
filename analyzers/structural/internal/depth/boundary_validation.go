package depth

import (
	"crypto/sha256"
	"fmt"
	"slopslap.dev/structural/internal/facts"
)

// Rejection completions establish validation, never an empty successful service.
// Compound rejection domains stay unknown until their bypass alternatives can
// be separated; crediting a conditional check as universal would overstate H.
func validatedNormalFlow(arena *RecipeArena, result FlowEvaluation, boundary facts.BoundaryIdentity, charge func(int) bool) (FlowEvaluation, RecipeID, facts.Obligation, string) {
	normal := result
	normal.Completions = nil
	if result.Status != TransferOK || len(result.Gaps) != 0 || len(result.Effects) != 0 {
		return normal, UnknownRecipeID, facts.Obligation{}, "incomplete_validation_flow"
	}
	guards := newControlGuards(arena, charge)
	covered, accepted, rejected := guardFalse, guardFalse, guardFalse
	for _, completion := range result.Completions {
		if !charge(1) {
			return normal, UnknownRecipeID, facts.Obligation{}, "work_limit"
		}
		guard := guards.atom(completion.Guard)
		if guards.and(covered, guard) != guardFalse {
			return normal, UnknownRecipeID, facts.Obligation{}, "overlapping_completions"
		}
		covered = guards.or(covered, guard)
		switch completion.Kind {
		case facts.EdgeNormal:
			accepted = guards.or(accepted, guard)
			normal.Completions = append(normal.Completions, completion)
		case facts.EdgeThrow, facts.EdgePanic, facts.EdgeReturnError:
			rejected = guards.or(rejected, guard)
		default:
			return normal, UnknownRecipeID, facts.Obligation{}, "unsupported_rejection_completion"
		}
	}
	if !charge(0) {
		return normal, UnknownRecipeID, facts.Obligation{}, "work_limit"
	}
	if covered != guardTrue || accepted == guardFalse {
		return normal, UnknownRecipeID, facts.Obligation{}, "missing_normal_behavior"
	}
	coverage := guards.recipe(accepted)
	if !charge(0) {
		return normal, UnknownRecipeID, facts.Obligation{}, "work_limit"
	}
	if rejected == guardFalse {
		return normal, coverage, facts.Obligation{}, ""
	}
	obligation, reason := proveRejectionGuard(guards, rejected, normal.Completions, boundary)
	return normal, coverage, obligation, reason
}

func proveRejectionGuard(guards *controlGuards, rejected guardID, normal []FlowCompletion, boundary facts.BoundaryIdentity) (facts.Obligation, string) {
	node := guards.nodes[rejected]
	if node.yes > guardTrue || node.no > guardTrue {
		return facts.Obligation{}, "conditional_validation_domain"
	}
	outcome := guards.arena.BuildOutcome(node.predicate)
	if outcome.Unknown {
		return facts.Obligation{}, outcome.Reason
	}
	input := recipeAny(guards.arena, node.predicate, guards.charge, func(n RecipeNode) bool { return n.Kind == KindFormal })
	if !guards.charge(0) {
		return facts.Obligation{}, "work_limit"
	}
	if !input {
		return facts.Obligation{}, "unsupported_validation_input"
	}
	relevant, reason := validationRelevance(guards, node.predicate, normal)
	if reason != "" || !relevant {
		return facts.Obligation{}, reason
	}
	governed := string(guards.recipe(rejected))
	if !guards.charge(0) {
		return facts.Obligation{}, "work_limit"
	}
	key := fmt.Sprintf("V:%x", sha256.Sum256([]byte(boundary.String()+":"+governed)))
	return facts.Obligation{ID: key, Category: facts.ObligationValidation, Governed: governed, Rule: "input_rejection_guard"}, ""
}

func validationRelevance(guards *controlGuards, predicate RecipeID, normal []FlowCompletion) (bool, string) {
	inputs := map[int]bool{}
	recipeAny(guards.arena, predicate, guards.charge, func(n RecipeNode) bool {
		if n.Kind == KindFormal {
			inputs[n.FormalIndex] = true
		}
		return false
	})
	matched := 0
	for _, completion := range normal {
		relevant := len(completion.Values) == 1 && completion.Values[0].Kind == KindError
		for _, value := range completion.Values {
			if value.Kind == KindError {
				continue
			}
			if recipeAny(guards.arena, value.Recipe, guards.charge, func(n RecipeNode) bool { return n.Kind == KindFormal && inputs[n.FormalIndex] }) {
				relevant = true
			}
		}
		if relevant {
			matched++
		}
	}
	if !guards.charge(0) {
		return false, "work_limit"
	}
	if matched != 0 && matched != len(normal) {
		return false, "conditional_validation_outcome"
	}
	return matched != 0, ""
}
