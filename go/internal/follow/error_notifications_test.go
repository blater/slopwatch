package follow

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/blater/slopwatch/internal/report"
	tea "github.com/charmbracelet/bubbletea"
)

func TestFileAnalysisFailureStaysInRowAndInfo(t *testing.T) {
	m := Model{width: 80, height: 24}
	broken := testFile("broken.ts", 0)
	broken.Complete = false
	broken.Coverage = map[string]string{"cognitive_complexity": "failed"}
	diagnostic := map[string]any{"severity": "error", "path": broken.Path, "message": "Unexpected token"}
	doc := report.Document{Files: []report.File{testFile("valid.ts", 0), broken}, Diagnostics: []map[string]any{diagnostic}}
	for i := 0; i < 2; i++ {
		m.Update(analysisResult{full: true, document: doc})
		if len(m.files.Document.Files) != 2 || m.runtimeError != "" || overlayPresent(m.overlays, OverlayRuntimeError) {
			t.Fatal("file diagnostic interrupted or discarded results")
		}
	}
	if marker, _ := rowMarker(m, broken, rowState{}, time.Now()); marker != "!" {
		t.Fatalf("failed file marker = %q", marker)
	}
	if text := strings.Join(fileDiagnosticText(doc, broken), "\n"); !strings.Contains(text, "Unexpected token") {
		t.Fatal("file info lost analysis failure", text)
	}
	if marker, _ := rowMarker(m, doc.Files[0], rowState{}, time.Now()); marker == "!" {
		t.Fatal("unaffected file marked failed")
	}
	m.Update(analysisResult{full: true, document: report.Document{Files: []report.File{testFile("broken.ts", 0)}}})
	if marker, _ := rowMarker(m, m.files.Document.Files[0], rowState{}, time.Now()); marker == "!" {
		t.Fatal("recovered file retained failure marker")
	}
}

func TestIncidentalAnalysisDiagnosticsNeverInterruptOrMarkFiles(t *testing.T) {
	file := testFile("valid.ts", 10)
	file.Complete = false // A published SHALLOW estimate is not an analysis failure.
	file.Coverage = map[string]string{"module_shallowness": "unavailable", "cognitive_complexity": "complete"}
	for _, diagnostic := range []map[string]any{
		{"severity": "error", "code": "typescript.compiler.6059", "path": file.Path, "message": "outside rootDir"},
		{"severity": "warning", "path": file.Path, "message": "incidental warning"},
		{"severity": "info", "path": file.Path, "message": "compiler layout warning", "attributes": map[string]any{"log_only": true}},
	} {
		m := Model{width: 80, height: 24}
		doc := report.Document{Files: []report.File{file}, Diagnostics: []map[string]any{diagnostic}}
		m.Update(analysisResult{full: true, document: doc})
		if m.runtimeError != "" || overlayPresent(m.overlays, OverlayRuntimeError) {
			t.Fatal("incidental analysis diagnostic opened popup")
		}
		if marker, _ := rowMarker(m, file, rowState{}, time.Now()); marker == "!" {
			t.Fatal("incidental diagnostic marked usable file")
		}
		if lines := fileDiagnosticText(doc, file); len(lines) != 0 {
			t.Fatal("incidental diagnostic exposed in info", lines)
		}
		if len(m.files.Document.Diagnostics) != 1 {
			t.Fatal("structured diagnostic ledger discarded")
		}
	}
}

func TestFailedFileInfoExcludesIncidentalDiagnostics(t *testing.T) {
	file := testFile("broken.ts", 0)
	file.Coverage = map[string]string{"cognitive_complexity": "failed"}
	doc := report.Document{Diagnostics: []map[string]any{
		{"path": file.Path, "severity": "error", "message": "parse failed"},
		{"path": file.Path, "severity": "warning", "message": "incidental"},
		{"path": file.Path, "severity": "error", "message": "layout only", "attributes": map[string]any{"log_only": true}},
	}}
	if text := strings.Join(fileDiagnosticText(doc, file), "\n"); text != "parse failed" {
		t.Fatal("info should contain only actual file failure", text)
	}
}

func TestDistinctUnreadErrorsAreRetained(t *testing.T) {
	m := Model{width: 80, height: 24}
	showRuntimeError(&m, errors.New("first error"))
	showRuntimeError(&m, errors.New("second error"))
	showRuntimeError(&m, errors.New("first error"))
	if m.runtimeError != "first error\n\nsecond error" {
		t.Fatal(m.runtimeError)
	}
	if m.overlays.Len() != 1 {
		t.Fatal("stack grew for each error")
	}
}

func TestBackgroundFixCompletionDoesNotPopError(t *testing.T) {
	m := Model{width: 80, height: 24, fixDialog: fixDialogState{generation: 1, starting: true}}
	m.overlays.Push(OverlayFixForm, OverlayCaller{})
	showRuntimeError(&m, errors.New("unrelated analysis error"))
	m.Update(fixStartedMsg{generation: 1, jobID: "job-1"})
	if overlayPresent(m.overlays, OverlayFixForm) {
		t.Fatal("completed form left underneath error")
	}
	if !overlayPresent(m.overlays, OverlayRuntimeError) {
		t.Fatal("background completion dismissed unread error")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m.overlays.Len() != 0 || m.mainView != MainViewAgents {
		t.Fatal("dismissal did not reveal completed transition")
	}
}

func TestCurrentFormErrorsNotifyButStaleMessagesDoNot(t *testing.T) {
	m := Model{width: 80, height: 24, fixDialog: fixDialogState{generation: 2, loading: true}}
	m.overlays.Push(OverlayFixForm, OverlayCaller{})
	m.Update(fixLoadedMsg{generation: 1, err: errors.New("old failure")})
	if m.runtimeError != "" {
		t.Fatal("stale error surfaced")
	}
	m.Update(fixLoadedMsg{generation: 2, err: errors.New("prepare failed")})
	if m.runtimeError != "prepare failed" {
		t.Fatal("current error not surfaced")
	}
}

func TestSourceReadFailureShowsErrorWithoutClosingSource(t *testing.T) {
	m := Model{width: 80, height: 24, source: sourceState{view: true, path: "a.go", loadGeneration: 1}}
	m.Update(sourceLoaded{generation: 1, path: "a.go", viewport: newSourceViewport(60, 20), err: errors.New("source no longer exists")})
	if m.runtimeError != "source no longer exists" || !m.source.view {
		t.Fatal("source error lost")
	}
}
