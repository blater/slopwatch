package rustadapter

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"slopslap.dev/structural/internal/facts"
	"slopslap.dev/structural/internal/metrics"
)

func TestRustDepthSourceToScorePreservesLegacyFacts(t *testing.T) {
	_, filename, _, _ := runtime.Caller(0)
	executable := filepath.Join(filepath.Dir(filename), "..", "..", "slopslap-structural-rust")
	if _, err := os.Stat(executable); err != nil {
		t.Skip("Rust helper is not built")
	}
	root := t.TempDir()
	identity := "identity.rs"
	wrapping := "wrapping.rs"
	unsupported := "unsupported.rs"
	shadowed := "shadowed.rs"
	if err := os.WriteFile(filepath.Join(root, identity), []byte("pub fn keep(value: i32) -> i32 { value }\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, wrapping), []byte("pub fn add(value: i32) -> i32 { let result = value.wrapping_add(1); result }\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, unsupported), []byte("pub fn choose(value: i32) -> i32 { if value > 0 { value } else { 0 } }\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, shadowed), []byte("struct i32; pub fn fake(value: i32) -> i32 { value.wrapping_add(1) }\n"), 0600); err != nil {
		t.Fatal(err)
	}
	adapter := Adapter{Executable: executable}
	paths := []string{identity, wrapping, unsupported, shadowed}
	legacy, err := adapter.Analyze(root, paths, nil)
	if err != nil {
		t.Fatal(err)
	}
	program, err := adapter.Analyze(root, paths, map[string]any{"depth_profile": "responsibility-v4"})
	if err != nil {
		t.Fatal(err)
	}
	depth := program.Depth
	program.Depth = nil
	if !reflect.DeepEqual(program, legacy) {
		t.Fatal("opt-in depth changed legacy Rust facts")
	}
	program.Depth = depth
	scores := metrics.MeasureDepth(program)
	if len(scores) != len(paths) {
		t.Fatalf("scores = %#v", scores)
	}
	var measured, wrappingMeasured, partial, shadowedPartial bool
	for _, score := range scores {
		switch score.Boundary.Symbol {
		case identity:
			measured = score.State == facts.KnowledgeMeasured && score.Shallow != nil && *score.Shallow == 100
		case wrapping:
			wrappingMeasured = score.State == facts.KnowledgeMeasured && score.Shallow != nil && *score.Shallow == 30 && score.H == 2
		case unsupported:
			partial = score.State == facts.KnowledgePartial && score.Shallow == nil
		case shadowed:
			shadowedPartial = score.State == facts.KnowledgePartial && score.Shallow == nil
		}
	}
	if !measured || !wrappingMeasured || !partial || !shadowedPartial {
		t.Fatalf("identity/unsupported scores = %#v", scores)
	}
}

func TestValidateFactResponsePreservesSyntaxFailures(t *testing.T) {
	response, err := decodeFactResponse(strings.NewReader(`{
		"schema_version":2,
		"program":{"functions":[],"types":[],"public_operations":[],"representation_exposure":[],"files":["valid.rs"],"unavailable":{},"failures":[{"path":"broken.rs","code":"SYNTAX_ERROR","diagnostic":"broken.rs:1:5: unexpected token"}]},
		"error":null
	}`))
	if err != nil {
		t.Fatal(err)
	}
	program, err := validateFactResponse(response)
	if err != nil {
		t.Fatal(err)
	}
	if len(program.Files) != 1 || program.Files[0] != "valid.rs" || len(program.Failures) != 1 {
		t.Fatalf("program = %#v", program)
	}
	if program.Failures[0].Path != "broken.rs" || program.Failures[0].Code != "SYNTAX_ERROR" {
		t.Fatalf("failure = %#v", program.Failures[0])
	}
}
