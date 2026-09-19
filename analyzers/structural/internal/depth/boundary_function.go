package depth

import (
	"slopslap.dev/structural/internal/facts"
	"sort"
	"strings"
)

func proveFunctionOutcomes(summaries *functionSummaries, id string, boundary facts.BoundaryIdentity) flowResponsibilities {
	evaluation := summaries.evaluate(id)
	variants, policies, reason := validationFlowVariants(summaries.arena, evaluation, summaries.budget.charge)
	out := flowResponsibilities{dependencies: evaluation.Dependencies}
	if reason != "" {
		out.reasons = append(out.reasons, reason)
		return out
	}
	function, _ := summaries.artifact.Function(id)
	for _, index := range policies {
		formals := function.FormalsSnapshot()
		if index >= len(formals) || formals[index].Path == "" {
			out.reasons = append(out.reasons, "missing_policy_slot")
			return out
		}
		out.policies = appendUniqueStrings(out.policies, formals[index].Path)
	}
	seen := map[string]bool{}
	for _, variant := range variants {
		if supportedFailureOnly(summaries.arena, variant, summaries.budget.charge) {
			continue
		}
		proof := proveEvaluatedOutcomes(summaries, id, variant, boundary)
		out.reasons = append(out.reasons, proof.reasons...)
		out.policies = appendUniqueStrings(out.policies, proof.policies...)
		out.obligations = append(out.obligations, proof.obligations...)
		for _, alternative := range proof.alternatives {
			sort.Strings(alternative)
			key := strings.Join(alternative, "\n")
			if !seen[key] {
				seen[key] = true
				out.alternatives = append(out.alternatives, alternative)
			}
			if len(out.alternatives) > 32 {
				out.reasons = append(out.reasons, "alternative_limit")
				return out
			}
		}
	}
	if len(out.alternatives) == 0 {
		out.reasons = append(out.reasons, "missing_normal_behavior")
	}
	if !summaries.budget.charge(0) {
		out.reasons = append(out.reasons, "work_limit")
	}
	return out
}

func supportedFailureOnly(arena *RecipeArena, evaluation FlowEvaluation, charge func(int) bool) bool {
	if evaluation.Status != TransferOK || len(evaluation.Gaps) != 0 || len(evaluation.Effects) != 0 {
		return false
	}
	g := newControlGuards(arena, charge)
	coverage := guardFalse
	for _, completion := range evaluation.Completions {
		if completion.Kind != facts.EdgeThrow && completion.Kind != facts.EdgePanic && completion.Kind != facts.EdgeReturnError {
			return false
		}
		coverage = g.or(coverage, g.atom(completion.Guard))
	}
	return charge(0) && coverage == guardTrue
}
