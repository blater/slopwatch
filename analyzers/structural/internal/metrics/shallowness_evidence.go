package metrics

import (
	"math"
	"slopslap.dev/structural/internal/facts"
)

func moduleShallownessWithOperations(operations []*facts.PublicOperation, types []*facts.Type) (int, map[string]any) {
	return moduleShallownessWithEvidence(operations, types, nil)
}

func moduleShallownessWithEvidence(operations []*facts.PublicOperation, types []*facts.Type, exposures []*facts.RepresentationExposure) (int, map[string]any) {
	entries, exits, parameterCount, resultCount := 0, 0, 0, 0
	operationCost, typeCost := 0.0, 0.0
	publicTypes, exposedStateCost, exposedRepresentation := 0, 0.0, 0
	for _, operation := range operations {
		operationCost++
		entries += max(1, len(operation.Parameters))
		parameterCount += len(operation.Parameters)
		resultCount += len(operation.Results)
		exits += len(operation.Results)
		if len(operation.Results) == 0 && operation.EmitsOutput {
			exits++
		}
		for _, shape := range operation.Parameters {
			typeCost += shapeComplexity(shape)
			exposedRepresentation += shapeExposedMembers(shape)
		}
		for _, shape := range operation.Results {
			typeCost += shapeComplexity(shape)
			exposedRepresentation += shapeExposedMembers(shape)
		}
	}
	for _, item := range types {
		if !exported(item.Name) {
			continue
		}
		publicTypes++
		for _, field := range item.Fields {
			if !field.Public {
				continue
			}
			exposedStateCost++
			if field.Mutable {
				exposedRepresentation++
			}
		}
	}
	if len(exposures) > 0 {
		exposedRepresentation = len(exposures)
	}
	typeCost += float64(publicTypes)
	interfaceCost := operationCost + typeCost + exposedStateCost
	if interfaceCost <= 0 {
		return 0, map[string]any{"available": false, "unavailable_reason": "module has no statically identifiable public interface"}
	}
	functionality := float64(entries + exits)
	depth := functionality / interfaceCost
	depthPenalty := 100 * math.Max(0, 1-depth/depthReference)
	leakagePenalty := 100 * math.Min(1, float64(exposedRepresentation)/interfaceCost)
	penalty := int(math.Round(clamp(0, 100, 0.80*depthPenalty+0.20*leakagePenalty)))
	return penalty, map[string]any{
		"available": true, "evidence_level": 2, "evidence_coverage": 1.0,
		"functionality": functionality, "entries": entries, "exits": exits, "reads": 0, "writes": 0,
		"parameter_count": parameterCount, "result_count": resultCount, "capability_basis": "cosmic-inspired-signature",
		"interface_cost": interfaceCost, "operation_cost": operationCost, "type_cost": typeCost, "public_types": publicTypes,
		"exposed_state_cost": exposedStateCost, "information_hiding_cost": 0.0, "exposed_representation": exposedRepresentation,
		"raw_depth_ratio": depth, "depth_reference": depthReference, "depth_penalty": depthPenalty,
		"leakage_penalty": leakagePenalty, "available_weight": 1.0, "total_weight": 1.0,
	}
}

func shapeComplexity(shape *facts.TypeShape) float64 {
	if shape == nil || shape.Complexity < 1 {
		return 1
	}
	return float64(shape.Complexity)
}

func shapeExposedMembers(shape *facts.TypeShape) int {
	if shape == nil {
		return 0
	}
	count := len(shape.ExposedMembers)
	for _, child := range shape.Children {
		count += shapeExposedMembers(child)
	}
	return count
}
