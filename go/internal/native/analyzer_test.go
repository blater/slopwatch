package native

import (
	"bytes"
	"context"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
	if difference := math.Abs(document.Files[0].Score - 54.677298739027); difference > 1e-9 {
		t.Fatalf("native score = %.12f, reference = 54.677298739027", document.Files[0].Score)
	}
	if document.Files[0].Path != target {
		t.Fatalf("path = %q", document.Files[0].Path)
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
		{"analyzers/structural/adapters/java/src/dev/slopslap/structural/JavaAnalyzer.java", 47.790495528375},
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
