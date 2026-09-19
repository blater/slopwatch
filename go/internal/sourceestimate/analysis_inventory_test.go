package sourceestimate

import (
	"reflect"
	"strings"
	"testing"
)

func inventoryTestUnit(language, path, source string) unit {
	file := File{Language: language, Path: path, Source: []byte(source)}
	tokens, limited, valid := lex([]byte(source))
	u := unit{file: file, tokens: tokens, pkg: packageName(language, tokens), limited: limited, lexicallyValid: valid, inventory: &unitInventory{}}
	u.ops = findOperations(file, 0, tokens, u.pkg)
	return u
}

func TestOwnerInventoryIsolationAndNegativeLookups(t *testing.T) {
	a := inventoryTestUnit("java", "a.java", `class Same { public int first; } class Other { public int second; }`)
	b := inventoryTestUnit("java", "b.java", `class Same { public int different; }`)
	for _, tc := range []struct {
		u            unit
		owner, field string
	}{{a, "Same", "first"}, {a, "Other", "second"}, {b, "Same", "different"}} {
		got := callerDeclaredFields(tc.u, tc.owner)
		if _, ok := got[tc.field]; !ok || len(got) != 1 {
			t.Fatalf("wrong inventory: %v", got)
		}
		if !reflect.DeepEqual(got, uncachedCallerDeclaredFields(tc.u, tc.owner)) {
			t.Fatal("cached fields differ")
		}
	}
	if callerDeclaredFields(a, "Missing") != nil {
		t.Fatal("unexpected missing owner")
	}
	if got, ok := a.inventory.fields["Missing"]; !ok || got != nil {
		t.Fatal("negative lookup not retained through value copy")
	}
	fresh := inventoryTestUnit("java", "a.java", `class Same { public int changed; }`)
	if _, ok := callerDeclaredFields(fresh, "Same")["changed"]; !ok {
		t.Fatal("inventory crossed analyses")
	}
	fallback := a
	fallback.inventory = nil
	if !reflect.DeepEqual(callerDeclaredFields(a, "Same"), callerDeclaredFields(fallback, "Same")) {
		t.Fatal("uncached fallback differs")
	}
}

const inventoryConstraintSource = `package p;type Example struct{Data []int;Count int};func(e Example)Safe()int{if e.Count<=0 || e.Count>len(e.Data){return 0};return e.Data[e.Count-1]};func(e Example)Unrelated()int{return 1}`

func TestFullOwnerConstraintsIsolationAndContext(t *testing.T) {
	u := inventoryTestUnit("go", "x.go", inventoryConstraintSource)
	got := gradedFullOwnerConstraints(u, "Example")
	if len(got) != 1 || !got[0].protected {
		t.Fatalf("fixture lacks protected constraint: %+v", got)
	}
	if !reflect.DeepEqual(got, gradedOwnerConstraints(u, "Example", u.ops)) {
		t.Fatal("full cache differs")
	}
	if len(u.inventory.constraints) != 1 {
		t.Fatal("cache did not persist")
	}
	// A selected root must not receive the cached whole-owner witness.
	if subset := gradedOwnerConstraints(u, "Example", u.ops[1:]); len(subset) != 0 {
		t.Fatalf("subset leaked full results: %+v", subset)
	}
	copied := *u.ops[0]
	copied.receiverName = "different"
	copied.constraintReceiver = "different"
	if constraints := gradedOwnerConstraints(u, "Example", []*operation{&copied}); len(constraints) != 0 {
		t.Fatalf("receiver copy leaked full results: %+v", constraints)
	}
	other := inventoryTestUnit("go", "other.go", strings.ReplaceAll(inventoryConstraintSource, "e.Data[e.Count-1]", "1"))
	if len(gradedFullOwnerConstraints(other, "Example")) != 0 {
		t.Fatal("constraint crossed unit boundary")
	}
	shadow := inventoryTestUnit("go", "shadow.go", `package p;func len(x []int)int{return 1000}`)
	units := []unit{u, shadow}
	old := u.inventory
	gradedCallerPrepare(units)
	got = gradedFullOwnerConstraints(units[0], "Example")
	if len(got) != 1 || got[0].protected {
		t.Fatalf("stale builtin context: %+v", got)
	}
	if units[0].inventory == old {
		t.Fatal("prepared context retained old cache")
	}
	if !reflect.DeepEqual(got, gradedOwnerConstraints(units[0], "Example", units[0].ops)) {
		t.Fatal("new context cache differs")
	}
	again := []unit{units[0]}
	gradedCallerPrepare(again)
	if got := gradedFullOwnerConstraints(again[0], "Example"); len(got) != 1 || !got[0].protected {
		t.Fatalf("repeated preparation retained shadow: %+v", got)
	}
}

