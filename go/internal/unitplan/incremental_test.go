package unitplan

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func writeDeltaFixture(t *testing.T, root, path, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(filepath.Join(root, path)), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, path), []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
}
func compareIncrementalPlan(t *testing.T, root string, index *Index, ids map[string]bool, options Options) {
	t.Helper()
	reference, err := PlanWorkspace(root, options)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]Unit{}
	for _, unit := range reference.Units {
		want[unit.ID] = unit
		ids[unit.ID] = true
	}
	got := map[string]Unit{}
	for id := range ids {
		if unit, ok := index.Unit(id); ok {
			unit.ReverseDependencies = index.Reverse(id)
			got[id] = unit
		}
	}
	normalize := func(units map[string]Unit) {
		for id, unit := range units {
			unit.Sources = uniqueStrings(unit.Sources)
			unit.ContextSources = uniqueStrings(unit.ContextSources)
			unit.ConfigInputs = uniqueStrings(unit.ConfigInputs)
			unit.DirectDependencies = uniqueStrings(unit.DirectDependencies)
			unit.ReverseDependencies = uniqueStrings(unit.ReverseDependencies)
			unit.Capabilities = uniqueCapabilities(unit.Capabilities)
			units[id] = unit
		}
	}
	normalize(got)
	normalize(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("incremental plan differs\ngot: %#v\nwant: %#v", got, want)
	}
}
func TestSparseLanguageDeltasMatchFullPlanner(t *testing.T) {
	cases := []struct {
		name    string
		initial map[string]string
		steps   []map[string]*string
		options Options
	}{
		{name: "go", initial: map[string]string{"go.mod": "module example\n", "app/a.go": "package app\nimport _ \"example/lib\"\n", "app/b.go": "package app\nimport _ \"example/lib\"\n"}, steps: []map[string]*string{{"lib/lib.go": textPointer("package lib\n")}, {"lib/lib.go": textPointer("package lib\nimport _ \"example/app\"\n")}, {"lib/lib.go": textPointer("package lib\n")}, {"app/a_test.go": textPointer("package app\nimport \"testing\"\nfunc TestA(t *testing.T) {}\n")}, {"app/a.go": textPointer("package app\n")}, {"lib/lib.go": nil}, {"lib/lib.go": textPointer("package lib\n")}, {"app/b.go": nil, "app/a.go": nil, "app/new.go": textPointer("package app\n"), "app/missing.go": nil}}},
		{name: "java", initial: map[string]string{"app/pom.xml": "<project><groupId>x</groupId><artifactId>app</artifactId><dependencies><dependency><groupId>x</groupId><artifactId>lib</artifactId></dependency></dependencies></project>", "lib/pom.xml": "<project><groupId>x</groupId><artifactId>lib</artifactId></project>", "app/src/main/java/A.java": "class A {}"}, steps: []map[string]*string{{"lib/src/main/java/L.java": textPointer("class L {}")}, {"app/src/test/java/T.java": textPointer("class T {}")}, {"loose/X.java": textPointer("class X {}")}, {"loose/X.java": nil}, {"lib/src/main/java/L.java": nil}, {"lib/src/main/java/B.java": textPointer("class B {}")}}},
		{name: "typescript", options: Options{TypeScriptMode: TypeScriptTyped}, initial: map[string]string{"tsconfig.json": "{}", "nested/tsconfig.json": "{}", "a.ts": "export const a=1;"}, steps: []map[string]*string{{"nested/b.ts": textPointer("export const b=2;")}, {"nested/global.d.ts": textPointer("declare const globalValue:number;")}, {"nested/b.ts": nil}, {"nested/global.d.ts": nil}}},
		{name: "rust", initial: map[string]string{"app/Cargo.toml": "[package]\nname=\"app\"\nversion=\"0.1.0\"\n[dependencies]\nlib={path=\"../lib\"}\n", "lib/Cargo.toml": "[package]\nname=\"lib\"\nversion=\"0.1.0\"\n", "app/src/main.rs": "fn main() {}"}, steps: []map[string]*string{{"lib/src/lib.rs": textPointer("pub fn f() {}")}, {"lib/src/main.rs": textPointer("fn main() {}")}, {"loose.rs": textPointer("pub fn loose() {}")}, {"loose.rs": nil}, {"lib/src/lib.rs": nil}, {"lib/src/main.rs": nil}, {"lib/src/lib.rs": textPointer("pub fn again() {}")}}},
		{name: "typescript-references", options: Options{TypeScriptMode: TypeScriptTyped}, initial: map[string]string{"app/tsconfig.json": "{\"references\":[{\"path\":\"../lib\"}]}", "lib/tsconfig.json": "{}", "app/a.ts": "export const a=1;"}, steps: []map[string]*string{{"lib/b.ts": textPointer("export const b=2;")}, {"lib/b.ts": nil}, {"lib/c.ts": textPointer("export const c=3;")}}},
		{name: "typescript-syntax-declarations", options: Options{TypeScriptMode: TypeScriptSyntax}, initial: map[string]string{"package.json": "{}", "types.d.ts": "declare const a:number;", "other/a.go": "package a\n"}, steps: []map[string]*string{{"first.ts": textPointer("export const b=a;")}, {"first.ts": nil}, {"second.ts": textPointer("export const c=a;")}}},
	}
	cases = append(cases, struct {
		name    string
		initial map[string]string
		steps   []map[string]*string
		options Options
	}{name: "typescript-first-language-with-declarations", options: Options{TypeScriptMode: TypeScriptTyped}, initial: map[string]string{"app/tsconfig.json": "{\"references\":[{\"path\":\"../types\"}]}", "types/tsconfig.json": "{}", "types/global.d.ts": "declare const value:number;"}, steps: []map[string]*string{{"app/a.ts": textPointer("export const a=value;")}, {"app/a.ts": nil}, {"app/b.ts": textPointer("export const b=value;")}}})
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			for path, data := range test.initial {
				writeDeltaFixture(t, root, path, data)
			}
			plan, err := PlanWorkspace(root, test.options)
			if err != nil {
				t.Fatal(err)
			}
			ids := map[string]bool{}
			for _, unit := range plan.Units {
				ids[unit.ID] = true
			}
			compareIncrementalPlan(t, root, plan.Index, ids, test.options)
			for number, step := range test.steps {
				t.Run(string(rune('a'+number)), func(t *testing.T) {
					changes := map[string]*Source{}
					for path, data := range step {
						if data == nil {
							if err := os.Remove(filepath.Join(root, path)); err != nil && !os.IsNotExist(err) {
								t.Fatal(err)
							}
							if _, exists := plan.Index.Source(path); exists {
								changes[path] = nil
							}
						} else {
							writeDeltaFixture(t, root, path, *data)
							changes[path] = ParseSource(path, []byte(*data))
						}
					}
					delta := plan.Index.Prepare(changes)
					for id := range delta.Units {
						ids[id] = true
					}
					delta.Commit()
					compareIncrementalPlan(t, root, plan.Index, ids, test.options)
				})
			}
		})
	}
}
func textPointer(value string) *string { return &value }

