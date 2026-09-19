package sourceestimate

import "testing"

func gradedSurfaceUnit(language, path, source string) unit {
	file := File{Path: path, Language: language, Source: []byte(source)}
	tokens, limited, valid := lex(file.Source)
	pkg := packageName(language, tokens)
	item := unit{index: 0, file: file, tokens: tokens, pkg: pkg, limited: limited, lexicallyValid: valid}
	item.ops = findOperations(file, 0, tokens, pkg)
	return item
}

func TestGradedCallerSurfaceCountsCoupledRepresentation(t *testing.T) {
	cases := []struct {
		language string
		path     string
		source   string
	}{
		{"java", "Example.java", `public class Example { public int value; public int checksum; public boolean enabled; private int limit; public int read() { if (!enabled || value < 0 || value > limit || checksum != value * 31) throw new IllegalStateException(); return value; } }`},
		{"typescript", "example.ts", `export class Example { public value:number; public checksum:number; public enabled:boolean=true; private limit:number=10; read():number { if (!this.enabled || this.value < 0 || this.value > this.limit || this.checksum !== this.value * 31) throw new Error(); return this.value; } }`},
		{"go", "example.go", "package sample\ntype Example struct { Value int; Checksum int; Enabled bool; limit int }\nfunc (e Example) Read() int { if !e.Enabled || e.Value < 0 || e.Value > e.limit || e.Checksum != e.Value*31 { panic(\"invalid\") }; return e.Value }\n"},
		{"rust", "example.rs", `pub struct Example { pub value:i32, pub checksum:i32, pub enabled:bool, limit:i32 } impl Example { pub fn read(&self)->i32 { assert!(self.enabled && self.value >= 0 && self.value <= self.limit && self.checksum == self.value * 31); self.value } }`},
	}
	for _, tc := range cases {
		u := gradedSurfaceUnit(tc.language, tc.path, tc.source)
		if len(u.ops) == 0 {
			t.Fatalf("%s: no operation parsed", tc.language)
		}
		surface := gradedCallerSurface(u, u.ops)
		if surface.OperationUnits != 1 || surface.RepresentationUnits < 2 {
			t.Errorf("%s: surface=%+v", tc.language, surface)
		}
	}
}

func TestGradedCallerSurfaceKeepsScalarGetterCheap(t *testing.T) {
	u := gradedSurfaceUnit("java", "Value.java", `public class Value { public int value; public int read() { return value; } }`)
	surface := gradedCallerSurface(u, u.ops)
	if surface.OperationUnits != 0.25 || surface.RepresentationUnits != 0 {
		t.Fatalf("scalar getter surface=%+v", surface)
	}
}

func TestGradedCallerSurfaceInventoriesGroupedAndParameterFields(t *testing.T) {
	goUnit := gradedSurfaceUnit("go", "example.go", `package sample; type Example struct { Value,Checksum int; enabled bool }; func (e Example) Read() int { return e.Value + e.Checksum }`)
	goFields := callerDeclaredFields(goUnit, "Example")
	if !goFields["Value"].public || !goFields["Checksum"].public || goFields["enabled"].public {
		t.Fatalf("grouped Go fields=%+v", goFields)
	}

	tsUnit := gradedSurfaceUnit("typescript", "example.ts", `export class Example { constructor(private readonly limit:number, public value:number, public checksum:number=value*31, public enabled=true) {} read():number { return this.value; } }`)
	tsFields := callerDeclaredFields(tsUnit, "Example")
	if !tsFields["value"].public || !tsFields["checksum"].public || !tsFields["enabled"].public || tsFields["limit"].public {
		t.Fatalf("parameter-property fields=%+v", tsFields)
	}
}

func TestGradedCheapOperationRejectsComputedReturnsAndAllowsOwnedQueries(t *testing.T) {
	u := gradedSurfaceUnit("java", "Example.java", `public class Example { private Child graph; public int direct() { return value; } public int computed() { return value * 2; } public int query(Edge edge) { return graph.child(edge); } public int utility(Edge edge) { return Utility.child(edge); } public void flush() { graph.flush(); } }`)
	cheap := map[string]bool{}
	for _, op := range u.ops {
		cheap[op.name] = gradedCheapOperation(u, op)
	}
	if !cheap["direct"] || cheap["computed"] || !cheap["query"] || cheap["utility"] || cheap["flush"] {
		t.Fatalf("cheap operation classification=%v", cheap)
	}
}
