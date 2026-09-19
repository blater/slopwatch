package sourceestimate

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
)

type sensitivityCase struct {
	ID       string
	Language string
	Path     string
	Target   string
	Range    [2]float64 `json:"expected_range"`
	Files    []struct{ Path, Content, Snapshot, SHA256 string }
}
type sensitivityCorpus struct {
	Cases       []sensitivityCase
	Comparisons []struct {
		Lower, Higher string
		MinDelta      float64 `json:"min_delta"`
	}
	Invariants []struct {
		Baseline, Variant string
		Kind              string
		Tolerance         float64
		NoIncrease        bool `json:"no_increase"`
	}
}
type sensitivityObservation struct {
	ID                     string `json:"id"`
	Baseline, Value, Delta float64
	RangeEscape            bool
	Selection              []string
	SelectionChanged       bool
}
type sensitivityRun struct {
	Parameter                                                              string
	Parameters                                                             map[string]float64
	CoverageExemption                                                      string
	RelationViolations                                                     []string
	Value                                                                  float64
	Identity                                                               string
	Cases                                                                  []sensitivityObservation
	RangeEscapes, DirectionViolations, InvarianceViolations, RankReversals int
	RatingChangedCases, EvidenceChangedCases                               int
	MaximumAbsoluteDelta                                                   float64
	GuardViolations                                                        int
}

func calibrationCorpus(t *testing.T) (sensitivityCorpus, map[string][]File) {
	t.Helper()
	var combined sensitivityCorpus
	files := map[string][]File{}
	base := filepath.Join("..", "..", "..", "docs", "evidence", "shallow-v4")
	for _, name := range []string{"graded-benchmark.json", "graded-real-benchmark.json"} {
		data, err := os.ReadFile(filepath.Join(base, name))
		if err != nil {
			t.Fatal(err)
		}
		var corpus sensitivityCorpus
		if err := json.Unmarshal(data, &corpus); err != nil {
			t.Fatal(err)
		}
		for i := range corpus.Cases {
			c := &corpus.Cases[i]
			if c.Path == "" {
				c.Path = c.Target
			}
			for _, f := range c.Files {
				source := []byte(f.Content)
				if f.Snapshot != "" {
					path, ok := containedSnapshot(base, f.Snapshot)
					if !ok {
						t.Fatal(f.Snapshot)
					}
					source, err = os.ReadFile(path)
					if err != nil {
						t.Fatal(err)
					}
					if fmt.Sprintf("%x", sha256.Sum256(source)) != f.SHA256 {
						t.Fatal("snapshot changed", f.Snapshot)
					}
				}
				files[c.ID] = append(files[c.ID], File{Path: f.Path, Language: c.Language, Source: source})
			}
		}
		combined.Cases = append(combined.Cases, corpus.Cases...)
		combined.Comparisons = append(combined.Comparisons, corpus.Comparisons...)
		combined.Invariants = append(combined.Invariants, corpus.Invariants...)
	}
	return combined, files
}

func TestCalibrationValidationAndIsolation(t *testing.T) {
	p := DefaultCalibration()
	original := p.Identity()
	for i := 1; i < reflect.TypeOf(p).NumField(); i++ {
		for _, invalid := range []float64{0, -1, math.NaN(), math.Inf(1), 1e7} {
			changed := p
			reflect.ValueOf(&changed).Elem().Field(i).SetFloat(invalid)
			if _, err := AnalyzeWithCalibration(nil, changed); err == nil {
				t.Fatalf("accepted invalid field %d", i)
			}
		}
		changed := p
		v := reflect.ValueOf(&changed).Elem().Field(i)
		v.SetFloat(v.Float() * 1.2)
		if changed.Identity() == original {
			t.Fatalf("identity ignores field %d", i)
		}
	}
	p.Name = ""
	if p.Validate() == nil {
		t.Fatal("accepted unnamed profile")
	}
	source := []File{{Path: "Example.java", Language: "java", Source: []byte("public class Example { public int process(int a,int b,int c){return external(a);} }")}}
	_, baseline := AnalyzeWithAttribution(source)
	var wg sync.WaitGroup
	for i := 0; i < 24; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			profile := DefaultCalibration()
			profile.Operation *= float64(i + 1)
			if _, err := AnalyzeWithCalibration(source, profile); err != nil {
				t.Error(err)
			}
			_, got := AnalyzeWithAttribution(source)
			if !reflect.DeepEqual(got, baseline) {
				t.Error("default contaminated by concurrent perturbation")
			}
		}(i)
	}
	wg.Wait()
	if DefaultCalibrationIdentity() != original {
		t.Fatal("default identity mutated")
	}
}

