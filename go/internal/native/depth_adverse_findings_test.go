package native

import (
	"context"
	"github.com/blater/slopwatch/internal/scoring"
	"testing"
)

func TestShallowReportsCallerCostFindingNotTriviality(t *testing.T) {
	root := testInstallationRoot(t)
	for _, tc := range []struct {
		name, implementation, caller, extra string
		finding                             bool
	}{
		{"discarded computation", `return value*2`, `return calculate(x,x*x,x+1)`, "", true},
		{"supporting type beside caller", `return value*2`, `return calculate(x,x*x,x+1)`, `type fault struct{};func(fault)Error()string{return ""}`, true},
		{"helper extraction", `return twice(value)`, `return calculate(x,x*x,x+1)`, `func twice(value int)int{return value*2}`, true},
		{"unrelated sibling behavior", `return value*2`, `return calculate(x,x*x,x+1)`, `func Unrelated(x int)int{return x*3}`, true},
		{"useful computation", `return value*extra*other`, `return calculate(x,x*x,x+1)`, "", false},
		{"literal compatibility input", `return value*2`, `return calculate(x,0,0)`, "", false},
		{"escaping contract", `return value*2`, `return calculate(x,x*x,x+1)`, `var callback = calculate`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			workspace := t.TempDir()
			writeTestFile(t, workspace, "go.mod", "module sample\n\ngo 1.24\n")
			writeTestFile(t, workspace, "service.go", `package sample;func calculate(value,extra,other int)int{`+tc.implementation+`}`)
			writeTestFile(t, workspace, "caller.go", `package sample;func Run(x int)int{`+tc.caller+`};`+tc.extra)
			analyzer, err := New(workspace, root, Options{Languages: []string{"go"}})
			if err != nil {
				t.Fatal(err)
			}
			doc, err := analyzer.Analyze(context.Background(), nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			service, ok := fileByPath(doc.Files, "service.go")
			if !ok {
				t.Fatal("service missing")
			}
			metric := scoring.Metric(service, "deep")
			// The graded fallback publishes a bounded numeric rating from caller
			// surface even when no adverse finding is proven. Findings remain
			// evidence, rather than the gate that manufactures the rating.
			if !metric.Available || metric.Value <= 0 || metric.Value > 100 || metric.State != "partial" {
				t.Fatalf("graded numeric estimate finding=%v metric=%+v", tc.finding, metric)
			}
			if tc.finding {
				found := false
				for _, id := range service.Components["module_shallowness"].DepthBoundaryIDs {
					raw := doc.Depth[id].Raw
					estimate, _ := raw["estimate"].(map[string]any)
					findings, _ := estimate["findings"].([]any)
					found = found || len(findings) > 0
				}
				if !found {
					t.Fatal("positive penalty lacks finding evidence")
				}
			} else {
				for _, id := range service.Components["module_shallowness"].DepthBoundaryIDs {
					raw := doc.Depth[id].Raw
					estimate, _ := raw["estimate"].(map[string]any)
					findings, _ := estimate["findings"].([]any)
					if len(findings) != 0 {
						t.Fatalf("non-adverse case gained unsupported finding evidence: %v", findings)
					}
				}
			}
			caller, _ := fileByPath(doc.Files, "caller.go")
			if scoring.Metric(caller, "deep").Value != 0 {
				t.Fatal("service defect replicated onto caller")
			}
		})
	}
}
