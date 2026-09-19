package native

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/blater/slopwatch/internal/report"
	"github.com/blater/slopwatch/internal/scoring"
)

// Exercise the installed adapters and JSON publication, not only token helpers.
// Admission, guarded use, and restoration are inverted together.
func TestSourceDepthBooleanPolarityPublication(t *testing.T) {
	installation := testInstallationRoot(t)
	for _, tc := range []struct{ language, path, source string }{
		{"java", "Example.java", `class Driver{boolean active=false;void begin(){active=true;}int use(){if(!active)throw new RuntimeException();return 1;}void end(){active=false;}}public class Example{private Driver d=new Driver();public int run(){d.begin();try{return d.use();}finally{d.end();}}}`},
		{"typescript", "example.ts", `class Driver{active=false;begin(){this.active=true;}use(){if(!this.active)throw new Error();return 1;}end(){this.active=false;}}export class Example{private d=new Driver();run(){this.d.begin();try{return this.d.use();}finally{this.d.end();}}}`},
		{"go", "example.go", `package sample;type Driver struct{active bool};func NewDriver()Driver{return Driver{active:false}};func(d *Driver)Begin(){d.active=true};func(d *Driver)Use()int{if !d.active {panic("inactive")};return 1};func(d *Driver)End(){d.active=false};type Example struct{d Driver};func(e *Example)Run()int{e.d.Begin();defer e.d.End();return e.d.Use()}`},
		{"rust", "lib.rs", `struct Driver{active:bool}impl Driver{fn new()->Self{Self{active:false}}fn begin(&mut self){self.active=true;}fn use_value(&self)->i32{assert!(self.active);1}fn end(&mut self){self.active=false;}}struct Guard<'a>(&'a mut Driver);impl Drop for Guard<'_>{fn drop(&mut self){self.0.end();}}pub struct Example{d:Driver}impl Example{pub fn run(&mut self)->i32{self.d.begin();let guard=Guard(&mut self.d);let out=guard.0.use_value();drop(guard);out}}`},
	} {
		t.Run(tc.language, func(t *testing.T) {
			inverted := strings.NewReplacer("true", "false", "false", "true").Replace(tc.source)
			if tc.language == "rust" {
				inverted = strings.ReplaceAll(inverted, "assert!(self.active)", "assert!(!self.active)")
			} else {
				inverted = strings.NewReplacer("!this.active", "this.active", "!d.active", "d.active", "!active", "active").Replace(inverted)
			}
			var prior scoring.MetricValue
			var priorHidden, priorEstimated any
			var priorGrade map[string]any
			var priorRating any
			for i, source := range []string{tc.source, inverted} {
				workspace := t.TempDir()
				writeTestFile(t, workspace, tc.path, source)
				analyzer, err := New(workspace, installation, Options{Languages: []string{tc.language}})
				if err != nil {
					t.Fatal(err)
				}
				document, err := analyzer.Analyze(context.Background(), nil, nil)
				if err != nil {
					t.Fatal(err)
				}
				encoded, err := json.Marshal(document)
				if err != nil {
					t.Fatal(err)
				}
				var exported report.Document
				if err = json.Unmarshal(encoded, &exported); err != nil {
					t.Fatal(err)
				}
				if len(exported.Files) != 1 {
					t.Fatalf("missing file publication: %+v", exported.Files)
				}
				file := exported.Files[0]
				metric := scoring.Metric(file, "deep")
				if !metric.Available || !scoring.ScoreAvailable(file) {
					t.Fatalf("missing numeric SHALLOW/SCORE: %+v", file)
				}
				found := false
				for _, boundary := range exported.Depth {
					estimate, _ := boundary.Raw["estimate"].(map[string]any)
					abstractions, _ := estimate["abstractions"].([]any)
					for _, value := range abstractions {
						abstraction, _ := value.(map[string]any)
						name, _ := abstraction["name"].(string)
						if !strings.HasSuffix(name, "Example") {
							continue
						}
						grade, _ := abstraction["graded"].(map[string]any)
						duties, _ := grade["responsibilities"].(map[string]any)
						resource, _ := duties["resource"].(float64)
						if resource <= 0 {
							t.Fatalf("connected resource responsibility missing (inverted=%v): %+v", i == 1, grade)
						}
						if i == 1 {
							for _, key := range []string{"responsibilities", "surface", "residual_burden", "supported_burden"} {
								if !reflect.DeepEqual(priorGrade[key], grade[key]) {
									t.Fatalf("polarity changes Example %s: %v -> %v", key, priorGrade[key], grade[key])
								}
							}
							if priorRating != abstraction["published_penalty"] {
								t.Fatalf("polarity changes Example rating: %v -> %v", priorRating, abstraction["published_penalty"])
							}
						}
						found = true
						if i == 0 {
							prior = metric
							priorHidden = grade["hidden_responsibility"]
							priorEstimated = grade["estimated_hidden_responsibility"]
							priorGrade = grade
							priorRating = abstraction["published_penalty"]
						} else if metric.Value != prior.Value || metric.Contribution != prior.Contribution || priorHidden != grade["hidden_responsibility"] || priorEstimated != grade["estimated_hidden_responsibility"] {
							t.Fatalf("polarity changes publication: SHALLOW %+v -> %+v; H %v -> %v; U %v -> %v", prior, metric, priorHidden, grade["hidden_responsibility"], priorEstimated, grade["estimated_hidden_responsibility"])
						}
					}
				}
				if !found {
					t.Fatal("missing Example evidence in exported report")
				}
			}
		})
	}
}
