package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slopslap.dev/structural/internal/metrics"
	"testing"
)

func TestProtocolExplicitDepthV4SourceToScore(t *testing.T) {
	for _, test := range []struct {
		source string
		known  bool
	}{
		{`package p; func Sum(n int) int { s:=0;for i:=0;i<n;i++ {s+=i};return s }`, true},
		{`package p; var state int; func Set(x int) int {state=x;return x}`, false},
		{`package p; func Divide(x,y int) int {return x/y}`, false},
	} {
		root := t.TempDir()
		if err := os.WriteFile(filepath.Join(root, "p.go"), []byte(test.source), 0600); err != nil {
			t.Fatal(err)
		}
		input := request{Type: "request", Version: 1, Invocation: "v4-test", Workspace: root, Units: []unit{{ID: "go", Language: "go", Paths: []string{"p.go"}}}, Components: []component{{ID: "module_shallowness", Version: metrics.DepthV4Definition}}}
		var output bytes.Buffer
		run(input, &output)
		decoder := json.NewDecoder(&output)
		measured, covered := false, false
		for decoder.More() {
			var record map[string]any
			if err := decoder.Decode(&record); err != nil {
				t.Fatal(err)
			}
			switch record["type"] {
			case "measurement":
				measured = true
				if test.known {
					if record["value"] != float64(30) {
						t.Fatalf("wrong score: %+v", record)
					}
				} else if record["value"] != nil {
					t.Fatalf("fabricated score: %+v", record)
				}
			case "coverage":
				covered = true
				if (record["state"] == "complete") != test.known {
					t.Fatalf("false coverage: %+v", record)
				}
			case "terminal":
				if record["status"] != "success" {
					t.Fatalf("fatal partial: %+v", record)
				}
			}
		}
		if !measured || !covered {
			t.Fatal("missing measurement or coverage")
		}
		input.Components[0].Version = componentVersions["module_shallowness"]
		if requestStrategies(input).Definitions()["module_shallowness"] != componentVersions["module_shallowness"] {
			t.Fatal("v4 leaked into legacy request")
		}
	}
}
