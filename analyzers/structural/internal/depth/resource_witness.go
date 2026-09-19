package depth

import (
	"crypto/sha256"
	"fmt"

	"slopslap.dev/structural/internal/facts"
)

// resourceLifecycleProof is deliberately conservative. A witness crosses a
// helper only after the callee's complete CFG has accepted its effects.
type resourceLifecycleProof struct {
	obligations       []facts.Obligation
	witnesses         []ResourceLifecycleWitness
	reasons           []string
	allEffectsProven  bool
	resourceEventSeen bool
}

const maxResourceWitnesses = 32

func proveResourceLifecycles(summaries *functionSummaries, id string, evaluation FlowEvaluation, boundary facts.BoundaryIdentity) resourceLifecycleProof {
	proof := proveResourceLifecycleWitnesses(summaries, id, evaluation)
	return mintResourceObligations(summaries, proof, evaluation, boundary)
}

func mintResourceObligations(summaries *functionSummaries, proof resourceLifecycleProof, evaluation FlowEvaluation, boundary facts.BoundaryIdentity) resourceLifecycleProof {
	seen := map[string]bool{}
	for _, witness := range proof.witnesses {
		if !summaries.budget.charge(1 + len(witness.Evidence)) {
			proof.allEffectsProven = false
			proof.reasons = append(proof.reasons, "work_limit")
			return proof
		}
		key := resourceWitnessKey(witness)
		if seen[key] {
			continue
		}
		seen[key] = true
		if witness.Guard != summaries.arena.Constant("bool", "true") {
			proof.allEffectsProven = false
			proof.reasons = append(proof.reasons, "resource_acquisition_guard_partial")
			continue
		}
		obligation, ok := resourceObligation(witness, evaluation, boundary)
		if !ok {
			proof.allEffectsProven = false
			proof.reasons = append(proof.reasons, "resource_returned")
			continue
		}
		proof.obligations = append(proof.obligations, obligation)
	}
	return proof
}

// proveResourceLifecycleWitnesses validates effects without binding them to a
// boundary identity. This is the only representation allowed to cross an
// exact scalar helper call.
func proveResourceLifecycleWitnesses(summaries *functionSummaries, id string, evaluation FlowEvaluation) resourceLifecycleProof {
	proof, seen, carriedProven := carriedResourceWitnesses(summaries, evaluation)
	if len(evaluation.Effects) == 0 || evaluation.ResourceEffectsProven {
		proof.allEffectsProven = carriedProven
		return proof
	}
	function, ok := summaries.artifact.Function(id)
	if !ok || function == nil {
		proof.allEffectsProven = false
		proof.reasons = append(proof.reasons, "resource_effect_function_unknown")
		return proof
	}
	collected := collectResourceEvents(summaries, function, evaluation)
	proof.allEffectsProven = carriedProven && collected.allEffectsProven
	proof.resourceEventSeen = collected.resourceEventSeen
	proof.reasons = append(proof.reasons, collected.reasons...)
	local, reasons, proven := proveResourceRoots(summaries, function, evaluation, collected)
	proof = appendResourceWitnesses(summaries, proof, seen, local)
	proof.reasons = append(proof.reasons, reasons...)
	proof.allEffectsProven = proof.allEffectsProven && proven && proof.resourceEventSeen
	return proof
}

func carriedResourceWitnesses(summaries *functionSummaries, evaluation FlowEvaluation) (resourceLifecycleProof, map[string]bool, bool) {
	proof := resourceLifecycleProof{allEffectsProven: true}
	seen := map[string]bool{}
	proven := true
	for _, witness := range evaluation.ResourceWitnesses {
		if !validResourceWitness(witness) {
			proven = false
			proof.reasons = append(proof.reasons, "invalid_resource_witness")
			continue
		}
		if !summaries.budget.charge(1 + len(witness.Evidence)) {
			proven = false
			proof.reasons = append(proof.reasons, "work_limit")
			return proof, seen, proven
		}
		key := resourceWitnessKey(witness)
		if seen[key] {
			continue
		}
		if len(seen) >= maxResourceWitnesses {
			proven = false
			proof.reasons = append(proof.reasons, "resource_witness_limit")
			return proof, seen, proven
		}
		seen[key] = true
		witness.Evidence = append([]string(nil), witness.Evidence...)
		proof.witnesses = append(proof.witnesses, witness)
	}
	return proof, seen, proven
}

func validResourceWitness(witness ResourceLifecycleWitness) bool {
	return witness.Governed != "" && witness.Rule != "" && witness.Guard != ""
}

func appendResourceWitnesses(summaries *functionSummaries, proof resourceLifecycleProof, seen map[string]bool, witnesses []ResourceLifecycleWitness) resourceLifecycleProof {
	for _, witness := range witnesses {
		if !summaries.budget.charge(1 + len(witness.Evidence)) {
			proof.allEffectsProven = false
			proof.reasons = append(proof.reasons, "work_limit")
			return proof
		}
		key := resourceWitnessKey(witness)
		if seen[key] {
			continue
		}
		if len(seen) >= maxResourceWitnesses {
			proof.allEffectsProven = false
			proof.reasons = append(proof.reasons, "resource_witness_limit")
			return proof
		}
		seen[key] = true
		proof.witnesses = append(proof.witnesses, witness)
	}
	return proof
}

func resourceWitnessKey(witness ResourceLifecycleWitness) string {
	return witness.Governed + "\x00" + witness.Rule + "\x00" + string(witness.Guard)
}

func resourceObligation(witness ResourceLifecycleWitness, evaluation FlowEvaluation, boundary facts.BoundaryIdentity) (facts.Obligation, bool) {
	if !resourceReturnedKey(witness.Governed, evaluation) {
		return facts.Obligation{}, false
	}
	obligationID := fmt.Sprintf("R:%x", sha256.Sum256([]byte(boundary.String()+":"+witness.Governed+":"+witness.Rule)))
	return facts.Obligation{ID: obligationID, Category: facts.ObligationResource, Governed: witness.Governed, Rule: witness.Rule, Evidence: append([]string(nil), witness.Evidence...)}, true
}