// This suite reports model sensitivity; perturbed band/direction/invariance
// escapes are observations, while default failures and guard increases fail.
func TestCalibrationSensitivity(t *testing.T) {
	corpus, files := calibrationCorpus(t)
	defaults := DefaultCalibration()
	baseline := map[string]Result{}
	for _, c := range corpus.Cases {
		_, normal := AnalyzeWithAttribution(files[c.ID])
		parameterized, err := AnalyzeWithCalibration(files[c.ID], defaults)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(normal[c.Path].Grade, parameterized[c.Path].Grade) || normal[c.Path].PublishedPenalty() != parameterized[c.Path].PublishedPenalty() || !reflect.DeepEqual(selectedAbstractions(normal[c.Path].Abstractions), selectedAbstractions(parameterized[c.Path].Abstractions)) {
			t.Fatalf("default mismatch %s normal=%+v parameterized=%+v", c.ID, normal[c.Path].Grade, parameterized[c.Path].Grade)
		}
		r, ok := parameterized[c.Path]
		if !ok {
			t.Fatal("missing case", c.ID)
		}
		baseline[c.ID] = r
		if v := r.PublishedPenalty(); v < c.Range[0] || v > c.Range[1] {
			t.Errorf("default %s = %v outside %v", c.ID, v, c.Range)
		}
	}
	baselineValues := map[string]float64{}
	for id, r := range baseline {
		baselineValues[id] = r.PublishedPenalty()
		assertCalibrationMaximum(t, r)
	}
	directions, invariants := calibrationRelations(t, corpus, baselineValues)
	if directions != 0 || invariants != 0 {
		t.Errorf("default relations failed: directions=%d invariants=%d", directions, invariants)
	}
	if violations := calibrationGuards(t, defaults); violations != 0 {
		t.Errorf("default guards: %d", violations)
	}
	calibrationSparseParameterProbes(t)
	runs := []sensitivityRun{}
	models := calibrationModels(defaults)
	for _, model := range models {
		p := model.profile
		run := sensitivityRun{Parameter: model.name, Value: model.value, Parameters: model.parameters, Identity: p.Identity()}
		values := map[string]float64{}
		for _, c := range corpus.Cases {
			results, err := AnalyzeWithCalibration(files[c.ID], p)
			if err != nil {
				t.Fatal(err)
			}
			r, ok := results[c.Path]
			if !ok {
				t.Fatal("missing perturbed case", c.ID)
			}
			value := r.PublishedPenalty()
			values[c.ID] = value
			assertCalibrationMaximum(t, r)
			selection := selectedAbstractions(r.Abstractions)
			delta := value - baseline[c.ID].PublishedPenalty()
			if delta != 0 {
				run.RatingChangedCases++
			}
			if math.Abs(delta) > run.MaximumAbsoluteDelta {
				run.MaximumAbsoluteDelta = math.Abs(delta)
			}
			if baseline[c.ID].Grade != nil && r.Grade != nil {
				before, after := *baseline[c.ID].Grade, *r.Grade
				before.DenominatorReference, before.ResponsibilityMultiplier = after.DenominatorReference, after.ResponsibilityMultiplier
				if !reflect.DeepEqual(before, after) {
					run.EvidenceChangedCases++
				}
			} else if !reflect.DeepEqual(baseline[c.ID].Grade, r.Grade) {
				run.EvidenceChangedCases++
			}
			escape := value < c.Range[0] || value > c.Range[1]
			if escape {
				run.RangeEscapes++
			}
			run.Cases = append(run.Cases, sensitivityObservation{ID: c.ID, Baseline: baseline[c.ID].PublishedPenalty(), Value: value, Delta: value - baseline[c.ID].PublishedPenalty(), RangeEscape: escape, Selection: selection, SelectionChanged: !reflect.DeepEqual(selection, selectedAbstractions(baseline[c.ID].Abstractions))})
		}
		run.DirectionViolations, run.InvarianceViolations = calibrationRelations(t, corpus, values, &run.RelationViolations)
		for j, left := range corpus.Cases {
			for _, right := range corpus.Cases[j+1:] {
				if (baseline[left.ID].PublishedPenalty()-baseline[right.ID].PublishedPenalty())*(values[left.ID]-values[right.ID]) < 0 {
					run.RankReversals++
				}
			}
		}
		run.GuardViolations = calibrationGuards(t, p)
		if run.GuardViolations > 0 {
			t.Errorf("%s=%g: %d guard violations", run.Parameter, run.Value, run.GuardViolations)
		}
		t.Logf("%s=%g ranges=%d directions=%d invariants=%d rank_reversals=%d guards=%d", run.Parameter, run.Value, run.RangeEscapes, run.DirectionViolations, run.InvarianceViolations, run.RankReversals, run.GuardViolations)
		if len(model.parameters) == 1 && run.RatingChangedCases == 0 && run.EvidenceChangedCases == 0 {
			switch model.name {
			case "mutable_alias", "state", "transform":
				run.CoverageExemption = "Frozen corpus has no observable effect; required component-specific synthetic mechanism probe covers this parameter."
			default:
				t.Errorf("uncovered calibration parameter %s=%g: add an explicit mechanism probe or reviewed exemption", model.name, model.value)
			}
		}
		runs = append(runs, run)
	}
	report := struct {
		Default         CalibrationProfile
		DefaultIdentity string
		Perturbation    string
		Runs            []sensitivityRun
	}{defaults, defaults.Identity(), "one-at-a-time 0.8/1.2 plus six declared interacting parameter pairs at all four 0.8/1.2 corners; all other parameters fixed", runs}
	if path := os.Getenv("SHALLOW_SENSITIVITY_OUTPUT"); path != "" {
		data, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, append(data, '\n'), 0644); err != nil {
			t.Fatal(err)
		}
	}
}

