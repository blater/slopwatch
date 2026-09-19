package native

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/blater/slopwatch/internal/report"
	"github.com/blater/slopwatch/internal/scoring"
)

func TestSourceDepthResponsibilitySyntaxPublication(t *testing.T) {
	installation := testInstallationRoot(t)
	for _, test := range []struct {
		name       string
		sources    [2]string
		noResource bool
	}{
		{"unused-resource-names", [2]string{
			`public class Example { public static int run(int x,int a,int b,int c) { int open=x; try { return x+a+b+c; } finally { int close=0; } } }`,
			`public class Example { public static int run(int x,int a,int b,int c) { int first=x; try { return x+a+b+c; } finally { int last=0; } } }`,
		}, true},
		{"equivalent-owned-field", [2]string{
			`public class Example { private int value; public int run(int x,int a,int b,int c) { value+=x; return value; } }`,
			`public class Example { private int value; public int run(int x,int a,int b,int c) { this.value+=x; return this.value; } }`,
		}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			var first scoring.MetricValue
			var firstHidden, firstEstimated any
			for i, source := range test.sources {
				workspace := t.TempDir()
				writeTestFile(t, workspace, "Example.java", source)
				analyzer, err := New(workspace, installation, Options{Languages: []string{"java"}})
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
				if err = json.Unmarshal(encoded, &exported); err != nil {
					t.Fatal(err)
				}
				if len(exported.Files) != 1 || len(exported.Depth) != 1 {
					t.Fatalf("missing publication: %+v", exported)
				}
				file := exported.Files[0]
				metric := scoring.Metric(file, "deep")
				if !metric.Available || !scoring.ScoreAvailable(file) {
					t.Fatalf("missing numeric SHALLOW/SCORE: %+v", file)
				}
				for _, boundary := range exported.Depth {
					grade, ok := boundary.Raw["graded"].(map[string]any)
					if !ok {
						t.Fatalf("missing grade: %+v", boundary.Raw)
					}
					if !test.noResource && grade["hidden_responsibility"] != float64(2) {
						t.Fatalf("owned state transition must retain one recognized duty: %+v", grade)
					}
					if test.noResource {
						responsibilities, _ := grade["responsibilities"].(map[string]any)
						if resource, ok := responsibilities["resource"].(float64); ok && resource != 0 {
							t.Fatalf("identifier-only resource credit: %+v", grade)
						}
					}
					if i == 0 {
						first = metric
						firstHidden = grade["hidden_responsibility"]
						firstEstimated = grade["estimated_hidden_responsibility"]
					} else if metric.Value != first.Value || metric.Contribution != first.Contribution || grade["hidden_responsibility"] != firstHidden || grade["estimated_hidden_responsibility"] != firstEstimated {
						t.Fatalf("equivalent syntax changes grade: SHALLOW %+v -> %+v; H %v -> %v; U %v -> %v", first, metric, firstHidden, grade["hidden_responsibility"], firstEstimated, grade["estimated_hidden_responsibility"])
					}
				}
			}
		})
	}
}
