package sourceestimate

import (
	"fmt"
	"math/rand"
	"reflect"
	"strings"
	"testing"
)

func rustInventoryTestUnits(sources ...string) []unit {
	units := make([]unit, len(sources))
	for i, source := range sources {
		file := File{Path: fmt.Sprintf("src/%d.rs", i), Language: "rust", Source: []byte(source)}
		tokens, limited, valid := lex(file.Source)
		units[i] = unit{file: file, index: i, tokens: tokens, pkg: packageName("rust", tokens), limited: limited, lexicallyValid: valid, inventory: &unitInventory{}}
	}
	return units
}
func rustReferenceCandidates(units []unit, owner, name string) []rustReleaseCandidate {
	var result []rustReleaseCandidate
	for i, u := range units {
		for _, fn := range rustFunctions(u.tokens) {
			if fn.owner == owner && fn.name == name {
				result = append(result, rustReleaseCandidate{i, fn})
			}
		}
	}
	return result
}
func TestRustWorkspaceInventoryReference(t *testing.T) {
	units := rustInventoryTestUnits("struct R {} impl R { fn release(&self) {} fn other(&self) {} }", "impl R { fn release(&self) {} }", "impl S { fn release(&self) {} }")
	prepareRustWorkspace(units)
	for _, current := range [][]unit{units, units[:1], units[1:], {units[2], units[0]}, rustInventoryTestUnits("impl R { fn release(&self) { changed(); } }")} {
		for _, key := range []rustMember{{"R", "release"}, {"S", "release"}, {"R", "missing"}, {"missing", "release"}} {
			got, want := rustReleaseCandidates(current, key.owner, key.name), rustReferenceCandidates(current, key.owner, key.name)
			if len(got) != len(want) || len(got) > 0 && !reflect.DeepEqual(got, want) {
				t.Fatalf("%v: got %#v want %#v", key, got, want)
			}
		}
	}
	reordered := []unit{units[1], units[0]}
	prepareRustWorkspace(reordered)
	if !reflect.DeepEqual(rustReleaseCandidates(reordered, "R", "release"), rustReferenceCandidates(reordered, "R", "release")) {
		t.Fatal("reordered context")
	}
}
func TestRustUnitInventoryImmutableContext(t *testing.T) {
	units := rustInventoryTestUnits("impl R { fn helper(&self) {} } impl Drop for R { fn drop(&mut self) {} }")
	u := units[0]
	first := rustUnitFunctions(u)
	if !reflect.DeepEqual(first, rustFunctions(u.tokens)) || first[0].impl != rustUnitFunctions(u)[0].impl {
		t.Fatal("function inventory not reused")
	}
	raw := rustUnitRawOperations(u)
	annotateRustAttribution(units)
	if raw[0].exposed || !units[0].ops[0].exposed {
		t.Fatal("raw inventory was attributed")
	}
	gradedCallerPrepare(units)
	if first[0].impl != rustUnitFunctions(units[0])[0].impl {
		t.Fatal("caller preparation discarded inventory")
	}
	contextual := u
	contextual.tokens = u.tokens[1:]
	if !reflect.DeepEqual(rustUnitFunctions(contextual), rustFunctions(contextual.tokens)) {
		t.Fatal("contextual token slice reused full inventory")
	}
	contextual = u
	contextual.index = 8
	if rustUnitRawOperations(contextual)[0].file != 8 {
		t.Fatal("contextual operation index reused")
	}
}
func TestRustInventoryHelpersReference(t *testing.T) {
	for _, source := range []string{
		"impl R { fn helper(&self) { cleanup(); } fn root(&self) { self.helper(); } }",
		"impl R { fn helper(&self) {} fn helper(&self) {} }",
		"impl R { async fn helper(&self) {} }",
		"impl T for R { fn helper(&self) {} }",
		"impl R { fn helper() {} }",
		"impl R { fn helper(&self) {",
	} {
		u := rustInventoryTestUnits(source)[0]
		if got, want := gradedRustUnitSynchronousHelper(u, "R", "helper"), gradedRustSynchronousHelper(u.tokens, "R", "helper"); got != want {
			t.Fatalf("%s: %v != %v", source, got, want)
		}
		body, _, _ := lex([]byte("self.helper();"))
		cached := gradedRustExpandHelpers(body, u, "R", 0, nil)
		u.inventory = nil
		if !reflect.DeepEqual(cached, gradedRustExpandHelpers(body, u, "R", 0, nil)) {
			t.Fatal("helper expansion differs")
		}
	}
}
func BenchmarkRustReleaseInventoryScaling(b *testing.B) {
	for _, size := range []int{8, 32, 128} {
		sources := make([]string, size)
		for i := range sources {
			sources[i] = fmt.Sprintf("struct R%d {} impl R%d { fn release(&self) {} }", i, i)
		}
		b.Run(fmt.Sprintf("units=%d/build-first-lookup", size), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				units := rustInventoryTestUnits(sources...)
				annotateRustAttribution(units)
				rustReleaseCandidates(units, "R0", "release")
			}
		})
		b.Run(fmt.Sprintf("units=%d/lookup", size), func(b *testing.B) {
			units := rustInventoryTestUnits(sources...)
			annotateRustAttribution(units)
			owners := make([]string, size)
			for i := range owners {
				owners[i] = fmt.Sprintf("R%d", i)
			}
			rustReleaseCandidates(units, "R0", "release")
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				for _, owner := range owners {
					if len(rustReleaseCandidates(units, owner, "release")) != 1 || len(rustReleaseCandidates(units, owner, "missing")) != 0 {
						b.Fatal("incorrect lookup")
					}
				}
			}
		})
	}
}

