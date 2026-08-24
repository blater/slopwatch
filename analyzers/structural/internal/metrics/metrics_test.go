package metrics

import (
	"math/big"
	"sort"
	"testing"

	"slopslap.dev/structural/internal/facts"
)

func location(line int) facts.Location {
	return facts.Location{Path: "main.go", Line: line, Column: 1, EndLine: line, EndColumn: 2}
}

func TestPMDAlignedFunctionStrategies(t *testing.T) {
	condition := &facts.Expression{Kind: facts.ExprAnd, Children: []*facts.Expression{{}, {}}}
	function := &facts.Function{Name: "run", Location: location(1), Body: []*facts.Statement{{
		Kind: facts.StmtIf, Location: location(2), Condition: condition,
		Body: []*facts.Statement{{Kind: facts.StmtReturn, Location: location(3)}},
	}}}
	if got := Cognitive(function); got != 2 {
		t.Fatalf("cognitive = %d, want 2", got)
	}
	if got := Cyclomatic(function); got != 3 {
		t.Fatalf("cyclomatic = %d, want 3", got)
	}
	if got := NPath(function); got.Cmp(big.NewInt(3)) != 0 {
		t.Fatalf("NPath = %s, want 3", got)
	}
}

func TestRoutineMeasurementsUseOwnerQualifiedSymbols(t *testing.T) {
	method := &facts.Function{Name: "Run", Receiver: "Service", Location: location(4)}
	constructor := &facts.Function{Name: "Service", Receiver: "Service", Location: location(8)}
	measurements := Analyze(
		&facts.Program{Functions: []*facts.Function{method, constructor}},
		map[string]bool{"cognitive_complexity": true},
	)
	if len(measurements) != 2 {
		t.Fatalf("measurements = %#v", measurements)
	}
	if measurements[0].Subject != "Service.Run" || measurements[1].Subject != "Service.<init>" {
		t.Fatalf("subjects = %q, %q", measurements[0].Subject, measurements[1].Subject)
	}
}

func TestNPathIsUnboundedAndDeepIfStopsAtProblemDepth(t *testing.T) {
	body := make([]*facts.Statement, 0, 70)
	for index := 0; index < 70; index++ {
		body = append(body, &facts.Statement{Kind: facts.StmtIf, Location: location(index + 1), Condition: &facts.Expression{}})
	}
	nested := &facts.Statement{Kind: facts.StmtIf, Location: location(101), Body: []*facts.Statement{{
		Kind: facts.StmtIf, Location: location(102), Body: []*facts.Statement{{Kind: facts.StmtIf, Location: location(103)}},
	}}}
	function := &facts.Function{Name: "large", Location: location(1), Body: append(body, nested)}
	want := new(big.Int).Lsh(big.NewInt(1), 70)
	if got := NPath(&facts.Function{Name: "paths", Body: body}); got.Cmp(want) != 0 {
		t.Fatalf("NPath = %s, want %s", got, want)
	}
	measurements := Analyze(&facts.Program{Functions: []*facts.Function{function}}, map[string]bool{"deeply_nested_if": true})
	if len(measurements) != 1 || measurements[0].Location.Line != 103 {
		t.Fatalf("deep-if measurements = %#v", measurements)
	}
}

func TestTypeStrategiesUseSharedReceiverFacts(t *testing.T) {
	first := &facts.Function{Name: "A", Body: []*facts.Statement{{Kind: facts.StmtIf, Condition: &facts.Expression{}}}}
	second := &facts.Function{Name: "B"}
	item := &facts.Type{
		Name: "Service", Kind: "struct", Location: location(1), Methods: []*facts.Function{first, second},
		ForeignTypes:  []string{"Peer", "Peer", "pkg.Other"},
		ForeignFields: []string{"peer.Value", "peer.Value", "other.Name"},
		MethodFields:  map[string][]string{"A": {"state"}, "B": {"state"}},
	}
	requested := map[string]bool{
		"cyclomatic_class_complexity": true, "coupling_between_objects": true, "god_class": true,
	}
	measurements := Analyze(&facts.Program{Types: []*facts.Type{item}}, requested)
	values := make(map[string]Measurement)
	for _, measurement := range measurements {
		values[measurement.Component] = measurement
	}
	if values["cyclomatic_class_complexity"].Value != 3 {
		t.Fatalf("WMC = %v", values["cyclomatic_class_complexity"].Value)
	}
	if values["coupling_between_objects"].Value != 2 {
		t.Fatalf("CBO = %v", values["coupling_between_objects"].Value)
	}
	god := values["god_class"].Attributes
	if god["atfd"] != 2 || god["tcc"] != float64(1) {
		t.Fatalf("God facts = %#v", god)
	}
}