func TestNormalizationEquivalenceAndInputOwnership(t *testing.T) {
	sources := []string{"", `return value;`, `if (false) { fail(); } return x;`, `if false { if false { fail(); } } next();`, `if (false) broken();`, `if (false) {bad();} else {good();}`, `if ((false)) {bad();} good();`, `if (false`, `if false {`, `const f = function() { function nested() { bad(); } }; good();`, `f = func() { bad(); }; good();`, `f = || { bad(); }; good();`, `f = move |x| { bad(); }; good();`, `f = x => { bad(); }; good();`, `f = x -> bad(); good();`, `f = x - > bad(); good();`, `f = |x;`, `function incomplete;`, `f = x => { bad();`, `if false {bad();}`}
	for _, language := range []string{"java", "typescript", "go", "rust"} {
		for _, source := range sources {
			t.Run(language+"/"+source, func(t *testing.T) {
				body, _, _ := lex([]byte(source))
				before := append([]token{}, body...)
				wantPruned := referencePruneDeadFalseBranches(body)
				gotPruned := pruneDeadFalseBranches(body)
				if !reflect.DeepEqual(wantPruned, gotPruned) {
					t.Fatalf("pruned tokens differ: %#v vs %#v", gotPruned, wantPruned)
				}
				want := referenceEagerBody(wantPruned, language)
				got := gradedEagerBody(gotPruned, language)
				if !reflect.DeepEqual(want, got) {
					t.Fatalf("eager tokens differ: %#v vs %#v", got, want)
				}
				if !reflect.DeepEqual(before, append([]token{}, body...)) {
					t.Fatal("input mutated")
				}
				if cap(got) != len(got) || cap(gotPruned) != len(gotPruned) {
					t.Fatal("shared result is not capped")
				}
				op := &operation{body: body, language: language}
				if !reflect.DeepEqual(normalizedEagerBody(op), want) || !reflect.DeepEqual(normalizedEagerBody(op), want) {
					t.Fatal("cached normalization differs")
				}
			})
		}
	}
	for _, body := range [][]token{nil, {}, make([]token, 0, 4)} {
		if !reflect.DeepEqual(pruneDeadFalseBranches(body), referencePruneDeadFalseBranches(body)) || !reflect.DeepEqual(gradedEagerBody(body, "go"), referenceEagerBody(body, "go")) {
			t.Fatal("empty behavior changed")
		}
	}
}

func TestNormalizationCopyInvalidation(t *testing.T) {
	body, _, _ := lex([]byte(`f = || { bad(); }; return x;`))
	op := &operation{body: body, language: "rust", id: "same"}
	normalizedEagerBody(op)
	copyOp := *op
	copyOp.language = "java"
	if !reflect.DeepEqual(normalizedEagerBody(&copyOp), referenceEagerBody(body, "java")) {
		t.Fatal("language key stale")
	}
	copyOp = *op
	copyOp.body = copyOp.body[:3]
	if !reflect.DeepEqual(normalizedEagerBody(&copyOp), referenceEagerBody(copyOp.body, "rust")) {
		t.Fatal("length key stale")
	}
	copyOp = *op
	copyOp.body, _, _ = lex([]byte(`return different;`))
	if !reflect.DeepEqual(normalizedEagerBody(&copyOp), referenceEagerBody(copyOp.body, "rust")) {
		t.Fatal("source identity key stale")
	}
	copyOp = *op
	copyOp.body = op.body[1:]
	if !reflect.DeepEqual(normalizedEagerBody(&copyOp), referenceEagerBody(copyOp.body, "rust")) {
		t.Fatal("slice start key stale")
	}
}

