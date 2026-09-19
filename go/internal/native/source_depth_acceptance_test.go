package native

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/blater/slopwatch/internal/report"
	"github.com/blater/slopwatch/internal/scoring"
	"github.com/blater/slopwatch/internal/sourceestimate"
)

// These exercise packaged adapters, recovery, the source fallback, SCORE and
// export together. Presentation uses the same scoring.Metric projection.
func TestSourceDepthNumericCoverageWithMissingSemanticInputs(t *testing.T) {
	root := testInstallationRoot(t)
	for _, test := range []struct {
		name, language, path string
		files                map[string]string
	}{
		{"go missing dependency", "go", "service.go", map[string]string{
			"go.mod":     "module sample\n\ngo 1.24\n",
			"service.go": "package sample\nimport missing \"example.invalid/notinstalled\"\nfunc Run(x missing.Value) missing.Value { return x }\n",
		}},
		{"java missing type", "java", "Service.java", map[string]string{
			"Service.java": "public class Service { public Missing run(Missing x) { return x; } }",
		}},
		{"typescript project references", "typescript", "src/service.ts", map[string]string{
			"tsconfig.json":     `{"files":[],"references":[{"path":"./missing-project"},{"path":"./src"}]}`,
			"src/tsconfig.json": `{"compilerOptions":{"composite":true},"include":["*.ts"]}`,
			"src/service.ts":    "import { Missing } from 'not-installed'; export class Service { run(x: Missing): Missing { return x; } }",
		}},
		{"rust unresolved module", "rust", "lib.rs", map[string]string{
			"lib.rs": "mod missing; pub fn run(x: missing::Value) -> missing::Value { x }",
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := os.Stat(analyzerExecutable(root, test.language)); err != nil {
				t.Fatal(err)
			}
			workspace := t.TempDir()
			for path, source := range test.files {
				writeTestFile(t, workspace, path, source)
			}
			analyzer, err := New(workspace, root, Options{Languages: []string{test.language}, TypeScriptTypes: true})
			if err != nil {
				t.Fatal(err)
			}
			document, err := analyzer.Analyze(context.Background(), nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			encoded, err := json.Marshal(document)
			if err != nil {
				t.Fatal(err)
			}
			var exported report.Document
			if err := json.Unmarshal(encoded, &exported); err != nil {
				t.Fatal(err)
			}
			found := false
			for _, file := range exported.Files {
				if file.Path != test.path {
					continue
				}
				found = true
				metric := scoring.Metric(file, "deep")
				if !metric.Available || metric.Value != 0 || metric.Contribution != 0 {
					t.Fatalf("identity service must have numeric no-finding SHALLOW: metric=%+v file=%+v", metric, file)
				}
				if !file.Components["module_shallowness"].DepthEstimated {
					t.Fatal("missing semantic inputs lost estimation metadata")
				}
				if file.Complete {
					t.Fatal("estimated rating must not authorize complete-evidence automated fixes")
				}
			}
			if !found {
				t.Fatalf("source omitted: %s", test.path)
			}
		})
	}
}

func TestSourceRecoveryPreservesProvenNonApplicability(t *testing.T) {
	if needsSourceDepth(report.DepthBoundary{State: "not_applicable", Estimated: true}) {
		t.Fatal("source recovery must not undo an independent supporting-role proof")
	}
}

func TestSourceRecoveryProjectsSharedBoundaryPerFile(t *testing.T) {
	workspace := t.TempDir()
	writeTestFile(t, workspace, "a.go", "package sample; func Twice(x Missing) Missing { return x * 2 }")
	writeTestFile(t, workspace, "b.go", "package sample; func Thrice(x Missing) Missing { return x * 3 }")
	inputs := newScoreInputs()
	inputs.depth["package"] = report.DepthBoundary{ID: "package", State: "partial", Raw: map[string]any{"B8": 20.0}}
	inputs.depthByPath["a.go"], inputs.depthByPath["b.go"] = []string{"package"}, []string{"package"}
	request := analyzerRequest{Workspace: workspace, Components: []requestedComponent{{ID: "module_shallowness", Version: ShallowDefinitionV4}}, Units: []protocolUnit{{Language: "go", Paths: []string{"a.go", "b.go"}}}}
	result, err := withSourceDepthEstimates(context.Background(), request, inputs)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"a.go", "b.go"} {
		ids := result.depthByPath[path]
		if len(ids) != 1 {
			t.Fatalf("unexpected projection for %s: %v", path, ids)
		}
		boundary := result.depth[ids[0]]
		if boundary.Scope != "file" || boundary.Raw["H"] != 2.0 || boundary.Raw["B8"] != 10.0 || boundary.Shallow == nil {
			t.Fatalf("file attribution lost: %+v", boundary)
		}
	}
}

