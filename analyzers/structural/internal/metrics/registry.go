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
	cached     func(*facts.Program, *analysisCache) []Measurement
}

func (item strategy) ComponentID() string                          { return item.component }
func (item strategy) DefinitionVersion() string                    { return item.definition }
func (item strategy) Measure(program *facts.Program) []Measurement { return item.measure(program) }

func (item strategy) measureWithCache(program *facts.Program, cache *analysisCache) []Measurement {
	if item.cached != nil {
		return item.cached(program, cache)
	}
	return item.measure(program)
}

type analysisCache struct {
	cyclomatic map[*facts.Function]int
}

type measurementCursor struct {
	chunk int
	index int
}

type measurementHeap struct {
	chunks  [][]Measurement
	cursors []measurementCursor
}

func (items *measurementHeap) less(left, right measurementCursor) bool {
	a := items.chunks[left.chunk][left.index]
	b := items.chunks[right.chunk][right.index]
	if measurementLess(a, b) {
		return true
	}
	if measurementLess(b, a) {
		return false
	}
	return left.chunk < right.chunk
}

func (items *measurementHeap) push(value measurementCursor) {
	items.cursors = append(items.cursors, value)
	for index := len(items.cursors) - 1; index > 0; {
		parent := (index - 1) / 2
		if !items.less(items.cursors[index], items.cursors[parent]) {
			break
		}
		items.cursors[index], items.cursors[parent] = items.cursors[parent], items.cursors[index]
		index = parent
	}
}

func (items *measurementHeap) pop() measurementCursor {
	result := items.cursors[0]
	last := len(items.cursors) - 1
	items.cursors[0] = items.cursors[last]
	items.cursors = items.cursors[:last]
	for index := 0; ; {
		left := index*2 + 1
		if left >= len(items.cursors) {
			break
		}
		smallest := left
		right := left + 1
		if right < len(items.cursors) && items.less(items.cursors[right], items.cursors[left]) {
			smallest = right
		}
		if !items.less(items.cursors[smallest], items.cursors[index]) {
			break
		}
		items.cursors[index], items.cursors[smallest] = items.cursors[smallest], items.cursors[index]
		index = smallest
	}
	return result
}

func measurementLess(a, b Measurement) bool {
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
}

func mergeMeasurements(chunks [][]Measurement, count int) []Measurement {
	queue := &measurementHeap{chunks: chunks, cursors: make([]measurementCursor, 0, len(chunks))}
	for index, chunk := range chunks {
		if len(chunk) > 0 {
			queue.push(measurementCursor{chunk: index})
		}
	}
	output := make([]Measurement, 0, count)
	for len(queue.cursors) > 0 {
		cursor := queue.pop()
		output = append(output, chunks[cursor.chunk][cursor.index])
		cursor.index++
		if cursor.index < len(chunks[cursor.chunk]) {
			queue.push(cursor)
		}
	}
	return output
}

func newAnalysisCache() *analysisCache {
	return &analysisCache{cyclomatic: make(map[*facts.Function]int)}
}

func (cache *analysisCache) cyclomaticValue(function *facts.Function) int {
	if value, exists := cache.cyclomatic[function]; exists {
		return value
	}
	value := Cyclomatic(function)
	cache.cyclomatic[function] = value
	return value
}

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
	return strategy{component: component, definition: definition, measure: func(program *facts.Program) []Measurement {
		output := make([]Measurement, 0, len(program.Functions))
		for _, function := range program.Functions {
			output = append(output, Measurement{component, definition, "function", value(function), qualifiedFunctionName(function), function.Location, map[string]any{}})
		}
		return output
	}}
}

func cyclomaticMethodStrategy() Strategy {
	measure := func(program *facts.Program, value func(*facts.Function) int) []Measurement {
		output := make([]Measurement, 0, len(program.Functions))
		for _, function := range program.Functions {
			output = append(output, Measurement{"cyclomatic_method_complexity", "pmd-v1", "function", value(function), qualifiedFunctionName(function), function.Location, map[string]any{}})
		}
		return output
	}
	return strategy{
		component: "cyclomatic_method_complexity", definition: "pmd-v1",
		measure: func(program *facts.Program) []Measurement { return measure(program, Cyclomatic) },
		cached: func(program *facts.Program, cache *analysisCache) []Measurement {
			return measure(program, cache.cyclomaticValue)
		},
	}
}

func qualifiedFunctionName(function *facts.Function) string {
	if function.Receiver == "" {
		return function.Name
	}
	if function.Name == function.Receiver {
		return function.Receiver + ".<init>"
	}
	return function.Receiver + "." + function.Name
}

