package sourceestimate

import (
	"math"
	"strings"
)

type gradedEvidenceAssessment struct {
	unit        unit
	roots       []*operation
	profile     CalibrationProfile
	evidence    *GradedEvidence
	rootOwners  map[string]bool
	protocol    map[string]map[string]bool
	transparent map[string]bool
	dispatches  map[string]string
}

func (a *gradedEvidenceAssessment) dispatchFor(op *operation) string {
	if key, assessed := a.dispatches[op.id]; assessed {
		return key
	}
	key, _ := gradedCapabilityDispatch(op, normalizedPrunedBody(op))
	a.dispatches[op.id] = key
	return key
}

// Follow only local implementation helpers here. The primary evidence traversal
// already follows owned delegates; this pass reconstructs local coordination
// across helper extraction without awarding an extra duty for the helper call.
func gradedEvidenceResponsibilities(a *gradedEvidenceAssessment, evidence []evidenceItem, units []unit, roots []*operation) {
	seen := map[string]bool{}
	for _, item := range evidence {
		key := item.key
		if item.category == "validation" && item.origin != nil {
			if dispatch := a.dispatchFor(item.origin); dispatch != "" {
				key = "validation|capability:" + dispatch
			}
		}
		if item.category == "storage_snapshot" || strings.HasPrefix(item.category, "unknown_") || seen[key] {
			continue
		}
		if item.origin != nil && !a.rootOwners[item.origin.owner] && (a.transparent[item.origin.id] || len(a.protocol[itoa(item.origin.file)+"#"+item.origin.owner]) > 0 && protocolCallerDuty(item, units[item.origin.file], a.protocol[itoa(item.origin.file)+"#"+item.origin.owner])) {
			continue
		}
		seen[key] = true
		a.evidence.Responsibilities[item.category] += a.profile.categoryWeight(item.category)
	}
	if resource, relocation := gradedRustResourceEffects(a.unit, roots); resource || relocation {
		if resource {
			a.evidence.Responsibilities["resource"] = math.Max(a.profile.Resource, a.evidence.Responsibilities["resource"])
		}
		if relocation {
			a.evidence.Responsibilities["representation-transformation"] = math.Max(a.profile.RepresentationTransformation, a.evidence.Responsibilities["representation-transformation"])
		}
	}
}

type gradedEvidenceRootState struct {
	coordinated        map[string]bool
	connectedFactories map[string]int
	phaseCommands      map[string]map[string]bool
	callerPhases       map[string]map[string]bool
	phaseMethods       map[string]map[string]bool
}

func newGradedEvidenceRootState() *gradedEvidenceRootState {
	return &gradedEvidenceRootState{coordinated: map[string]bool{}, connectedFactories: map[string]int{}, phaseCommands: map[string]map[string]bool{}, callerPhases: map[string]map[string]bool{}, phaseMethods: map[string]map[string]bool{}}
}

func gradedEvidenceRootResponsibilities(a *gradedEvidenceAssessment, state *gradedEvidenceRootState, root *operation, units []unit, byKey map[string][]*operation, storageSnapshots map[string]bool) {
	ops := gradeOwnedOperations(root, units, byKey)
	calls := map[string]map[string]bool{}
	for _, op := range ops {
		gradedEvidenceOperationResponsibilities(a, state, calls, op, units, byKey, storageSnapshots)
	}
	for receiver, methods := range calls {
		if !gradeOwnedReceiver(root, units[root.file], receiver) {
			continue
		}
		if state.phaseCommands[receiver] == nil {
			state.phaseCommands[receiver] = map[string]bool{}
		}
		if gradeCommand(root) {
			state.phaseCommands[receiver][root.id] = true
		}
		if state.callerPhases[receiver] == nil {
			state.callerPhases[receiver] = map[string]bool{}
			state.phaseMethods[receiver] = map[string]bool{}
		}
		state.callerPhases[receiver][root.id] = true
		for name := range methods {
			state.phaseMethods[receiver][name] = true
		}
	}
	if gradeGuaranteedCleanup(ops, units, byKey) {
		a.evidence.Responsibilities["resource"] = math.Max(a.profile.Cleanup, a.evidence.Responsibilities["resource"])
	}
}
