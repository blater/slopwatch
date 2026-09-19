package goadapter

import (
	"os"
	"path/filepath"
	"testing"

	"slopslap.dev/structural/internal/facts"
)

func TestGoPassiveEnumProvesTypedIotaPackage(t *testing.T) {
	program := analyzeGoPassiveEnum(t, `package status
type Code int
const (
	Unknown Code = iota
	Ready
	Failed
)`)
	if program.Depth == nil || len(program.Depth.Boundaries) != 1 {
		t.Fatalf("depth = %#v", program.Depth)
	}
	for _, evidence := range program.Depth.Boundaries[0].Evidence {
		if evidence.Kind == "passive-enum-v1" && evidence.Status == "proven" {
			return
		}
	}
	t.Fatalf("passive enum proof missing: %#v", program.Depth.Boundaries[0])
}

func TestGoPassiveEnumRejectsExecutablePackage(t *testing.T) {
	program := analyzeGoPassiveEnum(t, `package status
type Code int
const Ready Code = iota
func Cost(code Code) int { return int(code) + 1 }`)
	if program.Depth == nil || len(program.Depth.Boundaries) != 1 {
		t.Fatalf("depth = %#v", program.Depth)
	}
	for _, evidence := range program.Depth.Boundaries[0].Evidence {
		if evidence.Kind == "passive-enum-v1" && evidence.Status == "proven" {
			t.Fatalf("executable enum package was exempted: %#v", program.Depth.Boundaries[0])
		}
	}
}

func analyzeGoPassiveEnum(t *testing.T, source string) *facts.Program {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "status.go"), []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	program, err := (Adapter{}).Analyze(root, []string{"status.go"}, map[string]any{"depth_profile": "responsibility-v4"})
	if err != nil {
		t.Fatal(err)
	}
	return program
}
