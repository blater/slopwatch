package depth

import (
	"slopslap.dev/structural/internal/facts"
	"sort"
)

// AssessFlowBoundaries joins adapter-owned surface inventories with evaluated
// behavior. Missing flow or unsupported proofs withhold the affected score.
// Pre-assessed inputs without flows remain supported by the standalone scorer.
func AssessFlowBoundaries(input *facts.DepthFacts) []facts.BoundaryAssessment {
	return AssessFlowBoundariesProgress(input, nil)
}

// AssessFlowBoundariesProgress retains shared flow compilation and delivers each
// boundary immediately after its assessment.
func AssessFlowBoundariesProgress(input *facts.DepthFacts, emit func(facts.BoundaryAssessment)) []facts.BoundaryAssessment {
	artifacts := map[string]*CompiledArtifact{}
	for _, flow := range input.Flows {
		compiled, _, err := CompileFlowArtifactPartial(flow)
		if err == nil {
			artifacts[flow.Artifact] = compiled
		}
	}
	output := make([]facts.BoundaryAssessment, 0, len(input.Boundaries))
	for _, inventory := range input.Boundaries {
		assessment := assessBoundaryFlow(inventory, artifacts[inventory.Identity.Artifact])
		output = append(output, assessment)
		if emit != nil {
			emit(assessment)
		}
	}
	return output
}
func assessBoundaryFlow(inventory facts.BoundaryAssessment, artifact *CompiledArtifact) facts.BoundaryAssessment {
	out := ApplyBoundaryRoles(inventory)
	if out.State == facts.KnowledgeNotApplicable {
		return out
	}
	out.Knowledge = make(map[string]facts.Knowledge, len(inventory.Knowledge))
	for key, value := range inventory.Knowledge {
		out.Knowledge[key] = value
	}
	out.Reasons = append([]facts.Reason(nil), inventory.Reasons...)
	out.Obligations = nil
	out.Dependencies = append([]string(nil), inventory.Dependencies...)
	out.FamilyAlternatives = nil
	out.FamilyAlternativeIDs = nil
	if artifact == nil || len(out.RouteFamilies) == 0 {
		boundaryFlowGap(&out, "missing_boundary_flow")
		return out
	}
	summaries := newFunctionSummaries(NewRecipeArena(), artifact, TransferOptions{})
	obligations := map[string]facts.Obligation{}
	results := map[string]flowResponsibilities{}
	out.RouteFamilies = append([]facts.RouteFamily(nil), inventory.RouteFamilies...)
	for familyIndex, family := range out.RouteFamilies {
		out.RouteFamilies[familyIndex].Routes = append([]facts.Route(nil), family.Routes...)
		alternatives := []facts.ObligationSet{}
		for routeIndex, route := range family.Routes {
			proof, ok := results[route.TargetFunctionID]
			if !ok {
				proof = proveFunctionOutcomes(summaries, route.TargetFunctionID, out.Identity)
				results[route.TargetFunctionID] = proof
			}
			out.RouteFamilies[familyIndex].Routes[routeIndex].RequiredPolicies = appendUniqueStrings(append([]string(nil), route.RequiredPolicies...), proof.policies...)
			alternatives = append(alternatives, proof.alternatives...)
			out.Dependencies = appendUniqueStrings(out.Dependencies, proof.dependencies...)
			for _, reason := range proof.reasons {
				boundaryFlowGap(&out, reason)
			}
			for _, obligation := range proof.obligations {
				obligations[obligation.ID] = obligation
			}
		}
		if len(alternatives) == 0 {
			boundaryFlowGap(&out, "missing_normal_behavior")
		}
		out.FamilyAlternatives = append(out.FamilyAlternatives, alternatives)
		out.FamilyAlternativeIDs = append(out.FamilyAlternativeIDs, family.ID)
	}
	keys := make([]string, 0, len(obligations))
	for id := range obligations {
		keys = append(keys, id)
	}
	sort.Strings(keys)
	for _, id := range keys {
		out.Obligations = append(out.Obligations, obligations[id])
	}
	if out.State == facts.KnowledgeMeasured {
		out.Knowledge["behavior"] = facts.Knowledge{State: facts.KnowledgeMeasured, Essential: true}
		out.Knowledge["alias_effects"] = facts.Knowledge{State: facts.KnowledgeMeasured, Essential: true}
	}
	return out
}
func boundaryFlowGap(out *facts.BoundaryAssessment, reason string) {
	out.State = facts.KnowledgePartial
	out.Knowledge["behavior"] = facts.Knowledge{State: facts.KnowledgePartial, Reason: reason, Essential: true}
	out.Reasons = append(out.Reasons, facts.Reason{Code: reason, Dimension: "behavior", Message: reason})
}
