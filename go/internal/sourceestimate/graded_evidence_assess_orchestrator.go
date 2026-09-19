package sourceestimate

import (
	"math"
	"sort"
	"strings"
)

func assessGradedEvidence(u unit, roots []*operation, units []unit, byKey map[string][]*operation, evidence []evidenceItem, result Result) *GradedEvidence {
	a := newGradedEvidenceAssessment(u, roots, units, byKey, evidence)
	excludedLimits, ownedUnknown, storageSnapshots := gradedEvidenceLimits(evidence, a.rootOwners, a.protocol, result.Limitations)
	if !ownedUnknown {
		excludedLimits["unsupported_outcome_range_0_2"] = true
	}
	gradedEvidenceResponsibilities(a, evidence, units, roots)
	state := newGradedEvidenceRootState()
	for _, root := range roots {
		gradedEvidenceRootResponsibilities(a, state, root, units, byKey, storageSnapshots)
	}
	p, g := a.profile, a.evidence
	for _, count := range state.connectedFactories {
		g.Responsibilities["cache-consistency"] += p.StateConsistency * float64(count)
	}
	if len(state.coordinated) > 0 {
		g.Responsibilities["coordination"] = math.Max(p.Coordination*float64(len(state.coordinated)), g.Responsibilities["coordination"])
	}
	if g.Surface.RepresentationUnits > 0 && g.Responsibilities["validation"] > p.ExposedValidationCap {
		g.Responsibilities["validation"] = p.ExposedValidationCap
	}
	moduleState, moduleComputed := typeScriptModuleScalarEffects(u, roots, units, byKey)
	if moduleState {
		g.Responsibilities["state"] = math.Max(p.State, g.Responsibilities["state"])
	}
	if moduleComputed {
		g.Responsibilities["invariant-transformation"] = math.Max(p.InvariantTransformation, g.Responsibilities["invariant-transformation"])
	}
	categories := make([]string, 0, len(g.Responsibilities))
	for category := range g.Responsibilities {
		categories = append(categories, category)
	}
	sort.Strings(categories)
	for _, category := range categories {
		g.Hidden += g.Responsibilities[category]
	}
	g.EstimatedHidden = gradeUnresolvedReturnedDuty(roots, units, byKey, g.Responsibilities, p)
	if gradedPossibleOwnedProjection(roots, u) {
		g.EstimatedHidden = math.Max(g.EstimatedHidden, math.Max(0, p.TransformationEnvelope-g.Responsibilities["invariant-transformation"]-g.Responsibilities["transform"]-g.Responsibilities["representation-transformation"]))
		g.MaterialLimitations = append(g.MaterialLimitations, "unresolved_owned_element_projection")
	}
	if gradedUnresolvedCleanup(roots, units, byKey) {
		g.EstimatedHidden += math.Max(0, p.Resource-g.Responsibilities["resource"])
		g.MaterialLimitations = append(g.MaterialLimitations, "unresolved_connected_cleanup")
	}
	for receiver, phases := range state.callerPhases {
		if len(state.phaseCommands[receiver]) > 0 && len(phases) > 1 && len(state.phaseMethods[receiver]) > 1 {
			transitions := len(state.phaseCommands[receiver])
			if transitions == len(phases) {
				transitions--
			}
			g.Surface.RepresentationUnits += p.Sequencing * float64(transitions)
			g.Surface.Evidence = append(g.Surface.Evidence, "caller-sequencing:"+receiver)
		}
	}
	g.ResidualBurden = math.Max(0, g.Surface.OperationUnits+g.Surface.InputUnits-p.ResidualReference) + g.Surface.RepresentationUnits
	g.SupportedBurden = g.ResidualBurden
	for _, reason := range result.Limitations {
		if excludedLimits[reason] || strings.HasPrefix(reason, "unsupported_execution_alternatives:") {
			continue
		}
		g.MaterialLimitations = append(g.MaterialLimitations, reason)
	}

	if g.Value() == 0 {
		g.ZeroReason = "lowest_range_supported"
		if len(roots) == 0 && !result.NoAbstractionProven {
			g.ZeroReason = "conservative_uncertainty"
		}
	}
	sort.Strings(g.Surface.Evidence)
	sort.Strings(g.MaterialLimitations)
	return g
}
