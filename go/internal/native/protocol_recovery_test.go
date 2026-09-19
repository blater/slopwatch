package native

import (
	"context"
	"errors"
	"github.com/blater/slopwatch/internal/analysiscache"
	"github.com/blater/slopwatch/internal/report"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func recoveryExecutable(t *testing.T, stream string, status string) string {
	t.Helper()
	executable := filepath.Join(t.TempDir(), "analyzer")
	script := "#!/bin/sh\ncat <<'PROTOCOL_RECORDS'\n" + stream + "\nPROTOCOL_RECORDS\necho 'backend detail' >&2\nexit " + status + "\n"
	if err := os.WriteFile(executable, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return executable
}

func TestAnalyzerProcessRetainsValidatedFailureStream(t *testing.T) {
	executable := recoveryExecutable(t, recoveryStream(t, "failure"), "1")
	request := recoveryRequest()
	inputs, err := runAnalyzer(context.Background(), executable, request)
	if !isTerminalFailure(err) {
		t.Fatalf("terminal failure = %v", err)
	}
	recovered, err := recoverAnalyzerInputs(context.Background(), request, inputs, err)
	if err != nil {
		t.Fatal(err)
	}
	if recovered.coverage["good.go"]["cog"] != "complete" {
		t.Fatal("good coverage lost")
	}
	requireRecoveryDiagnostic(t, recovered.diagnostics, "parser.syntax")
	requireRecoveryDiagnostic(t, recovered.diagnostics, "native.terminal_failure")
}

func TestAnalyzerProcessCrashRetainsDiagnosticsAsFailedCoverage(t *testing.T) {
	stream := recoveryStream(t, "success")
	terminal := strings.LastIndex(stream, "{\"type\":\"terminal\"")
	if terminal < 0 {
		t.Fatal("missing terminal fixture")
	}
	executable := recoveryExecutable(t, stream[:terminal], "1")
	request := recoveryRequest()
	inputs, err := runAnalyzer(context.Background(), executable, request)
	if err == nil || !strings.Contains(err.Error(), "backend detail") {
		t.Fatalf("crash error = %v", err)
	}
	recovered, err := recoverAnalyzerInputs(context.Background(), request, inputs, err)
	if err != nil {
		t.Fatal(err)
	}
	if recovered.coverage["good.go"]["cog"] != "failed" {
		t.Fatal("crashed process retained good coverage")
	}
	requireRecoveryDiagnostic(t, recovered.diagnostics, "parser.syntax")
	requireRecoveryDiagnostic(t, recovered.diagnostics, "native.analyzer_failed")
}

func TestAnalyzerProcessMalformedOutputRemainsFatal(t *testing.T) {
	executable := recoveryExecutable(t, "not-json", "1")
	request := recoveryRequest()
	inputs, err := runAnalyzer(context.Background(), executable, request)
	_, recoveredErr := recoverAnalyzerInputs(context.Background(), request, inputs, err)
	var protocolErr *analyzerProtocolError
	if !errors.As(recoveredErr, &protocolErr) {
		t.Fatalf("malformed output = %v", recoveredErr)
	}
}

func TestMalformedProtocolIncludesEarlierParserDetail(t *testing.T) {
	request := recoveryRequest()
	stream := recoveryStream(t, "success")
	_, err := decodeScoreInputs(strings.NewReader(stream+"malformed"), request)
	if err == nil || !strings.Contains(err.Error(), "expected declaration at line 2") {
		t.Fatalf("parser detail absent from fatal error: %v", err)
	}
}

func TestToolFailureLinksDiagnosticToEachFile(t *testing.T) {
	request := recoveryRequest()
	inputs, err := recoverAnalyzerInputs(context.Background(), request, newScoreInputs(), errors.New("backend stderr detail"))
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range request.Units[0].Paths {
		requireFileFailureDetail(t, inputs, path)
	}
}
func requireFileFailureDetail(t *testing.T, inputs scoreInputs, path string) {
	t.Helper()
	for _, diagnostic := range inputs.diagnostics {
		if diagnostic["path"] == path && diagnostic["message"] == "go analyzer failed: backend stderr detail" {
			return
		}
	}
	t.Fatalf("file-linked error missing for %s: %#v", path, inputs.diagnostics)
}

func TestTerminalFailureWithAllFailedCoverageCannotBeReused(t *testing.T) {
	request := recoveryRequest()
	stream := strings.ReplaceAll(recoveryStream(t, "failure"), `"state":"complete"`, `"state":"failed"`)
	executable := recoveryExecutable(t, stream, "1")
	inputs, err := runAnalyzer(context.Background(), executable, request)
	recovered, err := recoverAnalyzerInputs(context.Background(), request, inputs, err)
	if err != nil {
		t.Fatal(err)
	}
	requireRecoveryDiagnostic(t, recovered.diagnostics, "native.terminal_failure")
	artifact := analysiscache.UnitArtifact{Report: report.Document{Diagnostics: recovered.diagnostics}}
	if _, reusable := scoreInputsFromArtifact(artifact, nil, false); reusable {
		t.Fatal("terminal helper failure reused without retry")
	}
}
