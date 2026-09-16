package follow

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/blater/slopwatch/internal/report"
	"github.com/blater/slopwatch/internal/style"
)

var syntaxDisplayedComponents = []string{
	"cognitive_complexity",
	"npath_complexity",
	"cyclomatic_method_complexity",
	"module_shallowness",
	"god_class",
	"coupling_between_objects",
}

func syntaxTestFile(path string, score float64, complete bool) report.File {
	file := report.File{
		Path: path, Language: "typescript", Score: score, Complete: complete,
		ValidZero:  complete && score == 0,
		Components: map[string]report.Component{}, Coverage: map[string]string{},
	}
	state := "failed"
	if complete {
		state = "complete"
	}
	for _, id := range syntaxDisplayedComponents {
		file.Coverage[id] = state
		file.Components[id] = report.Component{Subjects: []report.SubjectContribution{{Value: score}}}
	}
	return file
}

func syntaxFixedColumns(file report.File) string {
	return renderFixedColumns(Model{visible: defaultColumnVisibility()}, file, rowState{}, style.SurfaceScreen)
}

func TestFailedCoverageUsesXAcrossDisplayedColumnsAndKeepsValidZeroNumeric(t *testing.T) {
	failed := syntaxTestFile("broken.ts", 0, false)
	rendered := ansi.Strip(syntaxFixedColumns(failed))
	if got := strings.Count(rendered, "X"); got != len(syntaxDisplayedComponents)+1 {
		t.Fatalf("failed displayed columns = %d X values, want %d: %q", got, len(syntaxDisplayedComponents)+1, rendered)
	}

	zero := syntaxTestFile("empty.ts", 0, true)
	rendered = ansi.Strip(syntaxFixedColumns(zero))
	if strings.Contains(rendered, "X") || strings.Count(rendered, "0") < len(syntaxDisplayedComponents)+1 {
		t.Fatalf("valid zero columns = %q, want numeric zeros", rendered)
	}
}

func TestFailedMeasurementsSortLastInBothDirectionsAndTiesUsePath(t *testing.T) {
	failed := syntaxTestFile("broken.ts", 100, false)
	measured := syntaxTestFile("measured.ts", 1, true)
	measured.Components["cognitive_complexity"] = report.Component{Subjects: []report.SubjectContribution{{Value: 1}}}

	for _, key := range []string{"score", "cog"} {
		model := Model{sortKey: key}
		if !model.less(measured, failed) || model.less(failed, measured) {
			t.Fatalf("failed %s did not sort after measured value", key)
		}
		model.sortReverse = true
		if !model.less(measured, failed) || model.less(failed, measured) {
			t.Fatalf("failed %s moved ahead in reverse order", key)
		}
	}

	left := syntaxTestFile("a.ts", 5, true)
	right := syntaxTestFile("b.ts", 5, true)
	model := Model{sortKey: "score"}
	if !model.less(left, right) || model.less(right, left) {
		t.Fatal("ascending numeric tie is not deterministic")
	}
	model.sortReverse = true
	if !model.less(left, right) || model.less(right, left) {
		t.Fatal("descending numeric tie lost ascending path order")
	}
	model.sortKey = "filename"
	model.sortReverse = false
	if !model.less(left, right) || model.less(right, left) {
		t.Fatal("ascending filename tie is not deterministic")
	}
	model.sortReverse = true
	if !model.less(right, left) || model.less(left, right) {
		t.Fatal("descending filename tie is not deterministic")
	}
}

func TestFileDetailShowsMatchingDiagnosticsAndFailedComponentsAsX(t *testing.T) {
	file := syntaxTestFile("broken.ts", 0, false)
	model := Model{
		selected: file.Path,
		document: report.Document{
			Files: []report.File{file},
			Diagnostics: []map[string]any{
				{"path": file.Path, "code": "typescript.syntax.12", "message": "Unexpected token", "line": float64(12), "column": float64(5)},
				{"path": "other.ts", "code": "other", "message": "do not show"},
				{"code": "global", "message": "do not show"},
			},
		},
	}
	text := ansi.Strip(strings.Join(detailContent(model, file, 60), "\n"))
	for _, want := range []string{"score X", "Cognitive complexity", "cognitive_complexity  X", "typescript.syntax.12: Unexpected token (12:5)"} {
		if !strings.Contains(text, want) {
			t.Fatalf("file detail missing %q: %q", want, text)
		}
	}
	if strings.Contains(text, "contribution 0") || strings.Contains(text, "do not show") {
		t.Fatalf("file detail showed fake or unrelated data: %q", text)
	}
}

func TestFileDetailWrapsLongDiagnosticForScrolling(t *testing.T) {
	file := syntaxTestFile("broken.ts", 0, false)
	model := Model{
		width: 40, height: 8, selected: file.Path,
		document: report.Document{
			Files: []report.File{file},
			Diagnostics: []map[string]any{{
				"path": file.Path, "code": "typescript.syntax.12",
				"message": strings.Repeat("long parser detail ", 12) + "tail",
				"line":    float64(12),
			}},
		},
	}
	text := ansi.Strip(strings.Join(detailContent(model, file, 20), "\n"))
	if !strings.Contains(text, "tail") {
		t.Fatalf("long diagnostic was not preserved: %q", text)
	}
	if detailMaxOffset(model) == 0 {
		t.Fatal("wrapped diagnostic did not contribute to detail scrolling")
	}
}
