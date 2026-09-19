package javaadapter

import (
	"strings"
	"testing"

	"slopslap.dev/structural/internal/facts"
)

func TestJavaDepthValidatedCarrierFlowRetainsGuardAndFieldPayload(t *testing.T) {
	root, adapter := javaTestAdapter(t)
	writeSource(t, root, "Point.java", `public final class Point {
    private final int x;
    public Point(int x) { if (x < 0) throw new IllegalArgumentException(); this.x = x; }
    public int x() { return x; }
}`)
	program, err := adapter.Analyze(root, []string{"Point.java"}, map[string]any{"depth_profile": "responsibility-v4"})
	if err != nil {
		t.Fatal(err)
	}
	if program.Depth == nil || len(program.Depth.Flows) != 1 {
		t.Fatalf("carrier flow = %#v", program.Depth)
	}
	var constructor, accessor *facts.FlowFunction
	for index := range program.Depth.Flows[0].Functions {
		function := &program.Depth.Flows[0].Functions[index]
		if strings.Contains(function.ID, "#<init>(") {
			constructor = function
		}
		if strings.HasSuffix(function.ID, "#x()") {
			accessor = function
		}
	}
	if constructor == nil || accessor == nil {
		t.Fatalf("carrier functions = %#v", program.Depth.Flows[0].Functions)
	}
	if len(constructor.Results) != 1 || constructor.Results[0].Path != "Point#x" {
		t.Fatalf("constructor results = %#v", constructor.Results)
	}
	if !hasFlowOpcode(*constructor, facts.OpThrow) || !hasFlowOpcode(*constructor, facts.OpReturn) {
		t.Fatalf("constructor lost rejection or normal return: %#v", constructor)
	}
	if len(accessor.Formals) != 1 || accessor.Formals[0].ID != "Point#x" ||
		len(accessor.Results) != 1 || !hasFlowOpcode(*accessor, facts.OpReturn) {
		t.Fatalf("accessor flow = %#v", accessor)
	}
}

func hasFlowOpcode(function facts.FlowFunction, opcode facts.Opcode) bool {
	for _, block := range function.Blocks {
		for _, instruction := range block.Instructions {
			if instruction.Opcode == opcode {
				return true
			}
		}
	}
	return false
}