func calibrationGuards(t *testing.T, p CalibrationProfile) int {
	t.Helper()
	failures := 0
	for _, c := range []struct{ language, path, prefix, suffix, body, guard string }{
		{"java", "Example.java", "public class Example { public Object process(String key,int a,int b){", "}}", "var value=store.get(key);return value==null ? null : value.render();", "if(key==null)return null;"},
		{"typescript", "example.ts", "export class Example { process(key:string,a:number,b:number){", "}}", "let value=store.get(key);return value==null ? null : value.render();", "if(key==null)return null;"},
		{"go", "example.go", "package p;func Process(key string,a,b int)string{", "}", "value:=store.Get(key);return value.Render()", "if key==\"\" {return \"\"};"},
		{"rust", "lib.rs", "pub fn process(key:&str,a:i32,b:i32)->String{", "}", "let value=store.get(key);value.render()", "if key==\"\" {return String::new();}"},
	} {
		evaluate := func(body string) Result {
			r, err := AnalyzeWithCalibration([]File{{Path: c.path, Language: c.language, Source: []byte(c.prefix + body + c.suffix)}}, p)
			if err != nil {
				t.Fatal(err)
			}
			return r[c.path]
		}
		bare, guarded := evaluate(c.body), evaluate(c.guard+c.body)
		if guarded.PublishedPenalty() > bare.PublishedPenalty() {
			failures++
		}
		for _, r := range []Result{bare, guarded} {
			if r.Grade == nil {
				t.Fatal("guard missing grade", c.language)
			}
			expected := gradeResultDutyEnvelopeWithCalibration(r.Grade.Responsibilities, p)
			if math.Abs(r.Grade.EstimatedHidden-expected) > 1e-9 {
				failures++
			}
		}
	}
	return failures
}