func TestSameOwnerBatchPreservesTombstones(t *testing.T) {
	for iteration := 0; iteration < 30; iteration++ {
		root := t.TempDir()
		writeDeltaFixture(t, root, "go.mod", "module example\n")
		writeDeltaFixture(t, root, "pkg/a.go", "package pkg\n")
		writeDeltaFixture(t, root, "pkg/b.go", "package pkg\n")
		plan, err := PlanWorkspace(root, Options{})
		if err != nil {
			t.Fatal(err)
		}
		changes := map[string]*Source{"pkg/a.go": nil, "pkg/b.go": nil, "pkg/new.go": ParseSource("pkg/new.go", []byte("package pkg\n"))}
		delta := plan.Index.Prepare(changes)
		unit, ok := delta.Unit("go:package:pkg")
		if !ok {
			t.Fatal("new member missing")
		}
		sort.Strings(unit.Sources)
		if !reflect.DeepEqual(unit.Sources, []string{"pkg/new.go"}) {
			t.Fatalf("deleted members resurrected: %v", unit.Sources)
		}
	}
}

func TestProjectDirectoryMetadataDoesNotDecodeIDs(t *testing.T) {
	for _, language := range []string{"java", "rust"} {
		for _, directory := range []string{"root", "with:colon"} {
			t.Run(language+"/"+directory, func(t *testing.T) {
				root := t.TempDir()
				path, data := directory+"/src/main/java/A.java", "class A {}"
				if language == "java" {
					writeDeltaFixture(t, root, directory+"/pom.xml", "<project><groupId>x</groupId><artifactId>lib</artifactId></project>")
					writeDeltaFixture(t, root, "app/pom.xml", "<project><groupId>x</groupId><artifactId>app</artifactId><dependencies><dependency><groupId>x</groupId><artifactId>lib</artifactId></dependency></dependencies></project>")
					writeDeltaFixture(t, root, "app/src/main/java/App.java", "class App {}")
				} else {
					path, data = directory+"/src/lib.rs", "pub fn a() {}"
					writeDeltaFixture(t, root, directory+"/Cargo.toml", "[package]\nname=\"lib\"\nversion=\"0.1.0\"\n")
					writeDeltaFixture(t, root, "app/Cargo.toml", "[package]\nname=\"app\"\nversion=\"0.1.0\"\n[dependencies]\nlib={path=\"../"+directory+"\"}\n")
					writeDeltaFixture(t, root, "app/src/main.rs", "fn main() {}")
				}
				writeDeltaFixture(t, root, path, data)
				plan, err := PlanWorkspace(root, Options{})
				if err != nil {
					t.Fatal(err)
				}
				ids := map[string]bool{}
				for _, unit := range plan.Units {
					ids[unit.ID] = true
				}
				compareIncrementalPlan(t, root, plan.Index, ids, Options{})
				if err := os.Remove(filepath.Join(root, path)); err != nil {
					t.Fatal(err)
				}
				delta := plan.Index.Prepare(map[string]*Source{path: nil})
				delta.Commit()
				compareIncrementalPlan(t, root, plan.Index, ids, Options{})
				writeDeltaFixture(t, root, path, data)
				delta = plan.Index.Prepare(map[string]*Source{path: ParseSource(path, []byte(data))})
				delta.Commit()
				compareIncrementalPlan(t, root, plan.Index, ids, Options{})
			})
		}
	}
}

