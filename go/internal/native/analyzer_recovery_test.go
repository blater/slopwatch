package native

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/blater/slopwatch/internal/analysiscache"
	"github.com/blater/slopwatch/internal/report"
	"github.com/blater/slopwatch/internal/unitplan"
)

func recoveryRequest() analyzerRequest {
	return analyzerRequest{Version: 1, Invocation: "test", Units: []protocolUnit{
		{ID: "go-unit", Language: "go", Paths: []string{"good.go", "bad.go"}},
	}, Components: []requestedComponent{{ID: "cog", Version: "1"}}}
}
func recoveryStream(t *testing.T, terminal string) string {
	t.Helper()
	records := []protocolRecord{
		{Type: "diagnostic", Code: "parser.failed", Message: "unable to parse file", Severity: "error"},
		{Type: "diagnostic", Path: recoveryPath("bad.go"), Code: "parser.syntax", Message: "expected declaration at line 2", Severity: "error"},
		{Type: "coverage", UnitID: "go-unit", Path: recoveryPath("bad.go"), Component: "cog", State: "failed"},
		{Type: "coverage", UnitID: "go-unit", Path: recoveryPath("good.go"), Component: "cog", State: "complete"},
		{Type: "measurement", UnitID: "go-unit", Path: recoveryPath("good.go"), Component: "cog", Language: "go", Value: 2},
		{Type: "terminal", Status: terminal, Message: "one unit failed"},
	}
	var stream strings.Builder
	for _, record := range records {
		record.Version, record.Invocation = 1, "test"
		if err := json.NewEncoder(&stream).Encode(record); err != nil {
			t.Fatal(err)
		}
	}
	return stream.String()
}
func recoveryPath(path string) *string { return &path }

func TestTerminalFailurePreservesGoodFilesAndAllDiagnostics(t *testing.T) {
	request := recoveryRequest()
	units, err := decodeUnitScoreInputs(strings.NewReader(recoveryStream(t, "failure")), request)
	if !isTerminalFailure(err) {
		t.Fatalf("terminal error = %v", err)
	}
	inputs, err := recoverAnalyzerInputs(context.Background(), request, units["go-unit"], err)
	if err != nil {
		t.Fatal(err)
	}
	if got := inputs.observations["good.go"]["cog"][0].value; got != 2 {
		t.Fatalf("good value = %v", got)
	}
	if inputs.coverage["bad.go"]["cog"] != "failed" {
		t.Fatal("missing failed coverage")
	}
	for _, code := range []string{"parser.failed", "parser.syntax", "native.terminal_failure"} {
		requireRecoveryDiagnostic(t, inputs.diagnostics, code)
	}
}
func requireRecoveryDiagnostic(t *testing.T, diagnostics []map[string]any, code string) {
	t.Helper()
	for _, diagnostic := range diagnostics {
		if diagnostic["code"] == code {
			return
		}
	}
	t.Fatalf("missing diagnostic %s in %#v", code, diagnostics)
}
func TestMalformedProtocolAndCancellationRemainFatal(t *testing.T) {
	request := recoveryRequest()
	for _, suffix := range []string{"{}", "not-json"} {
		inputs, err := decodeScoreInputs(strings.NewReader(recoveryStream(t, "failure")+suffix), request)
		if _, recoveryErr := recoverAnalyzerInputs(context.Background(), request, inputs, err); recoveryErr == nil {
			t.Fatalf("accepted trailing malformed protocol %q", suffix)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := recoverAnalyzerInputs(ctx, request, newScoreInputs(), errors.New("failure")); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation = %v", err)
	}
}
func TestTransientAnalyzerFailureDiscardsMeasurementsAndCannotBeReused(t *testing.T) {
	request := recoveryRequest()
	inputs, err := decodeScoreInputs(strings.NewReader(recoveryStream(t, "success")), request)
	if err != nil {
		t.Fatal(err)
	}
	failed, err := recoverAnalyzerInputs(context.Background(), request, inputs, errors.New("process crashed"))
	if err != nil {
		t.Fatal(err)
	}
	if len(failed.observations) != 0 {
		t.Fatal("tool failure retained provisional measurements")
	}
	for _, path := range request.Units[0].Paths {
		if failed.coverage[path]["cog"] != "failed" {
			t.Fatalf("%s not failed", path)
		}
	}
	artifact := analysiscache.UnitArtifact{Report: report.Document{Diagnostics: failed.diagnostics}}
	if _, reusable := scoreInputsFromArtifact(artifact, nil, false); reusable {
		t.Fatal("transient failure reused")
	}
	requireRecoveryDiagnostic(t, failed.diagnostics, "parser.syntax")
}
func TestMissingLanguageFailureDoesNotCancelOtherLanguages(t *testing.T) {
	catalog := catalogDocument{Components: []componentDescriptor{{ID: "cog", Version: "1", Defaults: componentDefaults{Enabled: true}, Support: map[string]string{"go": "supported", "typescript": "supported"}}}}
	analyzer := &analysisEngine{runUnits: func(ctx context.Context, _ string, request analyzerRequest) (map[string]scoreInputs, error) {
		if request.Units[0].Language == "go" {
			return nil, fmt.Errorf("tool unavailable")
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		inputs := newScoreInputs()
		inputs.coverage["ok.ts"] = map[string]string{"cog": "complete"}
		return map[string]scoreInputs{request.Units[0].ID: inputs}, nil
	}}
	misses := []plannedCacheUnit{
		{plan: unitplan.Unit{ID: "go:unit", Language: unitplan.LanguageGo}, owned: []string{"bad.go"}, analysisPaths: []string{"bad.go"}},
		{plan: unitplan.Unit{ID: "typescript:unit", Language: unitplan.LanguageTypeScript}, owned: []string{"ok.ts"}, analysisPaths: []string{"ok.ts"}},
	}
	result, err := runMissingUnits(analyzer, context.Background(), "", catalog, misses, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if result["go:unit"].coverage["bad.go"]["cog"] != "failed" {
		t.Fatal("missing failed Go coverage")
	}
	if result["typescript:unit"].coverage["ok.ts"]["cog"] != "complete" {
		t.Fatal("TypeScript did not finish")
	}
}
