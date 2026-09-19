package javaadapter

import (
	"testing"

	"slopslap.dev/structural/internal/facts"
	"slopslap.dev/structural/internal/metrics"
)

func TestJavaDepthArgumentErrorContractsPreserveValidatedFlow(t *testing.T) {
	root, adapter := javaTestAdapter(t)
	cases := []struct {
		name, body string
		wantH      uint64
	}{
		{"throwThenIdentity", "if (x < 0) throw new IllegalArgumentException(); return x;", 1},
		{"throwThenTransform", "if (x < 0) throw new IllegalArgumentException(\"negative\"); return x + 1;", 3},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			writeSource(t, root, "Validation.java", "public final class Validation { private Validation() {} public static int check(int x) {"+test.body+"} }")
			program, err := adapter.Analyze(root, []string{"Validation.java"}, map[string]any{"depth_profile": "responsibility-v4"})
			if err != nil {
				t.Fatal(err)
			}
			scores := metrics.MeasureDepth(program)
			if len(scores) != 1 || scores[0].State != facts.KnowledgeMeasured || !scores[0].HKnown || scores[0].H != test.wantH {
				t.Fatalf("argument error score = %+v", scores)
			}
			assertJavaArgumentErrorFlow(t, program, test.name == "throwThenTransform")
		})
	}
}

func assertJavaArgumentErrorFlow(t *testing.T, program *facts.Program, wantMessage bool) {
	t.Helper()
	allocation, throwing := false, false
	for _, artifact := range program.Depth.Flows {
		for _, function := range artifact.Functions {
			for _, block := range function.Blocks {
				for _, instruction := range block.Instructions {
					if instruction.Opcode == facts.OpAllocate && instruction.ValueKind == facts.FlowKindError {
						allocation = true
						if len(instruction.Provenance) != 1 || instruction.Provenance[0].RuleID != "java.argument_error.empty" && instruction.Provenance[0].RuleID != "java.argument_error.message" {
							t.Fatalf("error allocation provenance = %#v", instruction.Provenance)
						}
						if (len(instruction.FieldBindings) == 1) != wantMessage {
							t.Fatalf("error allocation fields = %#v, want message=%v", instruction.FieldBindings, wantMessage)
						}
					}
					if instruction.Opcode == facts.OpThrow && len(instruction.Operands) == 1 {
						throwing = true
					}
				}
			}
		}
	}
	if !allocation || !throwing {
		t.Fatalf("argument error flow allocation=%v throw=%v", allocation, throwing)
	}
}

func TestJavaDepthArgumentErrorRejectsUnregisteredShapes(t *testing.T) {
	root, adapter := javaTestAdapter(t)
	cases := []string{
		`public final class Validation { private Validation() {} public static int check(int x) { if (x < 0) throw new IllegalArgumentException((Throwable) null); return x; } }`,
		`public final class Validation { private Validation() {} public static int check(int x) { if (x < 0) throw new IllegalArgumentException(System.setProperty("key", "value")); return x; } }`,
		`public final class Validation { static class IllegalArgumentException extends RuntimeException {} private Validation() {} public static int check(int x) { if (x < 0) throw new IllegalArgumentException(); return x; } }`,
	}
	for index, source := range cases {
		writeSource(t, root, "Validation.java", source)
		program, err := adapter.Analyze(root, []string{"Validation.java"}, map[string]any{"depth_profile": "responsibility-v4"})
		if err != nil {
			t.Fatalf("case %d: %v", index, err)
		}
		scores := metrics.MeasureDepth(program)
		if len(scores) != 1 || scores[0].State != facts.KnowledgePartial || scores[0].Shallow != nil {
			t.Fatalf("unregistered argument error was measured in case %d: %+v", index, scores)
		}
	}
}

func TestJavaDepthArgumentErrorAcceptsCompileTimeMessage(t *testing.T) {
	root, adapter := javaTestAdapter(t)
	writeSource(t, root, "Validation.java", `public final class Validation {
    private static final String MESSAGE = "negative";
    private Validation() {}
    public static int check(int x) { if (x < 0) throw new IllegalArgumentException(MESSAGE); return x; }
}`)
	program, err := adapter.Analyze(root, []string{"Validation.java"}, map[string]any{"depth_profile": "responsibility-v4"})
	if err != nil {
		t.Fatal(err)
	}
	scores := metrics.MeasureDepth(program)
	if len(scores) != 1 || scores[0].State != facts.KnowledgeMeasured || !scores[0].HKnown || scores[0].H != 1 || scores[0].Shallow == nil {
		t.Fatalf("compile-time message was not measured: %+v", scores)
	}
}

func TestJavaDepthArgumentErrorPropagatesThroughStaticHelper(t *testing.T) {
	root, adapter := javaTestAdapter(t)
	writeSource(t, root, "Validation.java", `public final class Validation {
    private Validation() {}
    private static int helper(int x) { if (x < 0) throw new IllegalArgumentException(); return x; }
    public static int check(int x) { return helper(x); }
}`)
	program, err := adapter.Analyze(root, []string{"Validation.java"}, map[string]any{"depth_profile": "responsibility-v4"})
	if err != nil {
		t.Fatal(err)
	}
	score := scoreForJavaBoundary(metrics.MeasureDepth(program), "Validation")
	if score == nil || score.State != facts.KnowledgeMeasured || !score.HKnown || score.H != 1 {
		t.Fatalf("helper argument error score = %+v", score)
	}
}