func TestCandidateBucketsAndLargeOwnerWorkIsLinear(t *testing.T) {
	for _, oneOwner := range []bool{false, true} {
		for _, size := range []int{64, 256} {
			t.Run(fmt.Sprintf("oneOwner=%t/size=%d", oneOwner, size), func(t *testing.T) {
				index := newIndex()
				index.goModules["."] = "example"
				counts := map[string]int{}
				index.work = func(op string) { counts[op]++ }
				changes := map[string]*Source{}
				for n := 0; n < size; n++ {
					path := fmt.Sprintf("p%d/a.go", n)
					if oneOwner {
						path = fmt.Sprintf("p/a%d.go", n)
					}
					changes[path] = ParseSource(path, []byte("package p\nimport _ \"example/lib\"\n"))
				}
				delta := index.Prepare(changes)
				for path := range changes {
					if len(delta.Consumers(path)) != 1 || len(delta.Owners(path)) != 1 {
						t.Fatal("missing candidate owner")
					}
				}
				if counts["consumer"] != size || counts["owner"] != size || counts["candidate_member"] != size || counts["go_member"] != size {
					t.Fatalf("nonlinear candidate work: %v", counts)
				}
				if len(delta.Referrers("go:import:example/lib")) != map[bool]int{true: 1, false: size}[oneOwner] {
					t.Fatal("reference bucket differs")
				}
				delta.Commit()
				clear(counts)
				// Startup ownership selection and a same-owner edit return metadata only.
				for path := range changes {
					for _, owner := range index.Owners(path) {
						if owner.Sources != nil || owner.ContextSources != nil {
							t.Fatal("owner materialized full unit")
						}
					}
				}
				delta = index.Prepare(changes)
				for path := range changes {
					delta.Consumers(path)
					delta.Owners(path)
				}
				if counts["go_member"] != 2*size || counts["candidate_member"] != size || counts["consumer"] != size || counts["owner"] != 2*size {
					t.Fatalf("nonlinear retained owner work: %v", counts)
				}
			})
		}
	}
}

