package native

// One manifest-driven scan validates the frozen relations across all four languages.

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/blater/slopwatch/internal/report"
	"github.com/blater/slopwatch/internal/scoring"
)

type structuralCalibrationManifest struct {
	ID    string `json:"id"`
	Scope struct {
		Languages []string `json:"languages"`
	} `json:"scope"`
	Evaluation struct {
		ScoreEpsilon    float64 `json:"score_epsilon"`
		RelativeEpsilon float64 `json:"relative_epsilon"`
	} `json:"evaluation"`
	Cases []struct {
		ID              string              `json:"id"`
		Partition       string              `json:"partition"`
		Files           map[string][]string `json:"files"`
		Maintainability struct {
			Relation     string  `json:"relation"`
			MinimumDelta float64 `json:"minimum_delta"`
		} `json:"maintainability"`
	} `json:"cases"`
}

func TestStructuralCalibrationManifestScan(t *testing.T) {
	root := testInstallationRoot(t)
	for _, path := range []string{
		"analyzers/structural/slopslap-structural",
		"analyzers/structural/slopslap-structural-java.jar",
		"analyzers/structural/slopslap-structural-rust",
		"analyzers/structural/java-runtime/bin/java",
		"build/typescript/slopslap-typescript",
	} {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(path))); err != nil {
			t.Skipf("structural calibration needs make-built analyzers: %s", path)
		}
	}
	manifestPath := filepath.Join(root, "go/internal/scoring/testdata/structural-v1/manifest.json")
	manifestData, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	// Changing a held-out contract requires a new benchmark version, not a
	// silent edit that could remove a language, pair, or acceptance condition.
	if got := fmt.Sprintf("%x", sha256.Sum256(manifestData)); got != "f4d685594f3ff2d931ddfc50d689596428996a07c51c0309dd9fb9132956b8de" {
		t.Fatalf("frozen structural-v1 manifest changed: %s", got)
	}
	var manifest structuralCalibrationManifest
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.ID != "structural-score-calibration-v1" {
		t.Fatalf("manifest id = %q", manifest.ID)
	}
	workspace := copyStructuralCalibrationFixture(t, root, manifest)
	analyzer, err := New(workspace, root, Options{
		Targets: []string{"."}, Languages: manifest.Scope.Languages,
		ShallowProfile: ShallowProfileResponsibilityV4,
	})
	if err != nil {
		t.Fatal(err)
	}
	document, err := analyzer.Analyze(context.Background(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	files := make(map[string]report.File, len(document.Files))
	for _, file := range document.Files {
		files[file.Path] = file
	}
	wantPaths := manifestPaths(manifest)
	assertCalibrationInventory(t, files, wantPaths)
	assertCalibrationNumericShallow(t, files)
	assertCalibrationGroupingArithmetic(t, files)
	assertCalibrationRenameInvariance(t, files, manifest)
	assertCalibrationUnchangedComponents(t, files, manifest)
	assertCalibrationRelations(t, files, manifest)
}

func copyStructuralCalibrationFixture(t *testing.T, root string, manifest structuralCalibrationManifest) string {
	t.Helper()
	sourceRoot := filepath.Join(root, "go/internal/scoring/testdata/structural-v1")
	workspace := t.TempDir()
	writeTestFile(t, workspace, "go.mod", "module structural-calibration.local\n\ngo 1.24\n")
	for _, language := range manifest.Scope.Languages {
		for _, fileList := range manifestCaseFiles(manifest, language) {
			contents, err := os.ReadFile(filepath.Join(sourceRoot, filepath.FromSlash(fileList)))
			if err != nil {
				t.Fatal(err)
			}
			writeTestFile(t, workspace, fileList, string(contents))
		}
	}
	return workspace
}

func manifestPaths(manifest structuralCalibrationManifest) []string {
	paths := map[string]bool{}
	for _, language := range manifest.Scope.Languages {
		for _, path := range manifestCaseFiles(manifest, language) {
			paths[path] = true
		}
	}
	result := make([]string, 0, len(paths))
	for path := range paths {
		result = append(result, path)
	}
	sort.Strings(result)
	return result
}

func manifestCaseFiles(manifest structuralCalibrationManifest, language string) []string {
	var paths []string
	for _, item := range manifest.Cases {
		paths = append(paths, item.Files[language]...)
	}
	return paths
}

func assertCalibrationInventory(t *testing.T, files map[string]report.File, wantPaths []string) {
	t.Helper()
	if len(files) != len(wantPaths) {
		t.Fatalf("calibration files = %d, want %d", len(files), len(wantPaths))
	}
	for _, path := range wantPaths {
		if _, ok := files[path]; !ok {
			t.Errorf("missing calibration file %s", path)
		}
	}
}

func assertCalibrationNumericShallow(t *testing.T, files map[string]report.File) {
	t.Helper()
	for path, file := range files {
		metric := scoring.Metric(file, "deep")
		if !metric.Available || math.IsNaN(metric.Value) || math.IsInf(metric.Value, 0) || math.IsNaN(metric.Contribution) || math.IsInf(metric.Contribution, 0) {
			t.Errorf("%s has no finite numeric SHALLOW: %#v", path, metric)
		}
		if math.IsNaN(file.Score) || math.IsInf(file.Score, 0) {
			t.Errorf("%s has no finite SCORE: %v", path, file.Score)
		}
	}
}

// Verify the scorer's published grouping contract from its report rather than
// restating a tautological max<=sum property over raw values. Every positive
// group charge must equal the largest supporting projected signal; the charges
// must reconcile to control-flow component totals, then to axes and SCORE.
func assertCalibrationGroupingArithmetic(t *testing.T, files map[string]report.File) {
	t.Helper()
	for path, file := range files {
		const tolerance = 1e-5
		componentAxes := map[string]float64{}
		for componentID, component := range file.Components {
			axis := component.Axis
			if axis == "" {
				axis = scoring.ComponentAxis(componentID)
			}
			componentAxes[axis] += component.Contribution
		}
		for axis, want := range file.Axes {
			if math.Abs(componentAxes[axis]-want) > tolerance {
				t.Errorf("%s axis %q = %v, component sum = %v", path, axis, want, componentAxes[axis])
			}
		}
		var axisTotal float64
		for axis, value := range file.Axes {
			axisTotal += value
			if _, ok := componentAxes[axis]; !ok && math.Abs(value) > tolerance {
				t.Errorf("%s axis %q has no component contribution: %v", path, axis, value)
			}
		}
		if math.Abs(axisTotal-file.Score) > tolerance {
			t.Errorf("%s SCORE = %v, axis sum = %v", path, file.Score, axisTotal)
		}

		attributedByComponent := map[string]float64{}
		seenGroups := map[string]bool{}
		var attributionTotal, largestCharge float64
		for _, attribution := range file.ScoringAttributions {
			if !isCalibrationControlFlow(attribution.Component) {
				t.Errorf("%s group %q charged unsupported component %q", path, attribution.Group, attribution.Component)
			}
			if seenGroups[attribution.Group] {
				t.Errorf("%s group %q charged more than once", path, attribution.Group)
			}
			seenGroups[attribution.Group] = true
			maximum := 0.0
			for id, component := range file.Components {
				if !isCalibrationControlFlow(id) || component.ScoringDefinition == nil {
					continue
				}
				for _, subject := range component.Subjects {
					group := subject.Routine
					if group == "" {
						group = subject.Subject
					}
					if group == attribution.Group {
						maximum = math.Max(maximum, subject.BaseSeverity*scoring.DefaultWeight(id))
					}
				}
			}
			if math.Abs(attribution.Contribution-maximum) > tolerance {
				t.Errorf("%s group %q charge = %v, supporting max = %v", path, attribution.Group, attribution.Contribution, maximum)
			}
			attributionTotal += attribution.Contribution
			largestCharge = math.Max(largestCharge, attribution.Contribution)
			attributedByComponent[attribution.Component] += attribution.Contribution
		}
		controlFlow := []string{"cognitive_complexity", "cyclomatic_method_complexity", "npath_complexity", "deeply_nested_if"}
		var controlFlowTotal float64
		for _, componentID := range controlFlow {
			want := file.Components[componentID].Contribution
			got := attributedByComponent[componentID]
			controlFlowTotal += want
			if math.Abs(got-want) > tolerance {
				if math.Abs(want) > tolerance || len(file.ScoringAttributions) > 0 {
					t.Errorf("%s control-flow component %q = %v, attribution sum = %v", path, componentID, want, got)
				}
			}
		}
		if len(file.ScoringAttributions) == 0 {
			if math.Abs(controlFlowTotal) > tolerance {
				t.Errorf("%s has control-flow contribution %v but no group attributions", path, controlFlowTotal)
			}
			continue
		}
		if len(file.ScoringAttributions) > 1 && attributionTotal <= largestCharge+tolerance {
			t.Errorf("%s separate routine/group charges did not add: total %v, largest %v", path, attributionTotal, largestCharge)
		}
		var unrelatedTotal float64
		for componentID, component := range file.Components {
			if !isCalibrationControlFlow(componentID) {
				unrelatedTotal += component.Contribution
			}
		}
		if math.Abs(attributionTotal+unrelatedTotal-file.Score) > tolerance {
			t.Errorf("%s attribution sum %v + unrelated components %v != SCORE %v", path, attributionTotal, unrelatedTotal, file.Score)
		}
	}
}

func isCalibrationControlFlow(componentID string) bool {
	switch componentID {
	case "cognitive_complexity", "cyclomatic_method_complexity", "npath_complexity", "deeply_nested_if":
		return true
	default:
		return false
	}
}

func assertCalibrationRenameInvariance(t *testing.T, files map[string]report.File, manifest structuralCalibrationManifest) {
	t.Helper()
	for _, item := range manifest.Cases {
		if item.ID != "rename-move" {
			continue
		}
		for language, pair := range item.Files {
			if len(pair) != 2 {
				t.Fatalf("rename pair %s has %d files", language, len(pair))
			}
			before, beforeOK := files[pair[0]]
			after, afterOK := files[pair[1]]
			if !beforeOK || !afterOK {
				continue
			}
			if rawMetricFingerprint(before) != rawMetricFingerprint(after) {
				t.Errorf("rename/move changed raw metric multiset for %s", language)
			}
			if math.Abs(before.Score-after.Score) > 1e-9 {
				t.Errorf("rename/move changed SCORE for %s: %v -> %v", language, before.Score, after.Score)
			}
		}
	}
}

func rawMetricFingerprint(file report.File) string {
	var values []string
	for componentID, component := range file.Components {
		for _, subject := range component.Subjects {
			values = append(values, componentID+":"+formatCalibrationValue(subject.Value))
		}
	}
	sort.Strings(values)
	return strings.Join(values, ",")
}

// Structural transforms may change class complexity and other related signals.
// Only rename-move is an unchanged-context pair, so only that manifest case
// gets the unrelated-component invariant here.
func assertCalibrationUnchangedComponents(t *testing.T, files map[string]report.File, manifest structuralCalibrationManifest) {
	t.Helper()
	for _, item := range manifest.Cases {
		if item.ID != "rename-move" {
			continue
		}
		for language, pair := range item.Files {
			if len(pair) != 2 {
				t.Fatalf("case %s language %s has %d files, want a pair", item.ID, language, len(pair))
			}
			before, beforeOK := files[pair[0]]
			after, afterOK := files[pair[1]]
			if !beforeOK || !afterOK {
				continue
			}
			if allComponentFingerprint(before) != allComponentFingerprint(after) {
				t.Errorf("rename/move changed unrelated component inputs for case=%s language=%s", item.ID, language)
			}
		}
	}
}

func allComponentFingerprint(file report.File) string {
	var values []string
	for componentID, component := range file.Components {
		values = append(values, componentID+":contribution="+formatCalibrationValue(component.Contribution))
		if component.RawMaximum != nil {
			values = append(values, componentID+":raw_max="+formatCalibrationValue(*component.RawMaximum))
		}
		for _, subject := range component.Subjects {
			values = append(values, componentID+":"+formatCalibrationValue(subject.Value))
		}
	}
	sort.Strings(values)
	return strings.Join(values, ",")
}

func formatCalibrationValue(value float64) string {
	return strings.TrimRight(strings.TrimRight(strconv.FormatFloat(value, 'f', 6, 64), "0"), ".")
}

func assertCalibrationRelations(t *testing.T, files map[string]report.File, manifest structuralCalibrationManifest) {
	t.Helper()
	for _, item := range manifest.Cases {
		for language, pair := range item.Files {
			if len(pair) != 2 {
				t.Fatalf("case %s language %s has %d files, want a pair", item.ID, language, len(pair))
			}
			before, beforeOK := files[pair[0]]
			after, afterOK := files[pair[1]]
			if !beforeOK || !afterOK {
				continue
			}
			primaryBefore := calibrationGroupedContribution(before)
			primaryAfter := calibrationGroupedContribution(after)
			primaryPass := calibrationRelationPasses(item.Maintainability.Relation, primaryBefore, primaryAfter, item.Maintainability.MinimumDelta, manifest.Evaluation.ScoreEpsilon, manifest.Evaluation.RelativeEpsilon)
			fullPass := calibrationRelationPasses(item.Maintainability.Relation, before.Score, after.Score, item.Maintainability.MinimumDelta, manifest.Evaluation.ScoreEpsilon, manifest.Evaluation.RelativeEpsilon)
			admissible, reason := calibrationFullScoreAdmissible(before, after)
			if !primaryPass || (admissible && !fullPass) {
				t.Errorf("calibration relation failed: case=%s language=%s relation=%s primary=%v->%v full=%v->%v full_admissible=%t", item.ID, language, item.Maintainability.Relation, primaryBefore, primaryAfter, before.Score, after.Score, admissible)
			}
			t.Logf("calibration relation: case=%s language=%s relation=%s primary=%v->%v primary_pass=%t full_score=%v->%v full_pass=%t full_score_admissible=%t reason=%s partition=%s", item.ID, language, item.Maintainability.Relation, primaryBefore, primaryAfter, primaryPass, before.Score, after.Score, fullPass, admissible, reason, item.Partition)
		}
	}
}

func calibrationGroupedContribution(file report.File) float64 {
	var total float64
	for _, attribution := range file.ScoringAttributions {
		total += attribution.Contribution
	}
	return total
}

func calibrationFullScoreAdmissible(before, after report.File) (bool, string) {
	beforeShallow := scoring.Metric(before, "deep")
	afterShallow := scoring.Metric(after, "deep")
	if beforeShallow.State != afterShallow.State {
		return false, "shallow_state_changed"
	}
	if !beforeShallow.Available || !afterShallow.Available || math.Abs(beforeShallow.Value-afterShallow.Value) > 1e-5 {
		return false, "shallow_value_changed"
	}
	if nonPrimaryComponentFingerprint(before) != nonPrimaryComponentFingerprint(after) {
		return false, "unrelated_component_changed"
	}
	return true, "equivalent_inputs"
}

func nonPrimaryComponentFingerprint(file report.File) string {
	var values []string
	for componentID, component := range file.Components {
		if isCalibrationControlFlow(componentID) || componentID == "module_shallowness" {
			continue
		}
		values = append(values, componentID+":contribution="+formatCalibrationValue(component.Contribution))
		for _, subject := range component.Subjects {
			values = append(values, componentID+":"+formatCalibrationValue(subject.Value))
		}
	}
	sort.Strings(values)
	return strings.Join(values, ",")
}

func calibrationRelationPasses(relation string, before, after, minimum, absolute, relative float64) bool {
	epsilon := math.Max(absolute, relative*math.Max(math.Abs(before), math.Abs(after)))
	switch relation {
	case "after_lt_before":
		return after <= before-minimum-epsilon
	case "after_ge_before":
		return after+epsilon >= before
	case "interacting_ge_flat", "two_ge_one":
		return after >= before+minimum+epsilon
	case "tie":
		return math.Abs(after-before) <= epsilon
	case "record_only":
		return true
	default:
		return false
	}
}