func defaultStrategies() []Strategy {
	return []Strategy{
		functionValueStrategy("cognitive_complexity", "pmd-sonar-v1", func(function *facts.Function) any { return Cognitive(function) }),
		cyclomaticMethodStrategy(),
		functionValueStrategy("npath_complexity", "pmd-v1", func(function *facts.Function) any { return NPath(function) }),
		deeplyNestedIfStrategy(),
		cyclomaticClassStrategy(),
		couplingStrategy(),
		godClassStrategy(),
		strategy{component: "module_shallowness", definition: shallowDefinition, measure: moduleShallownessMeasurements},
	}
}

func deeplyNestedIfStrategy() Strategy {
	return strategy{component: "deeply_nested_if", definition: "pmd-v1", measure: func(program *facts.Program) []Measurement {
		output := make([]Measurement, 0)
		for _, function := range program.Functions {
			locations := make([]facts.Location, 0)
			deepIf(function.Body, 0, &locations)
			for _, location := range locations {
				output = append(output, Measurement{"deeply_nested_if", "pmd-v1", "expression", 1, "if", location, map[string]any{
					"problem_depth": 3, "routine_symbol": qualifiedFunctionName(function),
				}})
			}
		}
		return output
	}}
}

func cyclomaticClassStrategy() Strategy {
	return strategy{component: "cyclomatic_class_complexity", definition: "pmd-v1", measure: func(program *facts.Program) []Measurement {
		output := make([]Measurement, 0, len(program.Types))
		for _, item := range program.Types {
			output = append(output, Measurement{"cyclomatic_class_complexity", "pmd-v1", "type", typeWMC(item), item.Name, item.Location, map[string]any{"kind": item.Kind}})
		}
		return output
	}, cached: func(program *facts.Program, cache *analysisCache) []Measurement {
		output := make([]Measurement, 0, len(program.Types))
		for _, item := range program.Types {
			output = append(output, Measurement{"cyclomatic_class_complexity", "pmd-v1", "type", typeWMCWith(item, cache.cyclomaticValue), item.Name, item.Location, map[string]any{"kind": item.Kind}})
		}
		return output
	}}
}

func couplingStrategy() Strategy {
	return strategy{component: "coupling_between_objects", definition: "pmd-v1", measure: func(program *facts.Program) []Measurement {
		output := make([]Measurement, 0, len(program.Types))
		for _, item := range program.Types {
			if available, _ := program.Availability(item.Location.Path, "coupling_between_objects"); !available {
				continue
			}
			output = append(output, Measurement{"coupling_between_objects", "pmd-v1", "type", len(unique(item.ForeignTypes)), item.Name, item.Location, map[string]any{"kind": item.Kind}})
		}
		return output
	}}
}

func godClassStrategy() Strategy {
	return strategy{component: "god_class", definition: "pmd-v1", measure: func(program *facts.Program) []Measurement {
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
	}, cached: func(program *facts.Program, cache *analysisCache) []Measurement {
		output := make([]Measurement, 0, len(program.Types))
		for _, item := range program.Types {
			if available, _ := program.Availability(item.Location.Path, "god_class"); !available || item.Kind == "interface" {
				continue
			}
			wmc := typeWMCWith(item, cache.cyclomaticValue)
			output = append(output, Measurement{"god_class", "pmd-v1", "type", wmc, item.Name, item.Location, map[string]any{
				"kind": item.Kind, "wmc": wmc, "atfd": len(unique(item.ForeignFields)), "tcc": tcc(item),
			}})
		}
		return output
	}}
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
	chunks := make([][]Measurement, 0, len(components))
	measurementCount := 0
	allSorted := true
	cache := newAnalysisCache()
	for _, component := range components {
		item, exists := registry.byComponent[component]
		if !exists {
			return nil, fmt.Errorf("no metric strategy for %s", component)
		}
		var measurements []Measurement
		if cached, ok := item.(interface {
			measureWithCache(*facts.Program, *analysisCache) []Measurement
		}); ok {
			measurements = cached.measureWithCache(program, cache)
		} else {
			measurements = item.Measure(program)
		}
		chunks = append(chunks, measurements)
		measurementCount += len(measurements)
		allSorted = allSorted && sort.SliceIsSorted(measurements, func(left, right int) bool {
			return measurementLess(measurements[left], measurements[right])
		})
	}
	if allSorted {
		return mergeMeasurements(chunks, measurementCount), nil
	}
	output := make([]Measurement, 0, measurementCount)
	for _, measurements := range chunks {
		output = append(output, measurements...)
	}
	sort.Slice(output, func(left, right int) bool {
		return measurementLess(output[left], output[right])
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
