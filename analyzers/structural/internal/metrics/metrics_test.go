package metrics

import (
	"math/big"
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
	duplicate := strategy{"cognitive_complexity", "other-v1", func(*facts.Program) []Measurement { return nil }}
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
