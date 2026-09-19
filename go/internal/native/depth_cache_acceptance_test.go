package native

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/blater/slopwatch/internal/analysiscache"
	"github.com/blater/slopwatch/internal/report"
)

func TestDepthProfileSourceCacheAndEditAcceptance(t *testing.T) {
	root := testInstallationRoot(t)
	if _, err := os.Stat(analyzerExecutable(root, "go")); err != nil {
		t.Skip("structural analyzer not built")
	}
	workspace := t.TempDir()
	writeTestFile(t, workspace, "go.mod", "module sample\n\ngo 1.24\n")
	writeTestFile(t, workspace, "service.go", "package sample\nfunc Run(x int) int { return x + 1 }\n")
	analyzer, err := New(workspace, root, Options{Targets: []string{"."}, Languages: []string{"go"}, ShallowProfile: ShallowProfileResponsibilityV4, ReadCache: true})
	if err != nil {
		t.Fatal(err)
	}
	store, err := analysiscache.NewStore(filepath.Join(t.TempDir(), "cache"))
	if err != nil {
		t.Fatal(err)
	}
	analyzer.SetCacheStore(store)
	analyze := func(wantRatio, wantHidden float64) report.Document {
		t.Helper()
		document, err := analyzer.Analyze(context.Background(), []string{"."}, []string{"go"})
		if err != nil {
			t.Fatal(err)
		}
		if len(document.Files) != 1 {
			t.Fatalf("files = %d", len(document.Files))
		}
		component := document.Files[0].Components["module_shallowness"]
		if component.DepthState != "measured" || component.RawMaximum == nil || *component.RawMaximum != 0 || len(document.Depth) != 1 {
			t.Fatalf("SHALLOW want numeric zero, got component=%+v ledger=%+v", component, document.Depth)
		}
		for _, boundary := range document.Depth {
			if boundary.Raw["responsibility_ratio"] != wantRatio || boundary.Raw["penalty_basis"] != "graded-caller-responsibility" {
				t.Fatalf("lost graded descriptive ratio or penalty basis: %+v", boundary)
			}
			grade, ok := boundary.Raw["graded"].(map[string]any)
			if !ok || grade["hidden_responsibility"] != wantHidden || grade["zero_reason"] != "lowest_range_supported" {
				t.Fatalf("lost graded measurement evidence: %+v", boundary)
			}
		}
		return document
	}
	cold := analyze(30, 2)
	// A warm read must succeed without invoking the analyzer at all.
	runner := analyzer.runUnits
	analyzer.runUnits = func(context.Context, string, analyzerRequest) (map[string]scoreInputs, error) {
		t.Fatal("warm depth analysis invoked a backend")
		return nil, nil
	}
	warm := analyze(30, 2)
	coldLedger, err := json.Marshal(cold.Depth)
	if err != nil {
		t.Fatal(err)
	}
	warmLedger, err := json.Marshal(warm.Depth)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(coldLedger, warmLedger) {
		t.Fatal("warm cache changed the boundary ledger")
	}
	analyzer.runUnits = runner
	writeTestFile(t, workspace, "service.go", "package sample\nfunc Run(x int) int { return x }\n")
	analyze(100, 0)
}
