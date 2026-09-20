package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"slopslap.dev/structural/internal/facts"
)

func TestRunDepthEvaluatorSourceNormalizedFlow(t *testing.T) {
	identity := facts.BoundaryIdentity{Artifact: "service", Audience: "external", View: "type", Symbol: "Service"}
	input := depthEvaluatorRequest{SchemaVersion: 1, Depth: &facts.DepthFacts{
		Boundaries: []facts.BoundaryAssessment{{
			Identity: identity, State: facts.KnowledgeMeasured,
			Knowledge: measuredDepthKnowledge(), Burden: facts.Burden{O: 1, T: 1},
			RouteFamilies: []facts.RouteFamily{{ID: "run", Routes: []facts.Route{{
				ID: "run", TargetFunctionID: "run", Boundary: identity,
				RequiredSlots: []string{"arg0"}, ExposedSlots: []string{"arg0"},
			}}}},
		}},
		Flows: []facts.FlowArtifact{{Artifact: "service", Language: "typescript", Functions: []facts.FlowFunction{{
			ID: "run", Entry: "entry",
			Formals: []facts.Formal{{ID: "arg0", Type: "number", ValueKind: facts.FlowKindNumeric}},
			Results: []facts.Formal{{ID: "result0", Type: "number", ValueKind: facts.FlowKindNumeric}},
			Blocks: []facts.FlowBlock{{ID: "entry", Instructions: []facts.Instruction{{
				ID: "return", Opcode: facts.OpReturn, Operands: []string{"arg0"},
			}}}},
		}}}},
	}}
	var encoded, output bytes.Buffer
	if err := json.NewEncoder(&encoded).Encode(input); err != nil {
		t.Fatal(err)
	}
	if err := runDepthEvaluator(&encoded, &output); err != nil {
		t.Fatal(err)
	}
	var response depthEvaluatorResponse
	if err := json.Unmarshal(output.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.SchemaVersion != 1 || len(response.Assessments) != 1 || len(response.Scores) != 1 {
		t.Fatalf("response = %+v", response)
	}
	var streamed bytes.Buffer
	payload, _ := json.Marshal(input)
	if err := runDepthEvaluatorStream(bytes.NewReader(payload), &streamed); err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(&streamed)
	var frame struct {
		Type       string                   `json:"type"`
		Assessment facts.BoundaryAssessment `json:"assessment"`
		Score      json.RawMessage          `json:"score"`
	}
	if err := decoder.Decode(&frame); err != nil {
		t.Fatal(err)
	}
	expected, _ := json.Marshal(response.Scores[0])
	if frame.Type != "score" || !bytes.Equal(frame.Score, expected) {
		t.Fatalf("stream score differs: %s", frame.Score)
	}
	var done struct {
		Type string `json:"type"`
	}
	if err := decoder.Decode(&done); err != nil || done.Type != "done" {
		t.Fatalf("missing terminal: %v", err)
	}
	if response.Scores[0].Shallow == nil || *response.Scores[0].Shallow != 100 {
		t.Fatalf("score = %+v", response.Scores[0])
	}
}

func TestRunDepthEvaluatorRejectsMalformedAndOversizeInput(t *testing.T) {
	for _, input := range []string{
		`{"schema_version":2,"depth":{}}`,
		`{"schema_version":1,"depth":{},"extra":true}`,
		`{"schema_version":1,"depth":{}} {"schema_version":1,"depth":{}}`,
		strings.Repeat("x", depthEvaluatorMaxBytes+1),
	} {
		if err := runDepthEvaluator(strings.NewReader(input), &bytes.Buffer{}); err == nil {
			t.Fatalf("accepted malformed input of length %d", len(input))
		}
	}
}

func measuredDepthKnowledge() map[string]facts.Knowledge {
	return map[string]facts.Knowledge{
		"inventory":     {State: facts.KnowledgeMeasured, Essential: true},
		"burden":        {State: facts.KnowledgeMeasured, Essential: true},
		"behavior":      {State: facts.KnowledgeMeasured, Essential: true},
		"alias_effects": {State: facts.KnowledgeMeasured, Essential: true},
	}
}
