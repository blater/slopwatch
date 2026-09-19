package native

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/blater/slopwatch/internal/report"
)

func TestGoFileDepthAttributionSurvivesExport(t *testing.T) {
	root := testInstallationRoot(t)
	if _, err := os.Stat(analyzerExecutable(root, "go")); err != nil {
		t.Skip("structural Go analyzer is not built")
	}
	workspace := t.TempDir()
	writeTestFile(t, workspace, "go.mod", "module sample\n\ngo 1.24\n")
	writeTestFile(t, workspace, "contract.go", `package sample

type Request struct { Value int }
type Service interface { Run(Request) int }
`)
	writeTestFile(t, workspace, "impl.go", `package sample

func Run(input Request) int { return double(input.Value) }
func double(value int) int { return value * 2 }
`)
	analyzer, err := New(workspace, root, Options{Targets: []string{"."}, Languages: []string{"go"}, ShallowProfile: ShallowProfileResponsibilityV4})
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
	contract, ok := fileByPath(exported.Files, "contract.go")
	if !ok {
		t.Fatal("contract.go missing from report")
	}
	implementation, ok := fileByPath(exported.Files, "impl.go")
	if !ok {
		t.Fatal("impl.go missing from report")
	}
	contractDepth := contract.Components["module_shallowness"]
	if contractDepth.RawMaximum == nil || *contractDepth.RawMaximum != 0 || contractDepth.Contribution != 0 || contractDepth.DepthRole != "declaration-only-contract" || contractDepth.DepthScope != "file" {
		t.Fatalf("contract attribution = %+v", contractDepth)
	}
	implementationDepth := implementation.Components["module_shallowness"]
	if implementationDepth.RawMaximum == nil || *implementationDepth.RawMaximum != 0 || implementationDepth.DepthScope != "file" {
		t.Fatalf("implementation attribution = %+v", implementationDepth)
	}
	fileBoundary := false
	for _, boundary := range exported.Depth {
		if boundary.Scope == "file" && boundary.Shallow != nil && len(boundary.Files) == 1 && boundary.Files[0] == "impl.go" {
			fileBoundary = true
			break
		}
	}
	if !fileBoundary {
		t.Fatalf("file numeric boundary was lost: %+v", exported.Depth)
	}
}

func fileByPath(files []report.File, path string) (report.File, bool) {
	for _, file := range files {
		if file.Path == path {
			return file, true
		}
	}
	return report.File{}, false
}
