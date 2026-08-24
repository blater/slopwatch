package native

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/blater/slopwatch/internal/report"
)

func TestActiveCatalogMakesTypeScriptTypesOptInWithoutMutatingSource(t *testing.T) {
	catalog := catalogDocument{Components: []componentDescriptor{
		{ID: "cognitive_complexity", Axis: "structural_core", Defaults: componentDefaults{Enabled: true}},
		{ID: "explicit_any", Axis: "typescript_type_safety", Defaults: componentDefaults{Enabled: true}},
	}}
	fast := activeCatalog(catalog, Options{})
	if !fast.Components[0].Defaults.Enabled || fast.Components[1].Defaults.Enabled {
		t.Fatalf("fast catalog components = %#v", fast.Components)
	}
	if !catalog.Components[1].Defaults.Enabled {
		t.Fatal("activeCatalog mutated the loaded catalog")
	}
	typed := activeCatalog(catalog, Options{TypeScriptTypes: true})
	if !typed.Components[1].Defaults.Enabled {
		t.Fatal("TypeScript type analysis was not enabled explicitly")
	}
}

func TestDiscoveryFollowsExplicitSymlinkTargetAndControlsNestedSymlinks(t *testing.T) {
	workspace := t.TempDir()
	project := t.TempDir()
	nested := t.TempDir()
	if err := os.WriteFile(filepath.Join(project, "Main.java"), []byte("class Main {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, ".DS_Store"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nested, "Nested.java"), []byte("class Nested {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(nested, filepath.Join(project, "nested")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := os.Symlink(project, filepath.Join(project, "cycle")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(project, filepath.Join(workspace, "erBuilder")); err != nil {
		t.Fatal(err)
	}

	analyzer := &Analyzer{workspace: workspace}
	discovered, err := analyzer.discover([]string{"erBuilder"}, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(discovered["java"], ","); got != "erBuilder/Main.java" {
		t.Fatalf("default discovery = %q, want only the explicit symlink target's direct source", got)
	}

	discovered, err = analyzer.discover([]string{"erBuilder"}, false, true)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(discovered["java"], ","); got != "erBuilder/Main.java,erBuilder/nested/Nested.java" {
		t.Fatalf("follow-symlinks discovery = %q", got)
	}
}

func TestDocumentInventoryRejectsPartialAnalyzerCoverage(t *testing.T) {
	discovered := map[string][]string{"java": {"a.java", "b.java", "c.java"}}
	partial := report.Document{Files: []report.File{{Path: "a.java"}, {Path: "c.java"}}}
	if err := validateDocumentInventory(partial, discovered, []string{"java"}); err == nil || !strings.Contains(err.Error(), "2 of 3") {
		t.Fatalf("partial inventory error = %v", err)
	}
	complete := report.Document{Files: []report.File{{Path: "a.java"}, {Path: "b.java"}, {Path: "c.java"}}}
	if err := validateDocumentInventory(complete, discovered, []string{"java"}); err != nil {
		t.Fatalf("complete inventory rejected: %v", err)
	}
}

func BenchmarkDiscoverFortyFiveThousandJavaFiles(b *testing.B) {
	const sourceCount = 45_000
	workspace := b.TempDir()
	for module := 0; module < 45; module++ {
		directory := filepath.Join(workspace, fmt.Sprintf("module-%02d/src/main/java", module))
		if err := os.MkdirAll(directory, 0o755); err != nil {
			b.Fatal(err)
		}
		for file := 0; file < sourceCount/45; file++ {
			path := filepath.Join(directory, fmt.Sprintf("Class%04d.java", file))
			if err := os.WriteFile(path, nil, 0o600); err != nil {
				b.Fatal(err)
			}
		}
	}
	analyzer := &Analyzer{workspace: workspace}
	b.ResetTimer()
	for range b.N {
		discovered, err := analyzer.discover([]string{"."}, false, false)
		if err != nil {
			b.Fatal(err)
		}
		if len(discovered["java"]) != sourceCount {
			b.Fatalf("discovered %d Java files, want %d", len(discovered["java"]), sourceCount)
		}
	}
}

func TestDecodeUnitScoreInputsKeepsOverlappingPathsIsolated(t *testing.T) {
	request := analyzerRequest{
		Type: "request", Version: 1, Invocation: "invocation",
		Units: []protocolUnit{{ID: "first"}, {ID: "second"}},
	}
	var encoded strings.Builder
	for _, record := range []map[string]any{
		{"type": "measurement", "protocol_version": 1, "invocation_id": "invocation", "unit_id": "first", "component_id": "cog", "path": "same.go", "language": "go", "value": 1},
		{"type": "measurement", "protocol_version": 1, "invocation_id": "invocation", "unit_id": "second", "component_id": "cog", "path": "same.go", "language": "go", "value": 2},
		{"type": "terminal", "protocol_version": 1, "invocation_id": "invocation", "status": "success"},
	} {
		payload, err := json.Marshal(record)
		if err != nil {
			t.Fatal(err)
		}
		encoded.Write(payload)
		encoded.WriteByte('\n')
	}
	units, err := decodeUnitScoreInputs(strings.NewReader(encoded.String()), request)
	if err != nil {
		t.Fatal(err)
	}
	if got := units["first"].observations["same.go"]["cog"][0].value; got != 1 {
		t.Fatalf("first unit value = %v", got)
	}
	if got := units["second"].observations["same.go"]["cog"][0].value; got != 2 {
		t.Fatalf("second unit value = %v", got)
	}
}

func TestNativeStructuralAnalysisMatchesBalancedReference(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	executable := filepath.Join(root, "analyzers", "structural", "slopslap-structural")
	if info, statErr := os.Stat(executable); statErr != nil || info.IsDir() {
		t.Skip("structural analyzer is not built")
	}
	target := "analyzers/structural/internal/goadapter/adapter.go"
	analyzer, err := New(root, root, Options{Targets: []string{target}, Timeout: 30})
	if err != nil {
		t.Fatal(err)
	}
	document, err := analyzer.Analyze(context.Background(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(document.Files) != 1 {
		t.Fatalf("got %d files", len(document.Files))
	}
	if difference := math.Abs(document.Files[0].Score - 49.017512982810); difference > 1e-9 {
		t.Fatalf("native score = %.12f, reference = 49.017512982810", document.Files[0].Score)
	}
	if document.Files[0].Path != target {
		t.Fatalf("path = %q", document.Files[0].Path)
	}
}

func TestBuiltGoAnalyzerReportsTypeMetricsWithoutGOROOT(t *testing.T) {
	installationRoot, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	executable := filepath.Join(installationRoot, "analyzers", "structural", "slopslap-structural")
	if info, statErr := os.Stat(executable); statErr != nil || info.IsDir() {
		t.Skip("structural analyzer is not built")
	}
	workspace := t.TempDir()
	workspace, err = filepath.EvalSymlinks(workspace)
	if err != nil {
		t.Fatal(err)
	}
	var source strings.Builder
	source.WriteString("package sample\nimport \"fmt\"\ntype Peer struct { A, B, C, D, E, F int }\ntype Service struct { state int }\nfunc (s *Service) Run(peer Peer) int {\n_ = fmt.Sprint(peer.A)\n")
	for index := 0; index < 48; index++ {
		fmt.Fprintf(&source, "if peer.A > %d {}\n", index)
	}
	source.WriteString("return s.state + peer.A + peer.B + peer.C + peer.D + peer.E + peer.F\n}\n")
	if err := os.WriteFile(filepath.Join(workspace, "main.go"), []byte(source.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOROOT", "")
	analyzer, err := New(workspace, installationRoot, Options{Targets: []string{"."}, Languages: []string{"go"}, Timeout: 30})
	if err != nil {
		t.Fatal(err)
	}
	document, err := analyzer.Analyze(context.Background(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(document.Files) != 1 {
		t.Fatalf("files = %d, want 1", len(document.Files))
	}
	file := document.Files[0]
	for _, componentID := range []string{"coupling_between_objects", "god_class"} {
		if state := file.Coverage[componentID]; state != "complete" {
			t.Fatalf("%s coverage = %q, want complete", componentID, state)
		}
	}
	coupling := file.Components["coupling_between_objects"]
	maximumCoupling := 0.0
	for _, subject := range coupling.Subjects {
		maximumCoupling = math.Max(maximumCoupling, subject.Value)
	}
	if maximumCoupling == 0 {
		t.Fatal("CPL did not observe the imported Go type")
	}
	if contribution := file.Components["god_class"].Contribution; contribution == 0 {
		t.Fatal("GOD did not trigger for the high-WMC, high-ATFD, low-TCC type")
	}
}

func TestDecodeRecordsAcceptsLargeRecords(t *testing.T) {
	request := analyzerRequest{Invocation: "00000000-0000-0000-0000-000000000001"}
	records := []map[string]any{
		{
			"type":             "measurement",
			"protocol_version": 1,
			"invocation_id":    request.Invocation,
			"attributes":       map[string]any{"payload": strings.Repeat("x", 2*1024*1024)},
		},
		{
			"type":             "terminal",
			"protocol_version": 1,
			"invocation_id":    request.Invocation,
			"status":           "success",
		},
	}
	var encoded bytes.Buffer
	encoder := json.NewEncoder(&encoded)
	for _, record := range records {
		if err := encoder.Encode(record); err != nil {
			t.Fatal(err)
		}
	}
	decoded, err := decodeRecords(&encoded, request)
	if err != nil {
		t.Fatal(err)
	}
	if len(decoded) != len(records) {
		t.Fatalf("decoded %d records, want %d", len(decoded), len(records))
	}
}

func TestNativeJavaAndRustScoresMatchBalancedReference(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"slopslap-structural", "slopslap-structural-java.jar", "slopslap-structural-rust", filepath.Join("java-runtime", "bin", "java")} {
		if _, statErr := os.Stat(filepath.Join(root, "analyzers", "structural", required)); statErr != nil {
			t.Skip("structural analyzer helpers are not built")
		}
	}
	tests := []struct {
		path  string
		score float64
	}{
		{"analyzers/structural/adapters/java/src/dev/slopslap/structural/JavaAnalyzer.java", 51.370888906001},
		{"analyzers/structural/adapters/rust/src/parser.rs", 11.315172029169},
	}
	for _, test := range tests {
		t.Run(filepath.Ext(test.path), func(t *testing.T) {
			analyzer, newErr := New(root, root, Options{Targets: []string{test.path}, Timeout: 30})
			if newErr != nil {
				t.Fatal(newErr)
			}
			document, analyzeErr := analyzer.Analyze(context.Background(), nil, nil)
			if analyzeErr != nil {
				t.Fatal(analyzeErr)
			}
			if len(document.Files) != 1 {
				t.Fatalf("got %d files", len(document.Files))
			}
			if difference := math.Abs(document.Files[0].Score - test.score); difference > 1e-9 {
				t.Fatalf("native score = %.12f, reference = %.12f", document.Files[0].Score, test.score)
			}
		})
	}
}
