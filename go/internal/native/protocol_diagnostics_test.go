package native

import (
	"encoding/json"
	"testing"
)

func TestDiagnosticClassificationSurvivesProtocolProjection(t *testing.T) {
	var record protocolRecord
	if err := json.Unmarshal([]byte(`{"type":"diagnostic","severity":"info","code":"typescript.compiler.6059","message":"outside rootDir","attributes":{"log_only":true,"classification":"incidental_emit_layout"}}`), &record); err != nil {
		t.Fatal(err)
	}
	record.attachMetadata()
	attributes, ok := record.Raw["attributes"].(map[string]any)
	if !ok || attributes["log_only"] != true || attributes["classification"] != "incidental_emit_layout" || record.Raw["severity"] != "info" {
		t.Fatalf("diagnostic classification lost: %#v", record.Raw)
	}
}
