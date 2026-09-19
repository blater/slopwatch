package native

import (
	"context"
	"github.com/blater/slopwatch/internal/report"
	"testing"
)

func TestSupportingErrorMovePreservesWorkspaceRating(t *testing.T) {
	const behavior = `package sample
 import "os"
 func newWorkspace(path string) (*os.File,error) {
  if path == "" { return nil, &problem{message:"empty"} }
  root,err := os.Open(path)
  if err != nil { return nil,err }
  if err := configure(path); err != nil { root.Close(); return nil,err }
  return root,nil
 }
 func configure(path string) error {
  if path == "." { return &problem{message:"bad"} }; return nil
 }
 `
	const representation = `type problem struct { message string }; func (p *problem) Error() string { return p.message }`
	root := testInstallationRoot(t)
	var baseline *report.Component
	for _, moved := range []bool{false, true} {
		workspace := t.TempDir()
		writeTestFile(t, workspace, "go.mod", "module sample\n\ngo 1.24\n")
		source := behavior
		if moved {
			writeTestFile(t, workspace, "problem.go", "package sample; "+representation)
		} else {
			source += representation
		}
		writeTestFile(t, workspace, "workspace.go", source)
		analyzer, err := New(workspace, root, Options{Languages: []string{"go"}})
		if err != nil {
			t.Fatal(err)
		}
		doc, err := analyzer.Analyze(context.Background(), nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		file, ok := fileByPath(doc.Files, "workspace.go")
		if !ok {
			t.Fatal("missing workspace")
		}
		component := file.Components["module_shallowness"]
		if component.RawMaximum == nil || *component.RawMaximum >= 100 {
			t.Fatalf("unrecognized behavior: %+v", component)
		}
		if baseline == nil {
			baseline = &component
		} else if *baseline.RawMaximum != *component.RawMaximum || baseline.Contribution != component.Contribution {
			t.Fatalf("move changed rating: %+v -> %+v", *baseline, component)
		}
	}
}
