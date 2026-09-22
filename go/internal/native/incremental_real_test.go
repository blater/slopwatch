package native

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"github.com/blater/slopwatch/internal/report"
)

func TestIncrementalRealBackendsMatchIndependentStartupPolicy(t *testing.T) {
	cases := []struct {
		language, config, path, before, after string
		files                                 map[string]string
	}{
		{language: "go", config: "go.mod", path: "p/a.go", before: "package p\nfunc Value(x int) int { return x+1 }\n", after: "package p\nfunc Value(x int) int { if x<0 { return 0 }; return x+2 }\n", files: map[string]string{"go.mod": "module example\n\ngo 1.24\n", "p/b.go": "package p\nfunc Other() int { return 3 }\n"}},
		{language: "java", config: "pom.xml", path: "src/main/java/A.java", before: "class A { public int value(int x) { return x+1; } }", after: "class A { public int value(int x) { if (x<0) return 0; return x+2; } }", files: map[string]string{"pom.xml": "<project><groupId>x</groupId><artifactId>app</artifactId></project>", "src/main/java/B.java": "class B { public int other() { return 3; } }"}},
		{language: "typescript", config: "tsconfig.json", path: "src/a.ts", before: "export function value(x:number):number { return x+1; }", after: "export function value(x:number):number { if(x<0) return 0; return x+2; }", files: map[string]string{"tsconfig.json": "{\"include\":[\"src/*.ts\"]}", "src/b.ts": "export function other():number { return 3; }"}},
		{language: "rust", config: "Cargo.toml", path: "src/lib.rs", before: "pub fn value(x:i32)->i32 { x+1 }", after: "pub fn value(x:i32)->i32 { if x<0 { return 0; } x+2 }", files: map[string]string{"Cargo.toml": "[package]\nname=\"app\"\nversion=\"0.1.0\"\n", "src/other.rs": "pub fn other()->i32 { 3 }"}},
	}
	for _, test := range cases {
		for _, cached := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/cache=%t", test.language, cached), func(t *testing.T) {
				root := t.TempDir()
				for path, data := range test.files {
					writeTestFile(t, root, path, data)
				}
				writeTestFile(t, root, test.path, test.before)
				options := Options{Languages: []string{test.language}, ReadCache: cached, TypeScriptTypes: true}
				analyzer, err := New(root, testInstallationRoot(t), options)
				if err != nil {
					t.Fatal(err)
				}
				if cached {
					analyzer.EnableCache(filepath.Join(t.TempDir(), "cache"))
				}
				initial, err := analyzer.Analyze(context.Background(), nil, nil)
				if err != nil {
					t.Fatal(err)
				}
				writeTestFile(t, root, test.config, "invalid live configuration after startup")
				writeTestFile(t, root, test.path, test.after)
				delta, replaced, err := analyzer.AnalyzeChanges(context.Background(), []string{test.path, test.config})
				if err != nil {
					t.Fatal(err)
				}
				got := map[string]report.File{}
				for _, file := range initial.Files {
					got[file.Path] = file
				}
				for _, path := range replaced {
					delete(got, path)
				}
				for _, file := range delta.Files {
					got[file.Path] = file
				}
				referenceRoot := t.TempDir()
				for path, data := range test.files {
					writeTestFile(t, referenceRoot, path, data)
				}
				writeTestFile(t, referenceRoot, test.path, test.after)
				reference, err := New(referenceRoot, testInstallationRoot(t), options)
				if err != nil {
					t.Fatal(err)
				}
				if cached {
					reference.EnableCache(filepath.Join(t.TempDir(), "reference-cache"))
				}
				want, err := reference.Analyze(context.Background(), nil, nil)
				if err != nil {
					t.Fatal(err)
				}
				if len(got) != len(want.Files) {
					t.Fatalf("inventory differs got=%d want=%d", len(got), len(want.Files))
				}
				for _, file := range want.Files {
					actual, ok := got[file.Path]
					actual.Rank, file.Rank = 0, 0
					actual.Freshness, file.Freshness = "", ""
					actual.FreshnessNote, file.FreshnessNote = "", ""
					// JSON normalizes numeric map values from persisted artifacts;
					// all substantive rating/evidence/estimation fields remain.
					left, _ := json.Marshal(actual)
					right, _ := json.Marshal(file)
					if !ok || string(left) != string(right) {
						t.Fatalf("%s substantive report mismatch\ngot %s\nwant %s", file.Path, left, right)
					}
				}
				if !reflect.DeepEqual(semanticDiagnostics(delta.Diagnostics), semanticDiagnostics(want.Diagnostics)) {
					t.Fatalf("diagnostics differ: got=%#v want=%#v", delta.Diagnostics, want.Diagnostics)
				}
				left, _ := json.Marshal(delta.Depth)
				right, _ := json.Marshal(want.Depth)
				if string(left) != string(right) {
					t.Fatalf("depth evidence differs: got=%s want=%s", left, right)
				}
			})
		}
	}
}

// Invocation tokens and coordinator-specific unit IDs identify requests, not
// diagnostic meaning. Keep codes, severity, source paths and every other field.
func semanticDiagnostics(input []map[string]any) []string {
	result := []string{}
	for _, diagnostic := range input {
		copy := map[string]any{}
		for key, value := range diagnostic {
			if key != "invocation_id" && key != "unit_id" {
				copy[key] = value
			}
		}
		data, _ := json.Marshal(copy)
		result = append(result, string(data))
	}
	sort.Strings(result)
	return result
}
