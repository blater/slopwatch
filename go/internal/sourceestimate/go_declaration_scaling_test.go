package sourceestimate

import (
	"fmt"
	"strings"
	"testing"
)

func TestGoReceiverInventoryMatchesScanner(t *testing.T) {
	for _, source := range []string{
		`package p;type A struct{};type B struct{};func(a A) Same(){};func(b *B) Same(){};func(a *A) Same(){}`,
		`package p;type A struct{};func(a *A) Same(){};func(a A) Same(){}`,
	} {
		u := gradedSurfaceUnit("go", "x.go", source)
		u.inventory = &unitInventory{}
		for _, name := range []string{"Same", "Missing"} {
			for _, owner := range []string{"A", "B", "a", "b", "*", "Missing"} {
				op := &operation{language: "go", name: name, owner: owner, receiverName: "contextual"}
				want := referenceMutableGoReceiver(op, u)
				if got := gradedMutableGoReceiver(op, u); got != want {
					t.Fatalf("%s/%s got %v want %v", name, owner, got, want)
				}
			}
		}
	}
}

func referenceMutableGoReceiver(op *operation, u unit) bool {
	for i, t := range u.tokens {
		if t.text != "func" || i+1 >= len(u.tokens) || u.tokens[i+1].text != "(" {
			continue
		}
		close := matching(u.tokens, i+1, "(", ")")
		if close < 0 || close+1 >= len(u.tokens) || u.tokens[close+1].text != op.name {
			continue
		}
		owner, pointer := false, false
		for _, tok := range u.tokens[i+2 : close] {
			owner = owner || tok.text == op.owner
			pointer = pointer || tok.text == "*"
		}
		if owner {
			return pointer
		}
	}
	return false
}

func TestGoOperationTypeMapOwnershipAndShadowing(t *testing.T) {
	fields := map[string]string{"x": "Original", "unused": "Other", "empty": ""}
	body, _, _ := lex([]byte(`x := &Local{}; empty.Call(); x.Run()`))
	selected := fields
	selected = receiverFields(selected, "r", "Receiver")
	local := goLocalReceiverTypes(body, selected)
	if local["x"] != "Local" || local["r"] != "Receiver" {
		t.Fatalf("bindings: %v", local)
	}
	if _, ok := local["empty"]; !ok {
		t.Fatal("empty type presence lost")
	}
	if selected["x"] != "Original" || fields["x"] != "Original" {
		t.Fatal("local binding mutated shared input")
	}
	if _, ok := fields["r"]; ok {
		t.Fatal("receiver binding mutated source")
	}
	local["empty"] = "Changed"
	if fields["empty"] != "" {
		t.Fatal("source map changed")
	}
	reassigned, _, _ := lex([]byte(`x := Local{}; x = Other{}`))
	if got := goLocalReceiverTypes(reassigned, fields)["x"]; got != "Original" {
		t.Fatalf("reassignment changed binding: %s", got)
	}
}

func TestGoContextualBodyRetainsSourceFieldTypes(t *testing.T) {
	u := gradedSurfaceUnit("go", "x.go", `package p;type Worker struct{};type A struct{ Hidden Worker };func(a *A) Run(){}`)
	if len(u.ops) != 1 {
		t.Fatalf("ops: %d", len(u.ops))
	}
	op := *u.ops[0]
	op.body, _, _ = lex([]byte(`a.Hidden.Execute()`))
	name, owner, typed := resolveCallOwner(&op, "a.Hidden.Execute")
	if name != "Execute" || owner != "Worker" || !typed {
		t.Fatalf("substituted field resolution: %s %s %v", name, owner, typed)
	}
	if typ, _ := operationFieldType(&op, "a"); typ != "A" {
		t.Fatalf("receiver: %s", typ)
	}
}

func TestConstraintBindingCandidateParameterPrecedence(t *testing.T) {
	for _, tc := range []struct {
		typ  string
		want bool
	}{{"Target", true}, {"Unknown", false}, {"", true}} {
		target := gradedSurfaceUnit("java", "Target.java", `package p;class Target{int[] data;int count;void reset(){count=0;}}`)
		caller := gradedSurfaceUnit("java", "Caller.java", `package p;class Caller{Target target;int read(Target target){return target.data[target.count-1];}}`)
		for _, op := range caller.ops {
			op.file = 1
			op.parameterTypes["target"] = tc.typ
		}
		units := []unit{target, caller}
		annotateConstraintWitnesses(units)
		got := len(units[0].callerObligations["Target"].constraints) > 0
		if got != tc.want {
			t.Fatalf("parameter type %q witness=%v want=%v", tc.typ, got, tc.want)
		}
	}
}

