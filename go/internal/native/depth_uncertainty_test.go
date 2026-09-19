package native

import (
	"encoding/json"
	"github.com/blater/slopwatch/internal/report"
	"github.com/blater/slopwatch/internal/scoring"
	"github.com/blater/slopwatch/internal/sourceestimate"
	"os"
	"path/filepath"
	"testing"
)

func TestMaterialUncertaintyPublishesSupportedPenalty(t *testing.T) {
	for _, test := range []struct {
		name      string
		low, high float64
	}{
		{"QueryResultRow.java", 15, 45},
		{"KeyedPath.java", 0, 20},
		{"workspace.go.txt", 0, 25},
	} {
		name := test.name
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("..", "sourceestimate", "testdata", "uncertainty", name))
			if err != nil {
				t.Fatal(err)
			}
			lang := "java"
			if name == "workspace.go.txt" {
				lang = "go"
			}
			_, estimates := sourceestimate.AnalyzeWithAttribution([]sourceestimate.File{{Path: name, Language: lang, Source: data}})
			result := estimates[name]
			if !result.Applicable || len(result.Limitations) == 0 {
				t.Fatalf("lost behavioral uncertainty: %+v", result)
			}
			boundary := estimateDepthBoundary(report.DepthBoundary{ID: "fixture", State: "partial"}, result)
			raw := boundary.Raw["estimate"].(map[string]any)
			if boundary.Shallow == nil || *boundary.Shallow < test.low || *boundary.Shallow > test.high || !boundary.Estimated || raw["penalty_support"] != "graded-caller-responsibility" || raw["descriptive_ratio"].(float64) <= 0 {
				t.Fatalf("graded uncertainty estimate outside reviewed range or missing evidence: %+v", boundary)
			}
			if raw["model"] != "graded-caller-responsibility-v1" {
				t.Fatalf("unexpected estimate model: %+v", raw)
			}
			if boundary.Shallow != nil && *boundary.Shallow == 0 {
				zeroReason, _ := boundary.Raw["zero_reason"].(string)
				if zeroReason != "recognized_role" && zeroReason != "lowest_range_supported" && zeroReason != "conservative_uncertainty" {
					t.Fatalf("zero estimate lacks an explicit category: %+v", boundary)
				}
			}
			component, err := scoreDepthComponent(depthDescriptor(), name, map[string]report.DepthBoundary{"fixture": boundary}, []string{"fixture"}, "partial", name)
			if err != nil {
				t.Fatal(err)
			}
			encoded, err := json.Marshal(component)
			if err != nil {
				t.Fatal(err)
			}
			var exported report.Component
			if err = json.Unmarshal(encoded, &exported); err != nil {
				t.Fatal(err)
			}
			metric := scoring.Metric(report.File{Components: map[string]report.Component{"module_shallowness": exported}}, "deep")
			if !metric.Available || metric.Value != *boundary.Shallow || !exported.DepthEstimated {
				t.Fatalf("numeric export/SCORE path: %+v %+v", metric, exported)
			}
			if *boundary.Shallow > 0 && metric.Contribution <= 0 {
				t.Fatalf("positive graded estimate lost SCORE contribution: %+v %+v", metric, exported)
			}
		})
	}
}
