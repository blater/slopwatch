package metrics

import "slopslap.dev/structural/internal/facts"

const (
	roleProtocol = "protocol"
	roleCommand  = "command"
	roleQuery    = "query"
	roleData     = "data"
	roleGeneral  = "general"
	roleUnknown  = "unknown"
)

// interfaceRole classifies caller-visible shape only. It is deliberately
// conservative: the result describes evidence and does not judge quality.
func interfaceRole(operations []*facts.PublicOperation, types []*facts.Type) (string, float64, string) {
	_, interfaceTypes, publicFields := publicSurface(types)
	if len(operations) == 0 {
		if publicFields > 0 {
			return roleData, 1, "public-state-surface"
		}
		return roleUnknown, 0, "insufficient-or-contradictory-evidence"
	}
	if interfaceTypes > 0 {
		return roleProtocol, 1, "public-contract-operations"
	}
	results, mutations := operationShape(operations)
	if results == 0 {
		return roleCommand, 0.9, "operation-result-shape"
	}
	if results*2 >= len(operations) && mutations <= max(1, len(operations)/3) {
		return roleQuery, 0.85, "operation-result-shape"
	}
	return roleGeneral, 0.6, "mixed-operation-shape"
}

func publicSurface(types []*facts.Type) (publicTypes, interfaceTypes, publicFields int) {
	for _, item := range types {
		if !exported(item.Name) {
			continue
		}
		publicTypes++
		if item.Kind == "interface" {
			interfaceTypes++
		}
		for _, field := range item.Fields {
			if field.Public {
				publicFields++
			}
		}
	}
	return publicTypes, interfaceTypes, publicFields
}

func operationShape(operations []*facts.PublicOperation) (results, mutations int) {
	for _, operation := range operations {
		if len(operation.Results) > 0 || operation.EmitsOutput {
			if operation.ObservableMutation {
				mutations++
			}
			results++
		}
	}
	return results, mutations
}