func TestGoReceiverInventoryContextualTokens(t *testing.T) {
	u := gradedSurfaceUnit("go", "x.go", `package p;type A struct{};func(a A) Same(){};func(a *A) Same(){}`)
	u.inventory = &unitInventory{}
	op := &operation{language: "go", name: "Same", owner: "A"}
	if gradedMutableGoReceiver(op, u) {
		t.Fatal("first declaration is value")
	}
	for i, tok := range u.tokens {
		if tok.text != "func" {
			continue
		}
		contextual := u
		contextual.tokens = u.tokens[i:]
		if got, want := gradedMutableGoReceiver(op, contextual), referenceMutableGoReceiver(op, contextual); got != want {
			t.Fatalf("slice %d got %v want %v", i, got, want)
		}
		contextual.tokens = u.tokens[:i]
		if got, want := gradedMutableGoReceiver(op, contextual), referenceMutableGoReceiver(op, contextual); got != want {
			t.Fatalf("prefix %d got %v want %v", i, got, want)
		}
	}
	replaced := u
	replaced.tokens, _, _ = lex([]byte(`func(a *A) Same(){}`))
	if !gradedMutableGoReceiver(op, replaced) || gradedMutableGoReceiver(op, u) {
		t.Fatal("replacement contaminated source")
	}
}

func TestConsumerTypeLayerPrecedence(t *testing.T) {
	op := &operation{fieldTypes: map[string]string{"base": "Base", "go": "Base", "old": "Base", "declared": "Base", "parameter": "Base"}, fieldTypeBindings: map[string]string{"go": "Go", "old": "Go"}, fieldTypeContext: &operationTypeContext{parameters: map[string]string{"old": "Old"}}}
	copy := *op
	copy.fieldTypeContext = &operationTypeContext{parameters: map[string]string{"parameter": ""}, fields: map[string]gradedSurfaceField{"declared": {typeName: "Declared"}, "parameter": {typeName: "Declared"}}, previous: op.fieldTypeContext}
	for name, want := range map[string]string{"base": "Base", "go": "Go", "old": "Old", "declared": "Declared", "parameter": ""} {
		if got, ok := operationFieldType(&copy, name); !ok || got != want {
			t.Fatalf("%s got %q,%v want %q", name, got, ok, want)
		}
	}
	if got, _ := operationFieldType(op, "declared"); got != "Base" {
		t.Fatal("context changed original lookup")
	}
}

func TestConstraintWitnessCalleeOnlyReceiver(t *testing.T) {
	target := gradedSurfaceUnit("go", "state.go", `package p;type State struct{Low int;High int}`)
	caller := gradedSurfaceUnit("go", "caller.go", `package p;var s State;var d Driver;type Caller struct{s State;d Driver};func(c Caller) Run()int{return d.Check()}`)
	driver := gradedSurfaceUnit("go", "driver.go", `package p;type Driver struct{};func(d Driver) Check()int{if s.Low>=s.High{return -1};return effect()}`)
	units := []unit{target, caller, driver}
	for i := range units {
		for _, op := range units[i].ops {
			op.file = i
		}
	}
	annotateConstraintWitnesses(units)
	if len(units[0].callerObligations["State"].constraints) == 0 {
		t.Fatal("callee-only receiver obligation lost")
	}
}

func BenchmarkTypedDeclarationScaling(b *testing.B) {
	for _, count := range []int{8, 32, 128, 256} {
		b.Run(fmt.Sprintf("declarations=%d", count), func(b *testing.B) {
			var source strings.Builder
			source.WriteString("package p;type State struct{Low int;High int};type Example struct{")
			for i := 0; i < count; i++ {
				fmt.Fprintf(&source, "F%d State;", i)
			}
			source.WriteString("};")
			for i := 0; i < count; i++ {
				fmt.Fprintf(&source, "func(e Example) M%d()int{return e.F%d.Low};", i, i)
			}
			files := []File{{Path: "typed.go", Language: "go", Source: []byte(source.String())}}
			u := gradedSurfaceUnit("go", "typed.go", source.String())
			if len(u.ops) != count {
				b.Fatalf("operations=%d", len(u.ops))
			}
			if result := AnalyzeGoFiles(files)["typed.go"]; !result.Applicable {
				b.Fatal("missing applicable grade")
			}
			b.ReportAllocs()
			b.SetBytes(int64(source.Len()))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				scalingResults = AnalyzeGoFiles(files)
			}
		})
	}
}
