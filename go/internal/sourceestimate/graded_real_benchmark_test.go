package sourceestimate

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// TestReviewedRealGradedBenchmark exercises the independently reviewed real
// source manifest without consulting packaged CLI output.  The manifest is a
// frozen engineering contract: snapshot hashes are verified before source is
// passed to the bounded estimator, and the published grade is checked against
// the reviewed range.
func TestReviewedRealGradedBenchmark(t *testing.T) {
	type snapshot struct {
		Path     string `json:"path"`
		Snapshot string `json:"snapshot"`
		SHA256   string `json:"sha256"`
		Origin   string `json:"origin"`
	}
	type realCase struct {
		ID           string     `json:"id"`
		Language     string     `json:"language"`
		Target       string     `json:"target"`
		Files        []snapshot `json:"files"`
		Range        [2]float64 `json:"expected_range"`
		AllowNA      bool       `json:"allow_not_applicable"`
		ExcludeCover bool       `json:"exclude_from_semantic_numeric_coverage"`
	}
	var spec struct {
		Status    string     `json:"status"`
		RangesSet bool       `json:"ranges_set_before_new_implementation"`
		Cases     []realCase `json:"cases"`
	}

	manifest := filepath.Join("..", "..", "..", "docs", "evidence", "shallow-v4", "graded-real-benchmark.json")
	data, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &spec); err != nil {
		t.Fatal(err)
	}
	if spec.Status != "independently-reviewed-frozen" {
		t.Fatalf("real benchmark status %q is not independently-reviewed-frozen", spec.Status)
	}
	if !spec.RangesSet {
		t.Fatal("real benchmark does not declare ranges_set_before_new_implementation")
	}

	manifestDir := filepath.Dir(manifest)
	values := make(map[string]float64, len(spec.Cases))
	for _, c := range spec.Cases {
		files := make([]File, 0, len(c.Files))
		caseFailed := false
		seenPaths := map[string]bool{}
		for _, f := range c.Files {
			if f.Path == "" || seenPaths[f.Path] {
				t.Errorf("%s: duplicate or empty source path %q", c.ID, f.Path)
				caseFailed = true
				continue
			}
			seenPaths[f.Path] = true
			snapshotPath, ok := containedSnapshot(manifestDir, f.Snapshot)
			if !ok {
				t.Errorf("%s: snapshot path escapes manifest directory: %q", c.ID, f.Snapshot)
				caseFailed = true
				continue
			}
			source, readErr := os.ReadFile(snapshotPath)
			if readErr != nil {
				t.Errorf("%s/%s: read snapshot: %v", c.ID, f.Path, readErr)
				caseFailed = true
				continue
			}
			got := sha256.Sum256(source)
			actualHash := hex.EncodeToString(got[:])
			if !strings.EqualFold(actualHash, f.SHA256) {
				t.Errorf("%s/%s: snapshot sha256 %s, want %s", c.ID, f.Path, actualHash, f.SHA256)
				caseFailed = true
				continue
			}
			files = append(files, File{Path: f.Path, Language: c.Language, Source: source})
		}
		if caseFailed || len(files) == 0 {
			continue
		}
		_, results := AnalyzeWithAttribution(files)
		r, exists := results[c.Target]
		if !exists {
			t.Errorf("%s: missing target result %q (results=%v)", c.ID, c.Target, resultPaths(results))
			continue
		}

		value := r.PublishedPenalty()
		if r.Grade != nil {
			t.Logf("GRADE %s E=%g H=%g ops=%g inputs=%g representation=%g responsibilities=%v", c.ID, r.Grade.ResidualBurden, r.Grade.Hidden, r.Grade.Surface.OperationUnits, r.Grade.Surface.InputUnits, r.Grade.Surface.RepresentationUnits, r.Grade.Responsibilities)
		}
		values[c.ID] = value
		selected := selectedAbstractions(r.Abstractions)
		zero := ""
		if r.Grade != nil {
			zero = r.Grade.ZeroReason
		}
		roleZero := value == 0 && (r.RoleOnly || !r.Applicable || r.NoAbstractionProven || len(r.Roles) > 0)
		t.Logf("%s actualGrade=%.0f expected=%v selected_abstractions=%v role_zero=%v roles=%v applicable=%v no_abstraction_proven=%v limitations=%v zero_reason=%q", c.ID, value, c.Range, selected, roleZero, r.Roles, r.Applicable, r.NoAbstractionProven, r.Limitations, zero)

		if value < c.Range[0] || value > c.Range[1] {
			t.Errorf("%s actualGrade=%.0f, expected range %v", c.ID, value, c.Range)
		}
		// The declaration-only Go control is explicitly permitted to remain
		// zero. Report NoAbstractionProven and role state above; the manifest's
		// range and allow_not_applicable flag define acceptance here.
	}
}

func containedSnapshot(base, relative string) (string, bool) {
	if relative == "" || filepath.IsAbs(relative) {
		return "", false
	}
	baseAbs, err := filepath.Abs(base)
	if err != nil {
		return "", false
	}
	candidate := filepath.Clean(filepath.Join(baseAbs, relative))
	rel, err := filepath.Rel(baseAbs, candidate)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return candidate, true
}

func resultPaths(results map[string]Result) []string {
	paths := make([]string, 0, len(results))
	for path := range results {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths
}

func selectedAbstractions(abstractions []Abstraction) []string {
	if len(abstractions) == 0 {
		return nil
	}
	best := -1.0
	for _, abstraction := range abstractions {
		grade := 0.0
		if abstraction.Grade != nil {
			grade = abstraction.Grade.Value()
		}
		if grade > best {
			best = grade
		}
	}
	selected := make([]string, 0, len(abstractions))
	for _, abstraction := range abstractions {
		grade := 0.0
		if abstraction.Grade != nil {
			grade = abstraction.Grade.Value()
		}
		if grade == best {
			name := abstraction.Name
			if name == "" {
				name = fmt.Sprintf("%s@%.0f", abstraction.Audience, grade)
			}
			selected = append(selected, name)
		}
	}
	sort.Strings(selected)
	return selected
}
