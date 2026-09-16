package native

import (
	"fmt"
	"testing"
)

func TestFortyFiveThousandZeroScoreJavaFilesRemainInReport(t *testing.T) {
	const count = 45_000
	inputs := newScoreInputs()
	for index := 0; index < count; index++ {
		path := fmt.Sprintf("src/main/java/example/Class%05d.java", index)
		inputs.coverage[path] = map[string]string{}
		inputs.languages[path] = "java"
	}
	document, err := scoreInputsReport(catalogDocument{}, []string{"java"}, inputs, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(document.Files) != count || document.ReturnedFiles != count {
		t.Fatalf("zero-score Java rows = %d/%d, want %d", len(document.Files), document.ReturnedFiles, count)
	}
	for _, file := range document.Files {
		if file.Score != 0 || !file.ValidZero {
			t.Fatalf("zero-score file was changed: %#v", file)
		}
	}
}

func TestFailedCoverageRemainsIncompleteAndInvalidZero(t *testing.T) {
	inputs := newScoreInputs()
	inputs.coverage["broken.go"] = map[string]string{"cognitive_complexity": "failed"}
	inputs.languages["broken.go"] = "go"
	inputs.diagnostics = []map[string]any{{"path": "broken.go", "code": "SYNTAX_ERROR", "message": "broken.go:1:14: syntax error"}}
	descriptors := []componentDescriptor{{ID: "cognitive_complexity", Version: "pmd-sonar-v1", Axis: "structural_core", Support: map[string]string{"go": "supported"}, Defaults: componentDefaults{Enabled: true, Weight: "1", Formula: "count"}}}
	passScore := 10.0
	document, err := scoreInputsReport(catalogDocument{Components: descriptors}, []string{"go"}, inputs, &passScore)
	if err != nil {
		t.Fatal(err)
	}
	if len(document.Files) != 1 || document.Files[0].Complete || document.Files[0].ValidZero {
		t.Fatalf("failed syntax file was presented as complete/valid zero: %#v", document.Files)
	}
	if document.Files[0].Passed == nil || *document.Files[0].Passed {
		t.Fatalf("failed syntax file passed threshold: %#v", document.Files[0].Passed)
	}
	if len(document.Diagnostics) != 1 || document.Diagnostics[0]["code"] != "SYNTAX_ERROR" {
		t.Fatalf("syntax diagnostic was lost: %#v", document.Diagnostics)
	}
}
