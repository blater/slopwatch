package sourceestimate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// These ranges are an independently reviewed, frozen input to implementation.
// The packaged-CLI runner separately checks publication, SCORE and metadata.
func TestReviewedGradedBenchmark(t *testing.T) {
	var spec struct {
		Status string `json:"status"`
		Cases  []struct {
			ID       string     `json:"id"`
			Language string     `json:"language"`
			Path     string     `json:"path"`
			Range    [2]float64 `json:"expected_range"`
			Files    []struct {
				Path    string `json:"path"`
				Content string `json:"content"`
			} `json:"files"`
		} `json:"cases"`
		Comparisons []struct {
			Lower, Higher string
			MinDelta      float64 `json:"min_delta"`
		} `json:"comparisons"`
		Invariants []struct {
			Baseline, Variant string
			Tolerance         float64
			NoIncrease        bool `json:"no_increase"`
		} `json:"invariants"`
	}
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "docs", "evidence", "shallow-v4", "graded-benchmark.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &spec); err != nil {
		t.Fatal(err)
	}
	if spec.Status != "independently-reviewed-frozen" && spec.Status != "independently-reviewed-frozen-before-scoring-implementation" {
		t.Fatal("benchmark is not independently frozen")
	}
	values := map[string]float64{}
	for _, c := range spec.Cases {
		files := make([]File, 0, len(c.Files))
		for _, f := range c.Files {
			files = append(files, File{Path: f.Path, Language: c.Language, Source: []byte(f.Content)})
		}
		_, results := AnalyzeWithAttribution(files)
		r, exists := results[c.Path]
		if !exists {
			t.Errorf("%s: missing %s", c.ID, c.Path)
			continue
		}
		value := r.PublishedPenalty()
		values[c.ID] = value
		t.Logf("%s = %.0f B=%.2f H=%.2f categories=%v abstractions=%v", c.ID, value, r.Burden, r.RecognizedHidden(), r.Categories, r.Abstractions)
		if value < c.Range[0] || value > c.Range[1] {
			t.Errorf("%s = %.0f, expected %v", c.ID, value, c.Range)
		}
	}
	for _, pair := range spec.Comparisons {
		left, lok := values[pair.Lower]
		right, rok := values[pair.Higher]
		if !lok || !rok || right-left < pair.MinDelta {
			t.Errorf("%s -> %s: delta %.0f, require %.0f", pair.Lower, pair.Higher, right-left, pair.MinDelta)
		}
	}
	for _, pair := range spec.Invariants {
		left, lok := values[pair.Baseline]
		right, rok := values[pair.Variant]
		delta := right - left
		if !lok || !rok || delta > pair.Tolerance || delta < -pair.Tolerance || pair.NoIncrease && delta > 0 {
			t.Errorf("%s -> %s: invariant delta %.0f (tolerance %.0f)", pair.Baseline, pair.Variant, delta, pair.Tolerance)
		}
	}
}
