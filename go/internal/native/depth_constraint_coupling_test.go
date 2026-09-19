package native

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/blater/slopwatch/internal/scoring"
	"github.com/blater/slopwatch/internal/sourceestimate"
)

func TestProtocolMetadataDoesNotManufactureCallerCoupling(t *testing.T) {
	source, err := os.ReadFile("protocol.go")
	if err != nil {
		t.Fatal(err)
	}
	result := sourceestimate.Analyze([]sourceestimate.File{{Path: "protocol.go", Language: "go", Source: source}})["protocol.go"]
	if result.Grade == nil || result.Grade.Surface.RepresentationUnits != 0 {
		t.Fatalf("derived metadata invented input obligation: %+v", result.Grade)
	}
	workspace := t.TempDir()
	writeTestFile(t, workspace, "go.mod", "module sample\n\ngo 1.24\n")
	writeTestFile(t, workspace, "protocol.go", string(source))
	analyzer, err := New(workspace, testInstallationRoot(t), Options{Languages: []string{"go"}})
	if err != nil {
		t.Fatal(err)
	}
	doc, err := analyzer.Analyze(context.Background(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	file, ok := fileByPath(doc.Files, "protocol.go")
	if !ok {
		t.Fatal("missing protocol source")
	}
	metric := scoring.Metric(file, "deep")
	if !metric.Available || metric.Value < 0 || metric.Value > 100 {
		t.Fatalf("numeric SHALLOW publication missing: %+v", metric)
	}
}

func TestConstraintDetailsSurviveNativeExport(t *testing.T) {
	source := []byte(`package p;type Example struct{Data []int;Count int};func(e Example)Read()int{return e.Data[e.Count-1]}`)
	result := sourceestimate.Analyze([]sourceestimate.File{{Path: "example.go", Language: "go", Source: source}})["example.go"]
	details := sourceGradeDetails(result.Grade)
	surface := details["surface"].(map[string]any)
	records, ok := surface["constraints"].([]sourceestimate.ConstraintEvidence)
	if !ok || len(records) != 1 || len(records[0].Storage) != 2 || records[0].Path != "example.go" || records[0].IndexExpression != "Count-1" {
		t.Fatalf("constraint provenance dropped: %+v", surface)
	}
}

func TestConstraintIndexExpressionJSON(t *testing.T) {
	for _, expression := range []string{"Count-1", "Count", ""} {
		encoded, err := json.Marshal(sourceestimate.ConstraintEvidence{IndexExpression: expression})
		if err != nil {
			t.Fatal(err)
		}
		var decoded map[string]any
		if err := json.Unmarshal(encoded, &decoded); err != nil {
			t.Fatal(err)
		}
		actual, present := decoded["index_expression"]
		if expression == "" {
			if present {
				t.Fatalf("non-index metadata not omitted: %s", encoded)
			}
		} else if actual != expression {
			t.Fatalf("index expression lost: %s", encoded)
		}
	}
}
