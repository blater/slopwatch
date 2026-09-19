package metrics

import "slopslap.dev/structural/internal/facts"

// DepthV4Registry is selected explicitly per analyzer request. It never mutates
// the default registry or legacy component definitions in a running process.
func DepthV4Registry() *Registry {
	items := defaultStrategies()
	for index, item := range items {
		if item.ComponentID() == "module_shallowness" {
			items[index] = strategy{component: "module_shallowness", definition: DepthV4Definition, measure: depthV4Measurements}
		}
	}
	registry, err := NewRegistry(items...)
	if err != nil {
		panic(err)
	}
	return registry
}
func depthV4Measurements(program *facts.Program) []Measurement {
	scores := MeasureDepth(program)
	if program.Depth == nil {
		return unavailableDepthMeasurements(program)
	}
	output := make([]Measurement, 0, len(scores))
	inventories := map[string]facts.BoundaryAssessment{}
	for _, boundary := range program.Depth.Boundaries {
		inventories[boundary.Identity.String()] = boundary
	}
	for _, score := range scores {
		inventory := inventories[score.Boundary.String()]
		locations := map[string]facts.Location{}
		for _, location := range inventory.SourceLocations {
			if _, ok := locations[location.Path]; !ok {
				locations[location.Path] = location
			}
		}
		for _, path := range inventory.Files {
			location := locations[path]
			location.Path = path
			var value any
			if score.Shallow != nil {
				value = *score.Shallow
			}
			output = append(output, Measurement{Component: "module_shallowness", Definition: DepthV4Definition, Scope: "module", Value: value, Subject: score.Boundary.Symbol, Location: location, Attributes: map[string]any{"depth": score, "boundary_id": score.Boundary.String(), "knowledge_state": score.State, "policy_revision": DepthV4PolicyRevision, "inventory_fingerprint": score.InventoryFingerprint}})
			if score.State == facts.KnowledgePartial || score.State == facts.KnowledgeUnavailable {
				setDepthUnavailable(program, path, "SHALLOW v4 boundary analysis is incomplete")
			}
		}
	}
	return output
}
func unavailableDepthMeasurements(program *facts.Program) []Measurement {
	output := make([]Measurement, 0, len(program.Files))
	for _, path := range program.Files {
		setDepthUnavailable(program, path, "SHALLOW v4 source facts are unavailable for this adapter")
		output = append(output, Measurement{Component: "module_shallowness", Definition: DepthV4Definition, Scope: "module", Location: facts.Location{Path: path}, Attributes: map[string]any{"knowledge_state": facts.KnowledgeUnavailable, "reason": "missing_depth_facts"}})
	}
	return output
}
func setDepthUnavailable(program *facts.Program, path, reason string) {
	if program.Unavailable == nil {
		program.Unavailable = map[string]map[string]string{}
	}
	if program.Unavailable[path] == nil {
		program.Unavailable[path] = map[string]string{}
	}
	program.Unavailable[path]["module_shallowness"] = reason
}
