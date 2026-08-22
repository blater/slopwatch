package metrics

import (
	"math"
	"sort"
	"strings"

	"slopslap.dev/structural/internal/facts"
)

const shallowDefinition = "ousterhout-v2"

const depthReference = 1.0

// ModuleShallowness measures useful capability per caller-visible interface
// cost. The shared fact model currently supplies the static approximation:
// exported operations are Entries and return statements with values are
// Exits. Deliberately do not use implementation size or control-flow metrics.
func ModuleShallowness(functions []*facts.Function, types []*facts.Type) (int, map[string]any) {
	entries, exits, operationCost := 0, 0, 0.0
	for _, function := range functions {
		if !exported(function.Name) {
			continue
		}
		entries++
		operationCost++
		if returnsValue(function.Body) {
			exits++
		}
	}

	typeCost := 0.0
	publicTypes := 0
	for _, item := range types {
		if !exported(item.Name) {
			continue
		}
		publicTypes++
		typeCost++
		// The current Go fact does not distinguish public signature types from
		// private representation types, so do not count ForeignTypes here.
		// Counting them would make private implementation detail look like
		// caller-visible interface cost.
	}

	// Public state and design-decision evidence are not yet present in the
	// language-neutral contract. They remain explicit zero-valued terms rather
	// than being guessed from implementation complexity.
	exposedStateCost := 0.0
	informationHidingCost := 0.0
	interfaceCost := operationCost + typeCost + exposedStateCost + informationHidingCost
	if interfaceCost <= 0 {
		return 0, map[string]any{
			"available":          false,
			"unavailable_reason": "module has no statically identifiable public interface",
		}
	}

	functionality := float64(entries + exits)
	depth := functionality / interfaceCost
	depthPenalty := 100 * math.Max(0, 1-depth/depthReference)
	leakagePenalty := 0.0
	penalty := int(math.Round(clamp(0, 100, 0.80*depthPenalty+0.20*leakagePenalty)))
	return penalty, map[string]any{
		"available":               true,
		"functionality":           functionality,
		"entries":                 entries,
		"exits":                   exits,
		"reads":                   0,
		"writes":                  0,
		"capability_basis":        "static-approximation",
		"interface_cost":          interfaceCost,
		"operation_cost":          operationCost,
		"type_cost":               typeCost,
		"public_types":            publicTypes,
		"exposed_state_cost":      exposedStateCost,
		"information_hiding_cost": informationHidingCost,
		"exposed_representation":  0,
		"raw_depth_ratio":         depth,
		"depth_reference":         depthReference,
		"depth_penalty":           depthPenalty,
		"leakage_penalty":         leakagePenalty,
		"available_weight":        1.0,
		"total_weight":            1.0,
	}
}

// moduleShallownessWithOperations uses caller-visible signatures when the
// adapter provides them. Each operation is one interface decision; each
// parameter/result shape and exposed field contributes its normalized cost.
func moduleShallownessWithOperations(operations []*facts.PublicOperation, types []*facts.Type) (int, map[string]any) {
	return moduleShallownessWithEvidence(operations, types, nil)
}