func TestRustParserInventoryReference(t *testing.T) {
	sources := []string{
		"struct R {} pub struct R {} pub trait T {} impl T for R { fn first(&self) {} fn second(&self) {} }",
		"mod private { pub struct R {} impl R { fn helper(&self) {} } } pub mod visible { pub fn f() {} }",
		"#[cfg(test)] mod tests { fn f() {} } #[test] fn t() {} fn ordinary() {}",
		"mod broken { fn f() {}",
		"mod outer { pub mod inner { fn f() {} } fn g() {} }",
		"impl R { fn f() { fn nested() {} } } impl R { fn g() {} }",
		"impl R { fn incomplete( ; fn valid() {} }",
		"pub(crate) mod restricted { pub(crate) fn f() {} }",
	}
	for _, source := range sources {
		tokens, _, _ := lex([]byte(source))
		if got, want := rustFunctions(tokens), referenceRustFunctions(tokens); !reflect.DeepEqual(got, want) {
			t.Fatalf("%s: got %#v want %#v", source, got, want)
		}
		for end := 0; end <= len(tokens); end++ {
			prefix := tokens[:end]
			if !reflect.DeepEqual(rustFunctions(prefix), referenceRustFunctions(prefix)) {
				t.Fatalf("truncated parser mismatch %s at %d", source, end)
			}
		}
		context := newRustParseContext(tokens)
		for i := range tokens {
			if context.moduleVisible[i] != rustModuleVisible(tokens, i) || context.testOnly[i] != rustTestOnly(tokens, i) {
				t.Fatalf("context mismatch %s at %d", source, i)
			}
		}
	}
}
func BenchmarkRustFunctionInventoryScaling(b *testing.B) {
	for _, size := range []int{8, 32, 128} {
		for _, manyImpls := range []bool{false, true} {
			var source strings.Builder
			if !manyImpls {
				source.WriteString("struct R {} impl R {")
			}
			for i := 0; i < size; i++ {
				if manyImpls {
					fmt.Fprintf(&source, "struct R%d {} impl R%d {", i, i)
				}
				fmt.Fprintf(&source, "fn f%d(&self) {}", i)
				if manyImpls {
					source.WriteString("}")
				}
			}
			if !manyImpls {
				source.WriteString("}")
			}
			tokens, _, _ := lex([]byte(source.String()))
			b.Run(fmt.Sprintf("functions=%d/manyImpls=%v", size, manyImpls), func(b *testing.B) {
				for i := 0; i < b.N; i++ {
					if len(rustFunctions(tokens)) != size {
						b.Fatal("missing functions")
					}
				}
			})
		}
	}
}

func TestRustPointerContractInventoryReference(t *testing.T) {
	for _, source := range []string{"use std::ptr; impl R { fn f() {} }", "mod std {}", "use core::ptr as raw; use other::raw;", "use std::ptr; fn ptr() {}"} {
		u := rustInventoryTestUnits(source)[0]
		if !reflect.DeepEqual(rustUnitPointerContracts(u), gradedRustPointerContracts(u.tokens)) {
			t.Fatal(source)
		}
		contextual := u
		contextual.tokens = u.tokens[1:]
		if !reflect.DeepEqual(rustUnitPointerContracts(contextual), gradedRustPointerContracts(contextual.tokens)) {
			t.Fatal("contextual contracts")
		}
		body, _, _ := lex([]byte("std::ptr::copy(self.p, self.p, 1);"))
		fields := map[string]gradedRustFlow{}
		gotD, gotM := gradedRustUnitPointerEffects(body, u, fields, "self")
		wantD, wantM := gradedRustPointerEffects(body, u.tokens, fields, "self")
		if gotD != wantD || gotM != wantM {
			t.Fatal("pointer effects")
		}
	}
}