func TestLanguageMembershipBatchesFinalizeEachOwnerOnce(t *testing.T) {
	for _, mode := range []string{"java", "typescript-typed", "typescript-syntax", "rust-fallback"} {
		for _, size := range []int{32, 128} {
			t.Run(fmt.Sprintf("%s/%d", mode, size), func(t *testing.T) {
				root := t.TempDir()
				options := Options{}
				prefix, extension, content := "src/main/java/", ".java", "class A {}"
				switch mode {
				case "java":
					writeDeltaFixture(t, root, "pom.xml", "<project><artifactId>a</artifactId></project>")
				case "typescript-typed":
					options.TypeScriptMode = TypeScriptTyped
					writeDeltaFixture(t, root, "tsconfig.json", "{}")
					prefix, extension, content = "src/", ".ts", "export const a=1;"
				case "typescript-syntax":
					options.TypeScriptMode = TypeScriptSyntax
					writeDeltaFixture(t, root, "package.json", "{}")
					writeDeltaFixture(t, root, "tsconfig.base.json", "{}")
					prefix, extension, content = "src/", ".ts", "export const a=1;"
				case "rust-fallback":
					prefix, extension, content = "loose/", ".rs", "pub fn a() {}"
				}
				plan, err := PlanWorkspace(root, options)
				if err != nil {
					t.Fatal(err)
				}
				counts := map[string]int{}
				plan.Index.work = func(op string) { counts[op]++ }
				ids := map[string]bool{}
				apply := func(remove bool) {
					clear(counts)
					changes := map[string]*Source{}
					for n := 0; n < size; n++ {
						path := fmt.Sprintf("%sf%d%s", prefix, n, extension)
						if remove {
							if err := os.Remove(filepath.Join(root, path)); err != nil {
								t.Fatal(err)
							}
							changes[path] = nil
						} else {
							writeDeltaFixture(t, root, path, content)
							changes[path] = ParseSource(path, []byte(content))
						}
					}
					// Include declarations in the same typed/syntax owner batch.
					if strings.HasPrefix(mode, "typescript") {
						for n := 0; n < size; n++ {
							path := fmt.Sprintf("src/d%d.d.ts", n)
							if remove {
								if err := os.Remove(filepath.Join(root, path)); err != nil {
									t.Fatal(err)
								}
								changes[path] = nil
							} else {
								writeDeltaFixture(t, root, path, "declare const a:number;")
								changes[path] = ParseSource(path, []byte("declare const a:number;"))
							}
						}
					}
					delta := plan.Index.Prepare(changes)
					if counts["membership_finalize"] != 1 {
						t.Fatalf("owner finalized per member: %v", counts)
					}
					if counts["membership_seed"] > 3*size || counts["typescript_owner"] > size || counts["typescript_config_source"] > size {
						t.Fatalf("nonlinear owner/member work: %v", counts)
					}
					for id := range delta.Units {
						ids[id] = true
					}
					delta.Commit()
					compareIncrementalPlan(t, root, plan.Index, ids, options)
				}
				apply(false)
				apply(true)
				apply(false)
			})
		}
	}
}
