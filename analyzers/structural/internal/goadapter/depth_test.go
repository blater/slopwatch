package goadapter

import (
	"os"
	"path/filepath"
	"slopslap.dev/structural/internal/facts"
	"slopslap.dev/structural/internal/metrics"
	"testing"
)

func TestDepthSourceNumericLoopsReachScorer(t *testing.T) {
	cases := []struct {
		name, source string
		score        int
		known        bool
	}{
		{"accumulate", `package p; func Sum(n int) int { s := 0; for i := 0; i < n; i++ { s += i }; return s }`, 30, true},
		{"identity", `package p; func Keep(n, x int) int { for i := 0; i < n; i++ { x = x }; return x }`, 100, true},
		{"zero", `package p; func Zero() int { s := 0; for i := 0; i < 0; i++ { s += i }; return s }`, 100, true},
		{"identity_helper", `package p; func hidden(x int) int { return x }; func Sum(n int) int { s := 0; for i := 0; i < n; i++ { s += hidden(i) }; return s }`, 30, true},
		{"named_return", `package p; func Sum(n int) (s int) { s = n + 1; return }`, 30, true},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, "p.go"), []byte(test.source), 0600); err != nil {
				t.Fatal(err)
			}
			program, err := (Adapter{}).Analyze(root, []string{"p.go"}, map[string]any{"depth_profile": "responsibility-v4"})
			if err != nil {
				t.Fatal(err)
			}
			scores := metrics.MeasureDepth(program)
			if len(scores) != 1 {
				t.Fatalf("scores %+v", scores)
			}
			score := scores[0]
			if test.known {
				if score.State != facts.KnowledgeMeasured || score.Shallow == nil || *score.Shallow != test.score {
					t.Fatalf("source score: %+v", score)
				}
			} else if score.Shallow != nil || score.State != facts.KnowledgePartial {
				t.Fatalf("unsupported source measured: %+v", score)
			}
			legacy, err := Analyze(root, []string{"p.go"})
			if err != nil || legacy.Depth != nil {
				t.Fatalf("legacy changed: %v", err)
			}
		})
	}
}

func TestDepthSourceStableUnderLocalRenameAndFileMove(t *testing.T) {
	root := t.TempDir()
	sources := []string{
		`package p; func Sum(n int) int { s:=0;for i:=0;i<n;i++ {s+=i};return s }`,
		`package p; func Sum(count int) int { total:=0;for cursor:=0;cursor<count;cursor++ {total+=cursor};return total }`,
	}
	var original metrics.DepthScore
	for index, source := range sources {
		path := []string{"first.go", "moved.go"}[index]
		if err := os.WriteFile(filepath.Join(root, path), []byte(source), 0600); err != nil {
			t.Fatal(err)
		}
		program, err := (Adapter{}).Analyze(root, []string{path}, map[string]any{"depth_profile": "responsibility-v4"})
		if err != nil {
			t.Fatal(err)
		}
		score := metrics.MeasureDepth(program)[0]
		if index == 0 {
			original = score
			continue
		}
		if score.Shallow == nil || original.Shallow == nil || *score.Shallow != *original.Shallow || score.H != original.H || len(score.Alternatives) != len(original.Alternatives) || score.B8 != original.B8 || score.Boundary.View != "file" || score.Boundary.Symbol != path {
			t.Fatalf("local rename/move changed semantics: %+v / %+v", original, score)
		}
	}
}

func TestDepthInventoryFingerprintTracksCallerSurface(t *testing.T) {
	root := t.TempDir()
	sources := []struct {
		name   string
		source string
	}{
		{"identity", `package p; func Add(value int) int { return value }`},
		{"body-edit", `package p; func Add(value int) int { result := value; return result }`},
		{"parameter-rename", `package p; func Add(input int) int { return input }`},
		{"signature-edit", `package p; func Add(value int64) int64 { return value }`},
		{"route-removed", `package p; func add(value int) int { return value }`},
	}
	var fingerprints []string
	for _, item := range sources {
		if err := os.WriteFile(filepath.Join(root, "p.go"), []byte(item.source), 0600); err != nil {
			t.Fatal(err)
		}
		program, err := (Adapter{}).Analyze(root, []string{"p.go"}, map[string]any{"depth_profile": "responsibility-v4"})
		if err != nil {
			t.Fatal(err)
		}
		scores := metrics.MeasureDepth(program)
		if len(scores) != 1 {
			t.Fatalf("%s scores = %+v", item.name, scores)
		}
		fingerprints = append(fingerprints, scores[0].InventoryFingerprint)
	}
	if fingerprints[0] == "" || fingerprints[0] != fingerprints[1] || fingerprints[0] != fingerprints[2] {
		t.Fatalf("body or parameter rename changed inventory: %+v", fingerprints[:3])
	}
	if fingerprints[3] == fingerprints[0] || fingerprints[4] != "" {
		t.Fatalf("signature or route removal was not conservatively detected: %+v", fingerprints)
	}
}

func TestDepthIdenticalRoutesDeduplicateResponsibility(t *testing.T) {
	root := t.TempDir()
	source := `package p;func Sum(n int) int { s:=0;for i:=0;i<n;i++ {s+=i};return s };func Total(limit int) int { result:=0;for k:=0;k<limit;k++ {result+=k};return result }`
	if err := os.WriteFile(filepath.Join(root, "p.go"), []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	program, err := (Adapter{}).Analyze(root, []string{"p.go"}, map[string]any{"depth_profile": "responsibility-v4"})
	if err != nil {
		t.Fatal(err)
	}
	score := metrics.MeasureDepth(program)[0]
	if score.Shallow == nil || score.H != 2 || score.Burden.O != 2 || *score.Shallow != 43 {
		t.Fatalf("duplicated X per route: %+v", score)
	}
}
func TestDepthSyntaxHoleMakesPackagePartial(t *testing.T) {
	root := t.TempDir()
	for path, source := range map[string]string{"good.go": `package p;func Add(x int) int {return x+1}`, "broken.go": `package p;func Broken(`} {
		if err := os.WriteFile(filepath.Join(root, path), []byte(source), 0600); err != nil {
			t.Fatal(err)
		}
	}
	program, err := (Adapter{}).Analyze(root, []string{"good.go", "broken.go"}, map[string]any{"depth_profile": "responsibility-v4"})
	if err != nil {
		t.Fatal(err)
	}
	score := metrics.MeasureDepth(program)[0]
	if score.Shallow != nil || score.State != facts.KnowledgePartial {
		t.Fatalf("scored incomplete namespace: %+v", score)
	}
}