func TestAnalysisCacheSharesCyclomaticValuesAcrossTypeStrategies(t *testing.T) {
	method := &facts.Function{Name: "Run", Body: []*facts.Statement{{Kind: facts.StmtIf, Condition: &facts.Expression{}}}}
	cache := newAnalysisCache()
	if first, second := cache.cyclomaticValue(method), cache.cyclomaticValue(method); first != 2 || second != first {
		t.Fatalf("cached cyclomatic values = %d, %d", first, second)
	}
	if len(cache.cyclomatic) != 1 {
		t.Fatalf("cached functions = %d, want 1", len(cache.cyclomatic))
	}
	item := &facts.Type{Methods: []*facts.Function{method}}
	if got := typeWMCWith(item, cache.cyclomaticValue); got != 2 || len(cache.cyclomatic) != 1 {
		t.Fatalf("cached WMC = %d, cache size = %d", got, len(cache.cyclomatic))
	}
}

func TestMergeMeasurementsPreservesLocationMajorOrder(t *testing.T) {
	chunks := [][]Measurement{
		{
			{Component: "npath_complexity", Location: facts.Location{Path: "a.go", Line: 1}},
			{Component: "npath_complexity", Location: facts.Location{Path: "b.go", Line: 2}},
		},
		{
			{Component: "cognitive_complexity", Location: facts.Location{Path: "a.go", Line: 1}},
			{Component: "cognitive_complexity", Location: facts.Location{Path: "c.go", Line: 1}},
		},
	}
	got := mergeMeasurements(chunks, 4)
	want := []string{
		"a.go:cognitive_complexity", "a.go:npath_complexity",
		"b.go:npath_complexity", "c.go:cognitive_complexity",
	}
	for index, measurement := range got {
		value := measurement.Location.Path + ":" + measurement.Component
		if value != want[index] {
			t.Fatalf("measurement %d = %q, want %q", index, value, want[index])
		}
	}
}

