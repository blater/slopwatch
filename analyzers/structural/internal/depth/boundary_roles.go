package depth

import "slopslap.dev/structural/internal/facts"

const SupportingContractRule = "supporting-contract-v1"
const PassiveCreationRule = "passive-creation-v1"

// ApplyBoundaryRoles recognizes proven passive creation and internal supporting
// contracts. Flow remains in the artifact for consumers to assess.
func ApplyBoundaryRoles(boundary facts.BoundaryAssessment) facts.BoundaryAssessment {
	if boundary.State == facts.KnowledgeNotApplicable {
		return boundary
	}
	if passiveCreationApplicable(boundary) {
		boundary.State = facts.KnowledgeNotApplicable
		creation := boundary.Creation
		boundary.Evidence = append([]facts.Evidence(nil), boundary.Evidence...)
		boundary.Evidence = append(boundary.Evidence, facts.Evidence{
			ID: PassiveCreationRule + ":" + creation.ID, Kind: PassiveCreationRule,
			Status: "not_applicable", Details: passiveCreationDetails(creation),
			Provenance: passiveCreationProvenance(boundary, creation),
		})
		return boundary
	}
	role := boundary.SupportingContract
	if !completeSupportingContract(role) || !supportingImplementationRoutes(boundary.RouteFamilies, role) {
		return boundary
	}
	uses := map[string][]facts.ContractUse{}
	for _, use := range role.Bindings {
		if completeContractUse(use) {
			uses[use.Member] = append(uses[use.Member], use)
		}
	}
	for _, member := range role.Members {
		if member == "" || len(uses[member]) == 0 {
			return boundary
		}
	}
	boundary.State = facts.KnowledgeNotApplicable
	boundary.Evidence = append([]facts.Evidence(nil), boundary.Evidence...)
	boundary.Dependencies = append([]string(nil), boundary.Dependencies...)
	for _, member := range role.Members {
		for _, use := range uses[member] {
			addSupportingEvidence(&boundary, use)
		}
	}
	return boundary
}

// A proven internal contract member remains inventoried, but is not an
// independently exposed service. Any route outside that proof prevents N/A.
func supportingImplementationRoutes(families []facts.RouteFamily, role *facts.SupportingContract) bool {
	members := make(map[string]bool, len(role.Members))
	for _, member := range role.Members {
		members[member] = true
	}
	for _, family := range families {
		if len(family.Routes) == 0 {
			return false
		}
		for _, route := range family.Routes {
			if !members[route.ID] || route.TargetFunctionID != route.ID {
				return false
			}
		}
	}
	return true
}

func completeEssentialKnowledge(knowledge map[string]facts.Knowledge) bool {
	for _, dimension := range []string{"inventory", "burden", "behavior", "alias_effects"} {
		item, ok := knowledge[dimension]
		if !ok || item.State != facts.KnowledgeMeasured {
			return false
		}
	}
	for _, item := range knowledge {
		if item.Essential && item.State != facts.KnowledgeMeasured {
			return false
		}
	}
	return true
}

func completeSupportingContract(role *facts.SupportingContract) bool {
	return role != nil && role.Inventory == facts.KnowledgeMeasured &&
		role.Exposure == facts.KnowledgeMeasured && len(role.Members) > 0 && len(role.ExternalRoutes) == 0
}

func completeContractUse(use facts.ContractUse) bool {
	return use.Member != "" && use.Contract != "" && use.ContractMember != "" &&
		use.Consumer != "" && use.Slot != "" && use.Use != ""
}

func addSupportingEvidence(boundary *facts.BoundaryAssessment, use facts.ContractUse) {
	boundary.Evidence = append(boundary.Evidence, facts.Evidence{
		ID:   SupportingContractRule + ":" + use.Member + ":" + use.Use,
		Kind: SupportingContractRule, Status: "not_applicable",
		Details: map[string]any{
			"member": use.Member, "contract_member": use.ContractMember,
			"consumer": use.Consumer, "slot": use.Slot,
		},
		Provenance: append([]facts.Provenance(nil), use.Provenance...),
	})
	boundary.Dependencies = appendUniqueStrings(boundary.Dependencies, use.Member, use.Contract, use.ContractMember, use.Consumer, use.Use)
}
