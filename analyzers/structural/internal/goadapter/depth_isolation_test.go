package goadapter

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// V4's deliberately restricted importer must never change the fact inputs to
// coupling, cohesion, complexity or any other pre-existing measurement.
func TestDepthProfilePreservesLegacyFacts(t *testing.T) {
	root := t.TempDir()
	source := `package p
import "bytes"
type Service struct{}
func (Service) Run() int { b:=bytes.NewBuffer(nil); return b.Len() }
func Add(x int) int {return x+1}
`
	if err := os.WriteFile(filepath.Join(root, "p.go"), []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	legacy, err := Analyze(root, []string{"p.go"})
	if err != nil {
		t.Fatal(err)
	}
	current, err := (Adapter{}).Analyze(root, []string{"p.go"}, map[string]any{"depth_profile": "responsibility-v4"})
	if err != nil {
		t.Fatal(err)
	}
	if current.Depth == nil {
		t.Fatal("missing opt-in facts")
	}
	current.Depth = nil
	if !reflect.DeepEqual(legacy, current) {
		t.Fatalf("SHALLOW profile changed legacy facts:\nlegacy %+v\ncurrent %+v", legacy.Types, current.Types)
	}
}