func assertCalibrationMaximum(t *testing.T, r Result) {
	t.Helper()
	if r.RoleOnly || len(r.Abstractions) == 0 {
		return
	}
	maximum := 0.0
	for _, a := range r.Abstractions {
		maximum = math.Max(maximum, a.Grade.Value())
	}
	if r.PublishedPenalty() != maximum {
		t.Fatalf("parameterized selection %v != abstraction maximum %v", r.PublishedPenalty(), maximum)
	}
}

// Mechanism probes complement the frozen corpus where weights are unexercised
// or rounded away. They establish wiring, not independent calibration fit.
func TestCalibrationSparseParameterProbes(t *testing.T) { calibrationSparseParameterProbes(t) }
func calibrationSparseParameterProbes(t *testing.T) {
	t.Helper()
	for _, c := range []struct{ field, source string }{
		{"MutableAlias", `public class Example { public int values[]; public int read(int a,int b,int c){return values[0]+a;} }`},
		{"State", `public class Example { private int value; public void advance(int a,int b,int c){this.value++;} }`},
		{"Transform", `public class Example { public int compute(int a,int b,int c){return a*3;} }`},
	} {
		files := []File{{Path: "Example.java", Language: "java", Source: []byte(c.source)}}
		p := DefaultCalibration()
		before, err := AnalyzeWithCalibration(files, p)
		if err != nil {
			t.Fatal(err)
		}
		v := reflect.ValueOf(&p).Elem().FieldByName(c.field)
		v.SetFloat(v.Float() * 1.2)
		after, err := AnalyzeWithCalibration(files, p)
		if err != nil {
			t.Fatal(err)
		}
		a, b := before["Example.java"].Grade, after["Example.java"].Grade
		if a == nil || b == nil {
			t.Fatal("missing mechanism grade", c.field)
		}
		switch c.field {
		case "MutableAlias":
			if a.Surface.RepresentationUnits != DefaultCalibration().MutableAlias || b.Surface.RepresentationUnits != p.MutableAlias || b.ResidualBurden <= a.ResidualBurden || b.Value() < a.Value() || a.Hidden != b.Hidden || a.EstimatedHidden != b.EstimatedHidden {
				t.Fatalf("mutable alias burden wiring/direction failed: %+v -> %+v", a, b)
			}
		case "State", "Transform":
			category := "state"
			expected := p.State
			old := DefaultCalibration().State
			if c.field == "Transform" {
				category = "transform"
				expected = p.Transform
				old = DefaultCalibration().Transform
			}
			if a.Responsibilities[category] != old || b.Responsibilities[category] != expected || math.Abs((b.Hidden-a.Hidden)-(expected-old)) > 1e-9 || a.ResidualBurden != b.ResidualBurden || b.Value() > a.Value() {
				t.Fatalf("%s responsibility wiring/direction failed: %+v -> %+v", c.field, a, b)
			}
		}
	}
}

type calibrationModel struct {
	name       string
	value      float64
	parameters map[string]float64
	profile    CalibrationProfile
}