func TestRegistryFallsBackForUnsortedStrategyOutput(t *testing.T) {
	registry, err := NewRegistry(strategy{
		component: "custom", definition: "v1",
		measure: func(*facts.Program) []Measurement {
			return []Measurement{
				{Component: "custom", Location: facts.Location{Path: "z.go"}},
				{Component: "custom", Location: facts.Location{Path: "a.go"}},
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := registry.Analyze(&facts.Program{}, map[string]bool{"custom": true})
	if err != nil {
		t.Fatal(err)
	}
	if got[0].Location.Path != "a.go" || got[1].Location.Path != "z.go" {
		t.Fatalf("fallback output is not sorted: %#v", got)
	}
}

func BenchmarkMeasurementOrdering(b *testing.B) {
	const chunkCount, measurementsPerChunk = 8, 10_000
	chunks := make([][]Measurement, chunkCount)
	all := make([]Measurement, 0, chunkCount*measurementsPerChunk)
	for component := range chunks {
		chunks[component] = make([]Measurement, measurementsPerChunk)
		for index := range chunks[component] {
			chunks[component][index] = Measurement{
				Component: string(rune('a' + component)),
				Location:  facts.Location{Path: "source.go", Line: index*chunkCount + component},
			}
		}
		all = append(all, chunks[component]...)
	}
	b.Run("global-sort", func(b *testing.B) {
		for range b.N {
			output := append([]Measurement(nil), all...)
			sort.Slice(output, func(left, right int) bool {
				return measurementLess(output[left], output[right])
			})
		}
	})
	b.Run("sorted-k-way-merge", func(b *testing.B) {
		for range b.N {
			mergeMeasurements(chunks, len(all))
		}
	})
}

func BenchmarkSharedCyclomaticCache(b *testing.B) {
	methods := make([]*facts.Function, 1000)
	for index := range methods {
		body := make([]*facts.Statement, 100)
		for statement := range body {
			body[statement] = &facts.Statement{
				Kind: facts.StmtIf, Condition: &facts.Expression{Kind: facts.ExprAnd, Children: []*facts.Expression{{}, {}}},
			}
		}
		methods[index] = &facts.Function{Name: "Run", Body: body}
	}
	item := &facts.Type{Methods: methods}
	b.Run("uncached-three-strategies", func(b *testing.B) {
		for range b.N {
			for _, method := range methods {
				Cyclomatic(method)
			}
			typeWMC(item)
			typeWMC(item)
		}
	})
	b.Run("shared-analysis-cache", func(b *testing.B) {
		for range b.N {
			cache := newAnalysisCache()
			for _, method := range methods {
				cache.cyclomaticValue(method)
			}
			typeWMCWith(item, cache.cyclomaticValue)
			typeWMCWith(item, cache.cyclomaticValue)
		}
	})
}

func TestDefaultRegistryOwnsAllVersionedStrategies(t *testing.T) {
	want := map[string]string{
		"cognitive_complexity":         "pmd-sonar-v1",
		"cyclomatic_method_complexity": "pmd-v1",
		"npath_complexity":             "pmd-v1",
		"deeply_nested_if":             "pmd-v1",
		"cyclomatic_class_complexity":  "pmd-v1",
		"coupling_between_objects":     "pmd-v1",
		"god_class":                    "pmd-v1",
		"module_shallowness":           "ousterhout-v3",
	}
	got := DefaultRegistry().Definitions()
	if len(got) != len(want) {
		t.Fatalf("definitions = %#v", got)
	}
	for component, version := range want {
		if got[component] != version {
			t.Fatalf("%s = %q", component, got[component])
		}
	}
	duplicate := strategy{component: "cognitive_complexity", definition: "other-v1", measure: func(*facts.Program) []Measurement { return nil }}
	if _, err := NewRegistry(defaultStrategies()[0], duplicate); err == nil {
		t.Fatal("expected duplicate strategy rejection")
	}
}

func TestModuleShallownessUsesCapabilityAndInterfaceNotImplementationSize(t *testing.T) {
	public := &facts.Function{
		Name: "Run", Body: []*facts.Statement{{Kind: facts.StmtReturn, Expressions: []*facts.Expression{{}}}},
	}
	private := &facts.Function{Name: "helper", Body: make([]*facts.Statement, 100)}
	base, baseAttributes := ModuleShallowness([]*facts.Function{public}, nil)
	withPrivate, privateAttributes := ModuleShallowness([]*facts.Function{public, private}, nil)
	if base != withPrivate {
		t.Fatalf("private implementation changed SHALLOW: base=%d with_private=%d", base, withPrivate)
	}
	if baseAttributes["functionality"] != privateAttributes["functionality"] {
		t.Fatalf("private implementation changed functionality: %#v vs %#v", baseAttributes, privateAttributes)
	}
}

func TestModuleShallownessPenalizesAdditionalInterfaceCost(t *testing.T) {
	public := &facts.Function{
		Name: "Run", Body: []*facts.Statement{{Kind: facts.StmtReturn, Expressions: []*facts.Expression{{}}}},
	}
	shallow, _ := ModuleShallowness([]*facts.Function{public}, []*facts.Type{
		{Name: "Payload"}, {Name: "Request"}, {Name: "Response"},
	})
	deep, _ := ModuleShallowness([]*facts.Function{public}, nil)
	if shallow <= deep {
		t.Fatalf("additional interface cost did not increase penalty: shallow=%d deep=%d", shallow, deep)
	}
}

func TestSignatureShallownessUsesPublicBoundaryFacts(t *testing.T) {
	primitive := &facts.TypeShape{Kind: "primitive", Complexity: 1}
	complex := &facts.TypeShape{Kind: "record", Complexity: 5}
	deep, _ := moduleShallownessWithOperations([]*facts.PublicOperation{{
		Name: "Run", Parameters: []*facts.TypeShape{primitive}, Results: []*facts.TypeShape{primitive},
	}}, nil)
	shallow, attributes := moduleShallownessWithOperations([]*facts.PublicOperation{{
		Name: "Run", Parameters: []*facts.TypeShape{complex}, Results: []*facts.TypeShape{complex},
	}}, nil)
	if shallow <= deep {
		t.Fatalf("complexer signature did not increase penalty: shallow=%d deep=%d", shallow, deep)
	}
	if attributes["evidence_level"] != 2 || attributes["capability_basis"] != "cosmic-inspired-signature" {
		t.Fatalf("signature evidence metadata = %#v", attributes)
	}
}

func TestSignatureShallownessPenalizesExposedMutableFields(t *testing.T) {
	operation := &facts.PublicOperation{Name: "Run", Results: []*facts.TypeShape{{Kind: "primitive", Complexity: 1}}}
	withoutFields, _ := moduleShallownessWithOperations([]*facts.PublicOperation{operation}, []*facts.Type{{Name: "Service"}})
	withFields, attributes := moduleShallownessWithOperations([]*facts.PublicOperation{operation}, []*facts.Type{{
		Name: "Service", Fields: []facts.Field{{Name: "State", Public: true, Mutable: true}},
	}})
	if withFields <= withoutFields || attributes["exposed_representation"] != 1 {
		t.Fatalf("exposed field did not increase penalty: with=%d without=%d attrs=%#v", withFields, withoutFields, attributes)
	}
}

func TestModuleShallownessUnavailableWithoutPublicInterface(t *testing.T) {
	value, attributes := ModuleShallowness([]*facts.Function{{Name: "private"}}, nil)
	if value != 0 || attributes["available"] != false {
		t.Fatalf("private-only module should be unavailable: value=%d attributes=%#v", value, attributes)
	}
}
