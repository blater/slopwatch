package sourceestimate

import (
	"reflect"
	"testing"
)

func TestAnalyzeWithAttributionProgressMatchesResults(t *testing.T) {
	files := []File{
		{Path: "service.go", Language: "go", Source: []byte("package p; type Service struct{}; func (Service) Run() {}")},
		{Path: "service.ts", Language: "typescript", Source: []byte("export function run(): number { return 1; }")},
		{Path: "Service.java", Language: "java", Source: []byte("public class Service { public void run() {} }")},
		{Path: "service.rs", Language: "rust", Source: []byte("pub struct Service; impl Service { pub fn run(&self) {} }")},
	}
	_, want := AnalyzeWithAttribution(files)
	seen := map[string]Result{}
	_, got := AnalyzeWithAttributionProgress(files, func(file File, result Result) {
		seen[file.Path] = result
	})
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("progress results differ from ordinary attribution: got=%#v want=%#v", got, want)
	}
	if !reflect.DeepEqual(seen, got) {
		t.Fatalf("callback results differ from returned attribution: got=%#v want=%#v", seen, got)
	}
}

func TestAttributionStatusReportsPreparationBeforeEvaluation(t *testing.T) {
	files := []File{
		{Path: "first.go", Language: "go", Source: []byte("package p; func Run() {}")},
		{Path: "second.go", Language: "go", Source: []byte("package p; func Stop() {}")},
	}
	var stages []AttributionProgress
	var results []string
	AnalyzeWithAttributionProgressAndStatus(files, func(file File, _ Result) {
		results = append(results, file.Path)
	}, func(progress AttributionProgress) {
		stages = append(stages, progress)
	})
	if len(stages) == 0 || len(results) != len(files) {
		t.Fatalf("status=%#v results=%#v", stages, results)
	}
	firstResultStage := -1
	for index, stage := range stages {
		if stage.Stage == "source_evaluate_go" && stage.Completed > 0 {
			firstResultStage = index
			break
		}
	}
	if firstResultStage < 0 {
		t.Fatalf("missing Go evaluation progress: %#v", stages)
	}
	for _, stage := range stages[:firstResultStage] {
		if stage.Stage == "source_complete" {
			t.Fatalf("completion reported before evaluation: %#v", stages)
		}
	}
}
