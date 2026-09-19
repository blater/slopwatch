package sourceestimate

import (
	"math"
	"strings"
)

func gradedEvidenceOperationResponsibilities(a *gradedEvidenceAssessment, state *gradedEvidenceRootState, calls map[string]map[string]bool, op *operation, units []unit, byKey map[string][]*operation, storageSnapshots map[string]bool) {
	body := pruneDeadFalseBranches(op.body)
	if n := gradedCachedFactories(op, units[op.file], units, byKey); n > 0 {
		state.connectedFactories[op.id] = n
	}
	if gradedIndependentJDBCCleanup(op, units[op.file], a.roots) {
		a.evidence.Responsibilities["resource"] = math.Max(a.profile.Resource, a.evidence.Responsibilities["resource"])
	}
	opReceivers := gradedEvidenceCallReceivers(op, body, calls)
	for receiver := range opReceivers {
		if gradeLocalCoordination(op, units[op.file], body, receiver) {
			state.coordinated[receiver] = true
		}
	}
	if !gradedSurfaceConstructor(op) {
		computed, observed := storageSnapshots[op.id]
		if !observed {
			computed = gradeComputedAssignment(op, units[op.file], body)
		}
		if computed {
			a.evidence.Responsibilities["invariant-transformation"] = a.profile.InvariantTransformation
		}
		if gradeConsistentWrites(op, units[op.file], body) {
			a.evidence.Responsibilities["state-consistency"] = a.profile.StateConsistency
		}
	}
	if gradeTypedAlternatives(op, units[op.file], body) && a.dispatchFor(op) == "" {
		a.evidence.Responsibilities["representation-transformation"] = a.profile.RepresentationTransformation
	}
	if op.language == "rust" && gradeAssertion(body) {
		a.evidence.Responsibilities["validation"] = math.Max(a.profile.Validation, a.evidence.Responsibilities["validation"])
	}
}
func gradedEvidenceCallReceivers(op *operation, body []token, calls map[string]map[string]bool) map[string]bool {
	result := map[string]bool{}
	for _, c := range callsIn(body) {
		if unreachableCall(body, c) {
			continue
		}
		dot := strings.LastIndexByte(c.name, '.')
		if dot < 0 {
			continue
		}
		receiver := gradeReceiver(op, c.name[:dot])
		if receiver == "this" || receiver == "self" || receiver == op.receiverName {
			continue
		}
		if calls[receiver] == nil {
			calls[receiver] = map[string]bool{}
		}
		calls[receiver][c.name[dot+1:]] = true
		result[receiver] = true
	}
	return result
}