func TestNormalizationEmptyAndReplacementIdentity(t *testing.T) {
	op := &operation{language: "go"}
	for _, body := range [][]token{nil, {}, nil} {
		op.body = body
		if got, want := normalizedEagerBody(op), referenceEagerBody(referencePruneDeadFalseBranches(body), "go"); !reflect.DeepEqual(got, want) {
			t.Fatal("empty identity changed")
		}
	}
	for _, source := range []string{"return first;", "return other;"} {
		op.body, _, _ = lex([]byte(source))
		want := append([]token(nil), op.body...)
		got := normalizedEagerBody(op)
		if !reflect.DeepEqual(got, want) {
			t.Fatal("same-length replacement reused stale tokens")
		}
		got = append(got, token{text: "extra"})
		if !reflect.DeepEqual(op.body, want) {
			t.Fatal("append changed input")
		}
	}
}

var normalizationTestSink []token

func TestUnchangedNormalizationAllocations(t *testing.T) {
	body, _, _ := lex([]byte(`return this.data[this.count-1];`))
	for _, language := range []string{"java", "typescript", "go", "rust"} {
		allocations := testing.AllocsPerRun(100, func() { normalizationTestSink = gradedEagerBody(pruneDeadFalseBranches(body), language) })
		if allocations != 0 {
			t.Fatalf("%s unchanged normalization allocates %v", language, allocations)
		}
	}
}

func BenchmarkOwnerInventory(b *testing.B) {
	for _, cached := range []bool{false, true} {
		name := "uncached"
		if cached {
			name = "cached"
		}
		b.Run(name, func(b *testing.B) {
			u := inventoryTestUnit("go", "x.go", inventoryConstraintSource)
			if !cached {
				u.inventory = nil
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				callerDeclaredFields(u, "Example")
				gradedFullOwnerConstraints(u, "Example")
			}
		})
	}
}

func BenchmarkOperationNormalization(b *testing.B) {
	for _, source := range []string{`return this.data[this.count-1];`, `if(false){bad();} f = x => {bad();}; return x;`} {
		for _, cached := range []bool{false, true} {
			name := "reference/"
			if cached {
				name = "cached/"
			}
			b.Run(name+source, func(b *testing.B) {
				body, _, _ := lex([]byte(source))
				op := &operation{body: body, language: "java"}
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					if cached {
						normalizationTestSink = normalizedEagerBody(op)
					} else {
						normalizationTestSink = referenceEagerBody(referencePruneDeadFalseBranches(body), "java")
					}
				}
			})
		}
	}
}

func TestRepeatedAnalysisCacheIsolation(t *testing.T) {
	cases := []struct {
		language, path, source string
		analyze                func([]File) map[string]Result
	}{
		{"go", "example.go", inventoryConstraintSource, AnalyzeGoFiles},
		{"java", "Example.java", `public class Example { public int[] data; public int count; public int read(){ return data[count-1]; } }`, AnalyzeJavaFiles},
		{"typescript", "example.ts", `export class Example { data:number[]=[]; count:number=0; read():number {return this.data[this.count-1];} }`, AnalyzeTypeScriptFiles},
		{"rust", "example.rs", `pub struct Example {pub data:Vec<i32>,pub count:usize} impl Example {pub fn read(&self)->i32 {self.data[self.count-1]} }`, func(files []File) map[string]Result { results, _ := AnalyzeRustAttribution(files); return results }},
	}
	for _, tc := range cases {
		t.Run(tc.language, func(t *testing.T) {
			t.Parallel()
			files := []File{{Path: tc.path, Language: tc.language, Source: []byte(tc.source)}}
			want := tc.analyze(files)
			changed := []File{{Path: tc.path, Language: tc.language, Source: []byte(strings.ReplaceAll(strings.ReplaceAll(tc.source, "count", "position"), "Count", "Position"))}}
			tc.analyze(changed)
			for i := 0; i < 3; i++ {
				if got := tc.analyze(files); !reflect.DeepEqual(got, want) {
					t.Fatal("repeated analysis changed output")
				}
			}
		})
	}
}
