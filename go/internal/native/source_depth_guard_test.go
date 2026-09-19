package native

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/blater/slopwatch/internal/report"
	"github.com/blater/slopwatch/internal/scoring"
)

// Exercise the actual language adapters, source fallback and exported report: a
// useful guard must not make the same unresolved conversion look shallower.
func TestSourceDepthGuardPreservesReturnedDutyPublication(t *testing.T) {
	root := testInstallationRoot(t)
	for _, fixture := range []struct {
		language, path, source, guard string
		guardHidden                   float64
	}{
		{"java", "Example.java", `public class Example { public String convert(String x,int a,int b,int c){GUARD return External.convert(x,a,b,c);}}`, `if(x == null) return "";`, 1},
		{"typescript", "example.ts", `export function convert(x:string,a:number,b:number,c:number):string {GUARD return external.convert(x,a,b,c);}`, `if(x == null) return "";`, 1},
		{"go", "example.go", "package example\nfunc Convert(x string,a,b,c int)string {GUARD return external.Convert(x,a,b,c)}", `if x == "" {return ""};`, 1},
		{"rust", "lib.rs", `pub fn convert(x:i32,a:i32,b:i32,c:i32)->i32 {GUARD external::convert(x,a,b,c)}`, `if x == 0 {return 0;}`, 0},
	} {
		t.Run(fixture.language, func(t *testing.T) {
			var baseline scoring.MetricValue
			for _, test := range []struct {
				name, guard       string
				hidden, estimated float64
			}{
				{"bare", "", 0, 3},
				{"guarded", fixture.guard, fixture.guardHidden, 3 - fixture.guardHidden},
			} {
				t.Run(test.name, func(t *testing.T) {
					workspace := t.TempDir()
					writeTestFile(t, workspace, fixture.path, strings.ReplaceAll(fixture.source, "GUARD", test.guard))
					if fixture.language == "go" {
						writeTestFile(t, workspace, "go.mod", "module example.local\n\ngo 1.20\n")
					}
					analyzer, err := New(workspace, root, Options{Languages: []string{fixture.language}})
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
					if len(exported.Files) != 1 || len(exported.Depth) != 1 {
						t.Fatalf("missing file or evidence: %+v", exported)
					}
					file := exported.Files[0]
					metric := scoring.Metric(file, "deep")
					if !metric.Available || metric.Value != 10 || !scoring.ScoreAvailable(file) {
						t.Fatalf("expected published numeric SHALLOW 10 and available SCORE: %+v", file)
					}
					if file.Complete || !file.Components["module_shallowness"].DepthEstimated {
						t.Fatal("unresolved call lost incomplete/estimated evidence status")
					}
					if test.name == "bare" {
						baseline = metric
					} else if metric.Value > baseline.Value || metric.Contribution > baseline.Contribution {
						t.Fatalf("guard increased SHALLOW/SCORE contribution: %+v -> %+v", baseline, metric)
					}
					for _, boundary := range exported.Depth {
						grade, ok := boundary.Raw["graded"].(map[string]any)
						if !ok || grade["hidden_responsibility"] != test.hidden || grade["estimated_hidden_responsibility"] != test.estimated {
							t.Fatalf("incorrect observed/estimated category replacement: %+v", boundary.Raw)
						}
						limits, ok := grade["material_limitations"].([]any)
						if !ok || len(limits) == 0 {
							t.Fatal("returned unknown implementation lost limitations")
						}
					}
				})
			}
		})
	}
}