func TestPassiveResultCarrierZeroSurvivesExportAndScore(t *testing.T) {
	workspace := t.TempDir()
	writeTestFile(t, workspace, "Payload.java", `public final class Payload {
  private Object value; private int status;
  public Object value() { return value; }
  public int status() { return status; }
  public void set(Object value, int status) { this.value = value; this.status = status; }
  public void reset() { value = null; status = 0; }
}`)
	analyzer, err := New(workspace, testInstallationRoot(t), Options{Languages: []string{"java"}})
	if err != nil {
		t.Fatal(err)
	}
	document, err := analyzer.Analyze(context.Background(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	var exported report.Document
	if err := json.Unmarshal(payload, &exported); err != nil {
		t.Fatal(err)
	}
	if len(exported.Files) != 1 {
		t.Fatalf("carrier report: %d files", len(exported.Files))
	}
	metric := scoring.Metric(exported.Files[0], "deep")
	if !metric.Available || metric.Value != 0 || metric.Contribution != 0 {
		t.Fatalf("carrier penalty: %+v", metric)
	}
	proven := false
	for _, boundary := range exported.Depth {
		for _, raw := range boundary.Evidence {
			if evidence, ok := raw.(map[string]any); ok && (evidence["kind"] == "passive-result-carrier-v1" || evidence["kind"] == "supporting-java-data-type") && evidence["status"] == "proven" {
				proven = true
			}
		}
	}
	if !proven {
		t.Fatalf("numeric exemption lost its structural role proof: %+v", exported.Depth)
	}
}

func TestSourceRecognitionFailureIsNotNonApplicability(t *testing.T) {
	boundary := report.DepthBoundary{ID: "source:typescript:run.ts", State: "partial"}
	unknown := estimateDepthBoundary(boundary, sourceestimate.Result{})
	if unknown.State == "not_applicable" || unknown.Shallow == nil || !unknown.Estimated {
		t.Fatalf("recognition failure hidden: %+v", unknown)
	}
	absent := estimateDepthBoundary(boundary, sourceestimate.Result{NoAbstractionProven: true})
	if absent.State != "not_applicable" || absent.Shallow != nil {
		t.Fatalf("absence proof lost: %+v", absent)
	}
}

func TestTypeScriptProjectReferenceArrowSyntaxHasEquivalentRatings(t *testing.T) {
	var first float64
	for index, source := range []string{
		"export const run = x => x * 2;",
		"export const run = (x: number) => x * 2;",
		"export const run = (x: number) => { return x * 2; };",
		"export const run = x => { return x * 2; };",
	} {
		workspace := t.TempDir()
		writeTestFile(t, workspace, "tsconfig.json", `{"files":[],"references":[{"path":"./missing-project"},{"path":"./src"}]}`)
		writeTestFile(t, workspace, "src/tsconfig.json", `{"compilerOptions":{"composite":true},"include":["*.ts"]}`)
		writeTestFile(t, workspace, "src/run.ts", source)
		writeTestFile(t, workspace, "other/tsconfig.json", `{}`)
		writeTestFile(t, workspace, "other/helper.ts", "export const other = (x: number) => x;")
		analyzer, err := New(workspace, testInstallationRoot(t), Options{Languages: []string{"typescript"}, TypeScriptTypes: true})
		if err != nil {
			t.Fatal(err)
		}
		document, err := analyzer.Analyze(context.Background(), nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		payload, err := json.Marshal(document)
		if err != nil {
			t.Fatal(err)
		}
		var exported report.Document
		if err = json.Unmarshal(payload, &exported); err != nil {
			t.Fatal(err)
		}
		found := false
		for _, file := range exported.Files {
			if file.Path == "src/run.ts" {
				found = true
				metric := scoring.Metric(file, "deep")
				if !metric.Available || metric.Value != 0 || metric.Contribution != 0 {
					t.Fatalf("%s: %+v", source, metric)
				}
				if index == 0 {
					first = metric.Value
				} else if metric.Value != first {
					t.Fatalf("syntax changed rating: first=%v %s=%v", first, source, metric.Value)
				}
			}
		}
		if !found {
			t.Fatal("missing run.ts")
		}
	}
}