func moduleShallownessWithEvidence(operations []*facts.PublicOperation, types []*facts.Type, exposures []*facts.RepresentationExposure) (int, map[string]any) {
	entries, exits := 0, 0
	parameterCount, resultCount := 0, 0
	operationCost, typeCost := 0.0, 0.0
	publicTypes, exposedStateCost, exposedRepresentation := 0, 0.0, 0
	for _, operation := range operations {
		operationCost++
		if len(operation.Parameters) == 0 {
			entries++
		} else {
			entries += len(operation.Parameters)
		}
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
		return 0, map[string]any{
			"available":          false,
			"unavailable_reason": "module has no statically identifiable public interface",
		}
	}

	functionality := float64(entries + exits)
	depth := functionality / interfaceCost
	depthPenalty := 100 * math.Max(0, 1-depth/depthReference)
	leakagePenalty := 100 * math.Min(1, float64(exposedRepresentation)/interfaceCost)
	penalty := int(math.Round(clamp(0, 100, 0.80*depthPenalty+0.20*leakagePenalty)))
	return penalty, map[string]any{
		"available":               true,
		"evidence_level":          2,
		"evidence_coverage":       1.0,
		"functionality":           functionality,
		"entries":                 entries,
		"exits":                   exits,
		"reads":                   0,
		"writes":                  0,
		"parameter_count":         parameterCount,
		"result_count":            resultCount,
		"capability_basis":        "cosmic-inspired-signature",
		"interface_cost":          interfaceCost,
		"operation_cost":          operationCost,
		"type_cost":               typeCost,
		"public_types":            publicTypes,
		"exposed_state_cost":      exposedStateCost,
		"information_hiding_cost": 0.0,
		"exposed_representation":  exposedRepresentation,
		"raw_depth_ratio":         depth,
		"depth_reference":         depthReference,
		"depth_penalty":           depthPenalty,
		"leakage_penalty":         leakagePenalty,
		"available_weight":        1.0,
		"total_weight":            1.0,
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

func returnsValue(statements []*facts.Statement) bool {
	for _, statement := range statements {
		if statement.Kind == facts.StmtReturn && len(statement.Expressions) > 0 {
			return true
		}
		if returnsValue(statement.Body) || returnsValue(statement.Else) {
			return true
		}
		for _, branch := range statement.Cases {
			if returnsValue(branch.Body) {
				return true
			}
		}
	}
	return false
}

func exported(name string) bool {
	name = strings.TrimSpace(name)
	return name != "" && name[0] >= 'A' && name[0] <= 'Z'
}

func clamp(minimum, maximum, value float64) float64 {
	return math.Max(minimum, math.Min(maximum, value))
}

func moduleFiles(program *facts.Program) []string {
	files := append([]string(nil), program.Files...)
	seen := map[string]bool{}
	for _, path := range files {
		seen[path] = true
	}
	for _, function := range program.Functions {
		if !seen[function.Location.Path] {
			files = append(files, function.Location.Path)
			seen[function.Location.Path] = true
		}
	}
	for _, operation := range program.PublicOperations {
		if !seen[operation.Location.Path] {
			files = append(files, operation.Location.Path)
			seen[operation.Location.Path] = true
		}
	}
	for _, item := range program.Types {
		if !seen[item.Location.Path] {
			files = append(files, item.Location.Path)
			seen[item.Location.Path] = true
		}
	}
	for _, exposure := range program.Representation {
		if !seen[exposure.Location.Path] {
			files = append(files, exposure.Location.Path)
			seen[exposure.Location.Path] = true
		}
	}
	sort.Strings(files)
	return files
}

func moduleShallownessMeasurements(program *facts.Program) []Measurement {
	output := make([]Measurement, 0, len(moduleFiles(program)))
	for _, path := range moduleFiles(program) {
		functions := make([]*facts.Function, 0)
		for _, function := range program.Functions {
			if function.Location.Path == path {
				functions = append(functions, function)
			}
		}
		types := make([]*facts.Type, 0)
		for _, item := range program.Types {
			if item.Location.Path == path {
				types = append(types, item)
			}
		}
		operations := make([]*facts.PublicOperation, 0)
		for _, operation := range program.PublicOperations {
			if operation.Location.Path == path {
				operations = append(operations, operation)
			}
		}
		value, attributes := ModuleShallowness(functions, types)
		if len(operations) > 0 {
			exposures := make([]*facts.RepresentationExposure, 0)
			for _, exposure := range program.Representation {
				if exposure.Location.Path == path {
					exposures = append(exposures, exposure)
				}
			}
			value, attributes = moduleShallownessWithEvidence(operations, types, exposures)
		}
		output = append(output, Measurement{"module_shallowness", shallowDefinition, "file", value, path, facts.Location{Path: path, Line: 1, Column: 1, EndLine: 1, EndColumn: 1}, attributes})
	}
	return output
}
