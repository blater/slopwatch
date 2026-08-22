package metrics

import (
	"math"
	"sort"
	"strings"

	"slopslap.dev/structural/internal/facts"
)

const shallowDefinition = "ousterhout-v3"

// Shape priors for the fast release path. They are blended with classifier
// confidence and surface size below, so ambiguous or tiny surfaces stay near
// the historical neutral reference.
const (
	referencePolicy       = "role-shape-v2"
	defaultDepthReference = 1.0
)

func depthReferenceForRole(role string, confidence float64, operationCount, publicTypeCount int) (float64, string) {
	prior := defaultDepthReference
	switch role {
	case roleProtocol:
		prior = 0.72
	case roleCommand:
		prior = 1.12
	case roleQuery:
		prior = 1.05
	case roleData:
		prior = 0.85
	}
	confidence = math.Max(0, math.Min(1, confidence))
	surfaceSize := math.Min(1, float64(operationCount+publicTypeCount)/4)
	depthReference := 1 + (prior-1)*confidence*surfaceSize
	return depthReference, referencePolicy
}

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
	publicTypes := 0
	for _, item := range types {
		if exported(item.Name) {
			publicTypes++
		}
	}
	typeCost := float64(publicTypes)
	interfaceCost := operationCost + typeCost
	if interfaceCost <= 0 {
		return 0, map[string]any{"available": false, "unavailable_reason": "module has no statically identifiable public interface"}
	}
	functionality := float64(entries + exits)
	depth := functionality / interfaceCost
	depthPenalty := 100 * math.Max(0, 1-depth/defaultDepthReference)
	penalty := int(math.Round(clamp(0, 100, 0.80*depthPenalty)))
	return penalty, map[string]any{
		"available": true, "functionality": functionality, "entries": entries, "exits": exits,
		"reads": 0, "writes": 0, "capability_basis": "static-approximation", "interface_cost": interfaceCost,
		"operation_cost": operationCost, "type_cost": typeCost, "public_types": publicTypes,
		"exposed_state_cost": 0, "information_hiding_cost": 0, "exposed_representation": 0,
		"raw_depth_ratio": depth, "depth_reference": defaultDepthReference, "depth_penalty": depthPenalty,
		"leakage_penalty": 0, "available_weight": 1, "total_weight": 1,
	}
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
	add := func(path string) {
		if !seen[path] {
			files = append(files, path)
			seen[path] = true
		}
	}
	for _, item := range program.Functions {
		add(item.Location.Path)
	}
	for _, item := range program.PublicOperations {
		add(item.Location.Path)
	}
	for _, item := range program.Types {
		add(item.Location.Path)
	}
	for _, item := range program.Representation {
		add(item.Location.Path)
	}
	sort.Strings(files)
	return files
}

func moduleShallownessMeasurements(program *facts.Program) []Measurement {
	output := make([]Measurement, 0, len(moduleFiles(program)))
	for _, path := range moduleFiles(program) {
		types := filterTypes(program.Types, path)
		operations := filterOperations(program.PublicOperations, path)
		value, attributes := 0, map[string]any{"available": false, "unavailable_reason": "no statically identifiable public operations"}
		if len(operations) > 0 {
			value, attributes = moduleShallownessWithEvidence(operations, types, filterExposures(program.Representation, path))
		}
		output = append(output, Measurement{"module_shallowness", shallowDefinition, "file", value, path, facts.Location{Path: path, Line: 1, Column: 1, EndLine: 1, EndColumn: 1}, attributes})
	}
	return output
}

func filterTypes(items []*facts.Type, path string) []*facts.Type {
	output := make([]*facts.Type, 0)
	for _, item := range items {
		if item.Location.Path == path {
			output = append(output, item)
		}
	}
	return output
}
func filterOperations(items []*facts.PublicOperation, path string) []*facts.PublicOperation {
	output := make([]*facts.PublicOperation, 0)
	for _, item := range items {
		if item.Location.Path == path {
			output = append(output, item)
		}
	}
	return output
}
func filterExposures(items []*facts.RepresentationExposure, path string) []*facts.RepresentationExposure {
	output := make([]*facts.RepresentationExposure, 0)
	for _, item := range items {
		if item.Location.Path == path {
			output = append(output, item)
		}
	}
	return output
}