func calibrationModels(defaults CalibrationProfile) []calibrationModel {
	models := []calibrationModel{}
	for i := 1; i < reflect.TypeOf(defaults).NumField(); i++ {
		for _, factor := range []float64{.8, 1.2} {
			p := defaults
			field := reflect.ValueOf(&p).Elem().Field(i)
			field.SetFloat(field.Float() * factor)
			name := reflect.TypeOf(p).Field(i).Tag.Get("json")
			models = append(models, calibrationModel{name, field.Float(), map[string]float64{name: field.Float()}, p})
		}
	}
	// These pairs exercise surface/normalization, overlapping observed/estimated
	// categories, and retained protocol burden versus coordination credit.
	for _, pair := range [][2]string{{"Operation", "ResidualReference"}, {"Validation", "ValidationEnvelope"}, {"Transform", "TransformationEnvelope"}, {"Sequencing", "Coordination"}, {"Validation", "ExposedValidationCap"}, {"CoupledField", "ExposedValidationCap"}} {
		for _, left := range []float64{.8, 1.2} {
			for _, right := range []float64{.8, 1.2} {
				p := defaults
				params := map[string]float64{}
				for i, name := range pair {
					field := reflect.ValueOf(&p).Elem().FieldByName(name)
					factor := left
					if i == 1 {
						factor = right
					}
					field.SetFloat(field.Float() * factor)
					decl, _ := reflect.TypeOf(p).FieldByName(name)
					params[decl.Tag.Get("json")] = field.Float()
				}
				models = append(models, calibrationModel{name: pair[0] + "+" + pair[1], parameters: params, profile: p})
			}
		}
	}
	return models
}

func calibrationRelations(t *testing.T, corpus sensitivityCorpus, values map[string]float64, details ...*[]string) (int, int) {
	t.Helper()
	directions, invariants := 0, 0
	record := func(message string) {
		if len(details) > 0 {
			*details[0] = append(*details[0], message)
		}
	}
	lookup := func(id string) float64 {
		v, ok := values[id]
		if !ok {
			t.Fatalf("relation references absent case %s", id)
		}
		return v
	}
	for _, pair := range corpus.Comparisons {
		if lookup(pair.Higher)-lookup(pair.Lower) < pair.MinDelta {
			directions++
			record(fmt.Sprintf("direction %s -> %s: delta %g below %g", pair.Lower, pair.Higher, lookup(pair.Higher)-lookup(pair.Lower), pair.MinDelta))
		}
	}
	for _, pair := range corpus.Invariants {
		left, right := lookup(pair.Baseline), lookup(pair.Variant)
		delta := right - left
		if math.Abs(delta) > pair.Tolerance || pair.NoIncrease && delta > 0 || pair.Kind == "uncertainty" && left > 0 && right == 0 {
			invariants++
			record(fmt.Sprintf("%s %s -> %s: %g -> %g; tolerance %g, no_increase=%t", pair.Kind, pair.Baseline, pair.Variant, left, right, pair.Tolerance, pair.NoIncrease))
		}
	}
	return directions, invariants
}

func TestCalibrationUncertaintyCannotCollapseWithinTolerance(t *testing.T) {
	corpus, _ := calibrationCorpus(t)
	selected := sensitivityCorpus{}
	for _, pair := range corpus.Invariants {
		if pair.Kind == "uncertainty" {
			selected.Invariants = append(selected.Invariants, pair)
			break
		}
	}
	if len(selected.Invariants) != 1 {
		t.Fatal("frozen uncertainty constraints missing")
	}
	pair := selected.Invariants[0]
	// The absolute drop fits the frozen tolerance; the uncertainty-specific
	// positive-to-zero safeguard must independently reject it.
	values := map[string]float64{pair.Baseline: 1, pair.Variant: 0}
	_, violations := calibrationRelations(t, selected, values)
	if violations != 1 {
		t.Fatal("uncertainty erased a positive baseline without violating tolerance")
	}
	values[pair.Variant] = 1
	_, violations = calibrationRelations(t, selected, values)
	if violations != 0 {
		t.Fatal("unchanged uncertainty variant rejected")
	}
}
