package native

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/blater/slopwatch/internal/analysiscache"
	"github.com/blater/slopwatch/internal/report"
	"github.com/blater/slopwatch/internal/scoring"
)

// These are deliberately separate real sources. The frozen NQL switch/decimal
// fixture cannot stand in for xmltoaster's conditional formatting/newValue code.
func TestQueryResultRowVariantsPublishBoundedRatings(t *testing.T) {
	root := testInstallationRoot(t)
	for _, fixture := range []struct {
		name, snapshot, sha256 string
		wantEstimate           bool
	}{
		{"xmltoaster conditional formatting", "go/internal/sourceestimate/testdata/xmltoaster/QueryResultRow.java", "7b8327b09f7f8b0b512aab2cdf2d05b18b84c6bf43b33c46079af9c3b4520823", true},
		{"frozen NQL switch formatting", "docs/evidence/shallow-v4/graded-real/java-query-result-row/QueryResultRow.java", "43ab9bf4f338af96f37241c756e59be6a3a7135fe828ae65fd64287dd26ba627", false},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			source, err := os.ReadFile(filepath.Join(root, fixture.snapshot))
			if err != nil {
				t.Fatal(err)
			}
			if got := fmt.Sprintf("%x", sha256.Sum256(source)); got != fixture.sha256 {
				t.Fatalf("exact regression source changed: SHA-256 %s", got)
			}
			workspace := t.TempDir()
			writeTestFile(t, workspace, "QueryResultRow.java", string(source))
			analyzer, err := New(workspace, root, Options{Languages: []string{"java"}, ReadCache: true})
			if err != nil {
				t.Fatal(err)
			}
			store, err := analysiscache.NewStore(filepath.Join(t.TempDir(), "cache"))
			if err != nil {
				t.Fatal(err)
			}
			analyzer.SetCacheStore(store)
			document, err := analyzer.Analyze(context.Background(), nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			encoded, err := json.Marshal(document)
			if err != nil {
				t.Fatal(err)
			}
			var exported report.Document
			if err := json.Unmarshal(encoded, &exported); err != nil {
				t.Fatal(err)
			}
			if len(exported.Files) != 1 || len(exported.Depth) != 1 {
				t.Fatalf("missing source or evidence: %+v", exported)
			}
			file := exported.Files[0]
			metric := scoring.Metric(file, "deep")
			if !metric.Available || metric.Value < 15 || metric.Value > 45 || metric.Contribution <= 0 || !scoring.ScoreAvailable(file) {
				t.Fatalf("expected meaningful bounded numeric SHALLOW and SCORE contribution, got %+v", metric)
			}
			if !fixture.wantEstimate && metric.Value != 36 {
				t.Fatalf("frozen NQL calibration changed: %v", metric.Value)
			}
			if file.Complete || !file.Components["module_shallowness"].DepthEstimated {
				t.Fatal("unresolved implementation lost incomplete/estimated status")
			}
			for _, boundary := range exported.Depth {
				grade, ok := boundary.Raw["graded"].(map[string]any)
				if !ok {
					t.Fatal("missing graded evidence")
				}
				limits, ok := grade["material_limitations"].([]any)
				if !ok || len(limits) == 0 {
					t.Fatal("missing uncertainty limitations")
				}
				if fixture.wantEstimate {
					units, ok := grade["estimated_hidden_responsibility"].(float64)
					if !ok || units <= 0 || units > 3 {
						t.Fatalf("missing bounded returned-duty estimate: %+v", grade)
					}
				}
			}
			analyzer.runUnits = func(context.Context, string, analyzerRequest) (map[string]scoreInputs, error) {
				t.Fatal("warm lookup analysis invoked a backend")
				return nil, nil
			}
			warm, err := analyzer.Analyze(context.Background(), nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			coldLedger, err := json.Marshal(document.Depth)
			if err != nil {
				t.Fatal(err)
			}
			warmLedger, err := json.Marshal(warm.Depth)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(coldLedger, warmLedger) || len(warm.Files) != 1 || scoring.Metric(warm.Files[0], "deep") != metric {
				t.Fatal("warm cache changed lookup rating or evidence")
			}
		})
	}
}
