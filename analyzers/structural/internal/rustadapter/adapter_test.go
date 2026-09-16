package rustadapter

import (
	"strings"
	"testing"
)

func TestValidateFactResponsePreservesSyntaxFailures(t *testing.T) {
	response, err := decodeFactResponse(strings.NewReader(`{
		"schema_version":2,
		"program":{"functions":[],"types":[],"public_operations":[],"representation_exposure":[],"files":["valid.rs"],"unavailable":{},"failures":[{"path":"broken.rs","code":"SYNTAX_ERROR","diagnostic":"broken.rs:1:5: unexpected token"}]},
		"error":null
	}`))
	if err != nil {
		t.Fatal(err)
	}
	program, err := validateFactResponse(response)
	if err != nil {
		t.Fatal(err)
	}
	if len(program.Files) != 1 || program.Files[0] != "valid.rs" || len(program.Failures) != 1 {
		t.Fatalf("program = %#v", program)
	}
	if program.Failures[0].Path != "broken.rs" || program.Failures[0].Code != "SYNTAX_ERROR" {
		t.Fatalf("failure = %#v", program.Failures[0])
	}
}
