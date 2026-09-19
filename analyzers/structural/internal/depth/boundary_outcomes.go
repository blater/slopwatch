package depth

import (
	"crypto/sha256"
	"fmt"
	"slopslap.dev/structural/internal/facts"
)

type flowResponsibilities struct {
	alternatives []facts.ObligationSet
	obligations  []facts.Obligation
	reasons      []string
	dependencies []string
	policies     []string
}

func proveEvaluatedOutcomes(summaries *functionSummaries, id string, evaluation FlowEvaluation, boundary facts.BoundaryIdentity) flowResponsibilities {
	out := flowResponsibilities{dependencies: evaluation.Dependencies}
	appendEvaluationStatus(&out, evaluation)
	resources := proveResourceLifecycles(summaries, id, evaluation, boundary)
	out.reasons = append(out.reasons, resources.reasons...)
	if len(evaluation.Effects) != 0 && !resources.allEffectsProven {
		out.reasons = append(out.reasons, "unsupported_effect_proof")
	}
	evaluation = resourceSafeEvaluation(evaluation, resources)
	normal, coverage, validation, reason := validatedNormalFlow(summaries.arena, evaluation, boundary, summaries.budget.charge)
	if reason != "" {
		out.reasons = append(out.reasons, reason)
		return out
	}
	completion, reason := coalescedNormalOutcome(summaries, normal, coverage)
	if reason != "" {
		out.reasons = append(out.reasons, reason)
		return out
	}
	completion.Values, reason = constrainNormalValues(summaries.arena, completion, summaries.budget.charge)
	if reason != "" {
		out.reasons = append(out.reasons, reason)
		return out
	}
	policies, reason := outcomePolicyInputs(summaries.arena, completion.Values, summaries.budget.charge)
	if reason != "" {
		out.reasons = append(out.reasons, reason)
		return out
	}
	if len(resources.obligations) != 0 && len(policies) != 0 {
		// Policy binding currently rewrites completion guards/values but does not
		// retain effect alternatives. Keep this combination partial until those
		// resource guards can be projected without cross-variant credit.
		out.reasons = append(out.reasons, "resource_policy_variant_unknown")
		resources.obligations = nil
	}
	policyPaths, reason := evaluatedPolicyPaths(summaries, id, policies)
	if reason != "" {
		out.reasons = append(out.reasons, reason)
		return out
	}
	out.policies = append(out.policies, policyPaths...)
	variants, reason := policyOutcomeVariants(summaries.arena, completion.Values, policies, summaries.budget.charge)
	if reason != "" {
		out.reasons = append(out.reasons, reason)
		return out
	}
	appendOutcomeAlternatives(&out, summaries.arena, variants, resources.obligations, validation, boundary)
	if !summaries.budget.charge(0) {
		out.reasons = append(out.reasons, "work_limit")
	}

	return out
}

func evaluatedPolicyPaths(summaries *functionSummaries, id string, policies []int) ([]string, string) {
	function, ok := summaries.artifact.Function(id)
	if !ok {
		return nil, "missing_policy_slot"
	}
	formals := function.FormalsSnapshot()
	paths := make([]string, 0, len(policies))
	for _, index := range policies {
		if index < 0 || index >= len(formals) || formals[index].Path == "" {
			return nil, "missing_policy_slot"
		}
		paths = append(paths, formals[index].Path)
	}
	return paths, ""
}

func appendEvaluationStatus(out *flowResponsibilities, evaluation FlowEvaluation) {
	for _, gap := range evaluation.Gaps {
		out.reasons = append(out.reasons, gap.Reason)
	}
	if evaluation.Status != TransferOK {
		out.reasons = append(out.reasons, "incomplete_flow_evaluation")
	}
}

func resourceSafeEvaluation(evaluation FlowEvaluation, resources resourceLifecycleProof) FlowEvaluation {
	if len(evaluation.Effects) != 0 && !resources.allEffectsProven {
		return evaluation
	}
	if resources.allEffectsProven {
		evaluation.Effects = nil
	}
	return evaluation
}

func coalescedNormalOutcome(summaries *functionSummaries, normal FlowEvaluation, coverage RecipeID) (FlowCompletion, string) {
	normal = withoutNormalErrorChannel(normal)
	return coalesceCoveredReturns(summaries.arena, normal, summaries.budget.charge, coverage)
}

func appendOutcomeAlternatives(out *flowResponsibilities, arena *RecipeArena, variants [][]DomainValue, resources []facts.Obligation, validation facts.Obligation, boundary facts.BoundaryIdentity) {
	for _, values := range variants {
		set, obligations, reasons := outcomeAlternative(arena, values, resources, validation, boundary)
		out.alternatives = append(out.alternatives, set)
		out.obligations = append(out.obligations, obligations...)
		out.reasons = append(out.reasons, reasons...)
	}
}

func outcomeAlternative(arena *RecipeArena, values []DomainValue, resources []facts.Obligation, validation facts.Obligation, boundary facts.BoundaryIdentity) (facts.ObligationSet, []facts.Obligation, []string) {
	set := facts.ObligationSet{}
	obligations := append([]facts.Obligation(nil), resources...)
	for _, obligation := range resources {
		set = append(set, obligation.ID)
	}
	if validation.ID != "" {
		set = append(set, validation.ID)
		obligations = append(obligations, validation)
	}
	var reasons []string
	for _, value := range values {
		obligation, reason := proveReturnedOutcome(arena, value, boundary)
		if reason != "" {
			reasons = append(reasons, reason)
		}
		if obligation.ID != "" {
			set = append(set, obligation.ID)
			obligations = append(obligations, obligation)
		}
	}
	return set, obligations, reasons
}
func proveReturnedOutcome(arena *RecipeArena, value DomainValue, boundary facts.BoundaryIdentity) (facts.Obligation, string) {
	if value.Unknown || value.Aliases.IsUnknown() || len(value.Aliases.Roots()) != 0 {
		return facts.Obligation{}, "unknown_returned_outcome"
	}
	if value.Kind != KindNumeric && value.Kind != KindBoolean && value.Kind != KindString {
		return facts.Obligation{}, "unsupported_returned_outcome"
	}
	outcome := arena.BuildOutcome(value.Recipe)
	if outcome.Unknown {
		return facts.Obligation{}, outcome.Reason
	}
	switch arena.Classify(value.Recipe) {
	case TransformationIdentity, TransformationConstant:
		return facts.Obligation{}, ""
	case TransformationPrimitive:
		governed := string(value.Recipe)
		key := fmt.Sprintf("X:%x", sha256.Sum256([]byte(boundary.String()+":"+governed)))
		rule := "connected_numeric_outcome"
		if textConcatenation(arena, value.Recipe) {
			rule = "connected_text_outcome"
		}
		return facts.Obligation{ID: key, Category: facts.ObligationTransform, Governed: governed, Rule: rule}, ""
	default:
		return facts.Obligation{}, "unknown_transformation"
	}
}

func withoutNormalErrorChannel(input FlowEvaluation) FlowEvaluation {
	out := input
	out.Completions = append([]FlowCompletion(nil), input.Completions...)
	for index, completion := range out.Completions {
		if len(completion.Values) > 0 && completion.Values[len(completion.Values)-1].Kind == KindError {
			out.Completions[index].Values = completion.Values[:len(completion.Values)-1]
		}
	}
	return out
}
