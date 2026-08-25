// Package scoring owns the dashboard scoring policy and the metric
// aggregations shared by presentation and fix verification.
package scoring

// Component describes one user-configurable contribution to SCORE.
type Component struct {
	ID            string
	Label         string
	Category      string
	Parent        string
	Axis          string
	DefaultWeight float64
	DefaultOn     bool
}

var components = []Component{
	{"cognitive_complexity", "Cognitive complexity", "Structural", "COG", "structural_core", 10, true},
	{"cyclomatic_method_complexity", "Routine", "Structural", "CYCLO", "structural_core", 5, true},
	{"cyclomatic_class_complexity", "Type", "Structural", "CYCLO", "structural_language", 5, true},
	{"npath_complexity", "NPath complexity", "Structural", "NPATH", "structural_core", 8, true},
	{"deeply_nested_if", "Deep nesting", "Structural", "Nesting", "structural_core", 6, false},
	{"module_shallowness", "Module shallowness", "Structural", "SHALLOW", "structural_core", 5, true},
	{"coupling_between_objects", "Type coupling", "Structural", "Coupling", "structural_language", 10, true},
	{"god_class", "Responsibility concentration", "Structural", "GOD", "structural_language", 1, true},
	{"ambiguous_boolean_expression", "Ambiguous boolean", "Type safety", "", "typescript_type_safety", 4, false},
	{"explicit_any", "Explicit any", "Type safety", "", "typescript_type_safety", 3, false},
	{"non_exhaustive_union", "Non-exhaustive union", "Type safety", "", "typescript_type_safety", 8, false},
	{"unsafe_type_assertion", "Unsafe assertion", "Type safety", "", "typescript_type_safety", 5, false},
	{"unsafe_type_boundary", "Unsafe boundary", "Type safety", "", "typescript_type_safety", 10, false},
	{"unsafe_type_propagation", "Unsafe propagation", "Type safety", "", "typescript_type_safety", 4, false},
	{"unsafe_type_use", "Unsafe type use", "Type safety", "", "typescript_type_safety", 4, false},
}

var componentsByID = indexComponents(components)

func indexComponents(values []Component) map[string]Component {
	result := make(map[string]Component, len(values))
	for _, value := range values {
		result[value.ID] = value
	}
	return result
}

// Components returns the configurable components in their stable display
// order. The returned slice does not alias package state.
func Components() []Component { return append([]Component(nil), components...) }

// ComponentByID returns one known configurable component.
func ComponentByID(id string) (Component, bool) {
	component, ok := componentsByID[id]
	return component, ok
}

// DefaultWeight preserves the historical projection behavior for unknown
// components. Unknown components are disabled, so this fallback is not
// normally observable in SCORE.
func DefaultWeight(id string) float64 {
	if component, ok := ComponentByID(id); ok {
		return component.DefaultWeight
	}
	return 1
}

// ComponentAxis returns the score axis for a component.
func ComponentAxis(id string) string {
	if component, ok := ComponentByID(id); ok {
		return component.Axis
	}
	return "unknown"
}

// DefaultWeights returns an independent map of default component weights.
func DefaultWeights() map[string]float64 {
	result := make(map[string]float64, len(components))
	for _, component := range components {
		result[component.ID] = component.DefaultWeight
	}
	return result
}

// DefaultEnabled returns an independent map of default component enablement.
func DefaultEnabled() map[string]bool {
	result := make(map[string]bool, len(components))
	for _, component := range components {
		result[component.ID] = component.DefaultOn
	}
	return result
}
