// Package metrics evaluates PMD-compatible strategies over normalized facts.
package metrics

import (
	"fmt"
	"sort"

	"slopslap.dev/structural/internal/facts"
)

// Strategy evaluates one versioned component over normalized facts.
type Strategy interface {
	ComponentID() string
	DefinitionVersion() string
	Measure(*facts.Program) []Measurement
}

type strategy struct {
	component  string
	definition string
	measure    func(*facts.Program) []Measurement
}

func (item strategy) ComponentID() string                          { return item.component }
func (item strategy) DefinitionVersion() string                    { return item.definition }
func (item strategy) Measure(program *facts.Program) []Measurement { return item.measure(program) }

// Registry owns versioned metric strategies independently of language adapters.
type Registry struct {
	byComponent map[string]Strategy
}

// NewRegistry creates a strategy registry and rejects component collisions.
func NewRegistry(items ...Strategy) (*Registry, error) {
	registry := &Registry{byComponent: make(map[string]Strategy, len(items))}
	for _, item := range items {
		component := item.ComponentID()
		if component == "" || item.DefinitionVersion() == "" {
			return nil, fmt.Errorf("metric strategy identity is incomplete")
		}
		if _, exists := registry.byComponent[component]; exists {
			return nil, fmt.Errorf("duplicate metric strategy for %s", component)
		}
		registry.byComponent[component] = item
	}
	return registry, nil
}

func functionValueStrategy(component, definition string, value func(*facts.Function) any) Strategy {
	return strategy{component, definition, func(program *facts.Program) []Measurement {
		output := make([]Measurement, 0, len(program.Functions))
		for _, function := range program.Functions {
			output = append(output, Measurement{component, definition, "function", value(function), function.Name, function.Location, map[string]any{}})
		}
		return output
	}}
}

func defaultStrategies() []Strategy {
	return []Strategy{
		functionValueStrategy("cognitive_complexity", "pmd-sonar-v1", func(function *facts.Function) any { return Cognitive(function) }),
		functionValueStrategy("cyclomatic_method_complexity", "pmd-v1", func(function *facts.Function) any { return Cyclomatic(function) }),
		functionValueStrategy("npath_complexity", "pmd-v1", func(function *facts.Function) any { return NPath(function) }),
		strategy{"deeply_nested_if", "pmd-v1", func(program *facts.Program) []Measurement {
			output := make([]Measurement, 0)
			for _, function := range program.Functions {
				locations := make([]facts.Location, 0)
				deepIf(function.Body, 0, &locations)
				for _, location := range locations {
					output = append(output, Measurement{"deeply_nested_if", "pmd-v1", "expression", 1, "if", location, map[string]any{"problem_depth": 3}})
				}
			}
			return output
		}},
		strategy{"cyclomatic_class_complexity", "pmd-v1", func(program *facts.Program) []Measurement {
			output := make([]Measurement, 0, len(program.Types))
			for _, item := range program.Types {
				output = append(output, Measurement{"cyclomatic_class_complexity", "pmd-v1", "type", typeWMC(item), item.Name, item.Location, map[string]any{"kind": item.Kind}})
			}
			return output
		}},
		strategy{"coupling_between_objects", "pmd-v1", func(program *facts.Program) []Measurement {
			output := make([]Measurement, 0, len(program.Types))
			for _, item := range program.Types {
				if available, _ := program.Availability(item.Location.Path, "coupling_between_objects"); !available {
					continue
				}
				output = append(output, Measurement{"coupling_between_objects", "pmd-v1", "type", len(unique(item.ForeignTypes)), item.Name, item.Location, map[string]any{"kind": item.Kind}})
			}
			return output
		}},
		strategy{"god_class", "pmd-v1", func(program *facts.Program) []Measurement {
			output := make([]Measurement, 0, len(program.Types))
			for _, item := range program.Types {
				if available, _ := program.Availability(item.Location.Path, "god_class"); !available {
					continue
				}
				if item.Kind == "interface" {
					continue
				}
				wmc := typeWMC(item)
				output = append(output, Measurement{"god_class", "pmd-v1", "type", wmc, item.Name, item.Location, map[string]any{
					"kind": item.Kind, "wmc": wmc, "atfd": len(unique(item.ForeignFields)), "tcc": tcc(item),
				}})
			}
			return output
		}},
	}
}

// DefaultRegistry returns the complete PMD-compatible strategy set.
func DefaultRegistry() *Registry {
	registry, err := NewRegistry(defaultStrategies()...)
	if err != nil {
		panic(err)
	}
	return registry
}

// Definitions returns the component-to-definition contract advertised by the host.
func (registry *Registry) Definitions() map[string]string {
	output := make(map[string]string, len(registry.byComponent))
	for component, item := range registry.byComponent {
		output[component] = item.DefinitionVersion()
	}
	return output
}

// Analyze evaluates requested registered strategies in deterministic order.
func (registry *Registry) Analyze(program *facts.Program, requested map[string]bool) ([]Measurement, error) {
	components := make([]string, 0, len(requested))
	for component, enabled := range requested {
		if enabled {
			components = append(components, component)
		}
	}
	sort.Strings(components)
	output := make([]Measurement, 0)
	for _, component := range components {
		item, exists := registry.byComponent[component]
		if !exists {
			return nil, fmt.Errorf("no metric strategy for %s", component)
		}
		output = append(output, item.Measure(program)...)
	}
	sort.Slice(output, func(left, right int) bool {
		a, b := output[left], output[right]
		if a.Location.Path != b.Location.Path {
			return a.Location.Path < b.Location.Path
		}
		if a.Location.Line != b.Location.Line {
			return a.Location.Line < b.Location.Line
		}
		if a.Location.Column != b.Location.Column {
			return a.Location.Column < b.Location.Column
		}
		return a.Component < b.Component
	})
	return output, nil
}

// Analyze evaluates the default strategy registry.
func Analyze(program *facts.Program, requested map[string]bool) []Measurement {
	output, err := DefaultRegistry().Analyze(program, requested)
	if err != nil {
		panic(err)
	}
	return output
}
