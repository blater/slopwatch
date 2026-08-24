package facts

import "testing"

func TestLinkTypeMethodsReusesCanonicalFunctions(t *testing.T) {
	location := Location{Path: "service.rs", Line: 4, Column: 5, EndLine: 6, EndColumn: 6}
	function := &Function{Name: "run", Location: location}
	item := &Type{Name: "Service", MethodLocations: []Location{location}}
	program := &Program{Functions: []*Function{function}, Types: []*Type{item}}

	if err := program.LinkTypeMethods(); err != nil {
		t.Fatal(err)
	}
	if len(item.Methods) != 1 || item.Methods[0] != function {
		t.Fatalf("method was not linked to the canonical function: %#v", item.Methods)
	}
	if item.MethodLocations != nil {
		t.Fatalf("transport references were retained: %#v", item.MethodLocations)
	}
}

func TestLinkTypeMethodsRejectsUnknownLocations(t *testing.T) {
	program := &Program{Types: []*Type{{
		Name:            "Service",
		MethodLocations: []Location{{Path: "missing.rs", Line: 1, Column: 1}},
	}}}
	if err := program.LinkTypeMethods(); err == nil {
		t.Fatal("expected an unknown method reference error")
	}
}