func TestRustWorkspaceLazyReprepare(t *testing.T) {
	units := rustInventoryTestUnits("impl R {fn release(&self) {}}", "impl Other {fn release(&self) {}}")
	units[1].file.Language = "java"
	units[1].file.Path = "Other.java"
	prepareRustWorkspace(units)
	if units[0].inventory.rust != nil || units[1].inventory.rust != nil || units[0].inventory.rustWorkspace.members != nil {
		t.Fatal("preparation parsed source eagerly")
	}
	if len(rustReleaseCandidates(units, "Other", "release")) != 1 {
		t.Fatal("broad legacy lookup lost")
	}
	replacement := rustInventoryTestUnits("impl Changed {fn release(&self) {}}")
	units[1] = replacement[0]
	prepareRustWorkspace(units)
	if len(rustReleaseCandidates(units, "Other", "release")) != 0 || len(rustReleaseCandidates(units, "Changed", "release")) != 1 {
		t.Fatal("same backing slice retained stale members")
	}
}
func TestRustHelperExpansionExpected(t *testing.T) {
	cases := []struct{ source, want string }{
		{"impl R {fn helper(&self) {cleanup();}}", "{cleanup();};"},
		{"impl R {fn helper(&self) {} fn helper(&self) {}}", "self.helper();"},
		{"impl R {async fn helper(&self) {cleanup();}}", "self.helper();"},
		{"impl R {fn helper(&self) {self.helper();}}", "{self.helper();};"},
	}
	body, _, _ := lex([]byte("self.helper();"))
	for _, c := range cases {
		u := rustInventoryTestUnits(c.source)[0]
		if got := joinTokens(gradedRustExpandHelpers(body, u, "R", 0, nil)); got != c.want {
			t.Fatalf("%s: %s != %s", c.source, got, c.want)
		}
	}
}
func TestRustNestedAttributeReference(t *testing.T) {
	for _, size := range []int{8, 32, 128} {
		source := strings.Repeat("#[", size) + "test" + strings.Repeat("]", size) + " fn f() {}"
		tokens, _, _ := lex([]byte(source))
		if !reflect.DeepEqual(rustFunctions(tokens), referenceRustFunctions(tokens)) {
			t.Fatal("nested attribute mismatch")
		}
	}
	for _, parts := range [][]string{{"te", "st"}, {"c", "fg", "(", "te", "st", ")"}, {"", "test", ""}, {"test", "x"}} {
		tokens := make([]token, len(parts))
		for i, s := range parts {
			tokens[i] = token{text: s}
		}
		for _, target := range []string{"test", "cfg(test)"} {
			if rustTokenConcatEquals(tokens, target) != (joinTokens(tokens) == target) {
				t.Fatal(parts)
			}
		}
	}
}
func BenchmarkRustMalformedInventoryScaling(b *testing.B) {
	for _, size := range []int{32, 128, 512, 2048} {
		for _, kind := range []string{"parameters", "header", "impl", "impl-unclosed", "struct", "attribute"} {
			source := ""
			switch kind {
			case "parameters":
				source = strings.Repeat("fn f(", size)
			case "header":
				source = strings.Repeat("fn f() ", size)
			case "impl":
				source = strings.Repeat("impl T ", size)
			case "impl-unclosed":
				source = strings.Repeat("impl T ", size) + "{"
			case "struct":
				source = strings.Repeat("struct T ", size)
			case "attribute":
				source = strings.Repeat("#[", size) + "test" + strings.Repeat("]", size) + "fn f() {}"
			}
			tokens, _, _ := lex([]byte(source))
			if size <= 128 && !reflect.DeepEqual(rustFunctions(tokens), referenceRustFunctions(tokens)) {
				b.Fatal("reference mismatch")
			}
			b.Run(fmt.Sprintf("%s/%d", kind, size), func(b *testing.B) {
				for i := 0; i < b.N; i++ {
					rustFunctions(tokens)
				}
			})
		}
	}
}

func TestRustParserRandomReference(t *testing.T) {
	random := rand.New(rand.NewSource(7021))
	words := []string{"fn", "f", "impl", "T", "struct", "trait", "mod", "pub", "(", ")", "[", "]", "{", "}", "<", ">", ">>", ";", "=>", "#", "test", "cfg", "te", "st"}
	for sample := 0; sample < 300; sample++ {
		tokens := make([]token, 48)
		for i := range tokens {
			tokens[i] = token{text: words[random.Intn(len(words))]}
		}
		if !reflect.DeepEqual(rustFunctions(tokens), referenceRustFunctions(tokens)) {
			t.Fatalf("parser sample %d: %s", sample, joinTokens(tokens))
		}
		context := newRustParseContext(tokens)
		for i := range tokens {
			if context.moduleVisible[i] != rustModuleVisible(tokens, i) || context.testOnly[i] != rustTestOnly(tokens, i) || context.barePub[i] != rustBarePubBefore(tokens, i) || context.restricted[i] != rustRestrictedBefore(tokens, i) {
				t.Fatalf("context sample %d token %d: %s", sample, i, joinTokens(tokens))
			}
		}
	}
}
