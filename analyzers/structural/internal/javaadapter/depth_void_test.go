package javaadapter

import (
	"testing"

	"slopslap.dev/structural/internal/facts"
	"slopslap.dev/structural/internal/metrics"
)

func TestJavaDepthVoidNoOpIsApplicable(t *testing.T) {
	root, adapter := javaTestAdapter(t)
	for _, body := range []string{"", "return;", "if (enabled) return;", "if (enabled) return; else return;"} {
		writeSource(t, root, "Service.java", "public final class Service { private Service() {} public static void run(boolean enabled) {"+body+"} }")
		program, err := adapter.Analyze(root, []string{"Service.java"}, map[string]any{"depth_profile": "responsibility-v4"})
		if err != nil {
			t.Fatal(err)
		}
		scores := metrics.MeasureDepth(program)
		if len(scores) != 1 || scores[0].State != facts.KnowledgeMeasured || scores[0].Shallow == nil || *scores[0].Shallow != 100 || scores[0].H != 0 {
			t.Fatalf("void no-op %q must remain applicable without hidden responsibility: %+v", body, scores)
		}
	}
}

func TestJavaDepthVoidUnknownEffectIsNotNoOp(t *testing.T) {
	root, adapter := javaTestAdapter(t)
	writeSource(t, root, "Service.java", `public final class Service {
    private Service() {}
    public static void run() { System.gc(); }
}`)
	program, err := adapter.Analyze(root, []string{"Service.java"}, map[string]any{"depth_profile": "responsibility-v4"})
	if err != nil {
		t.Fatal(err)
	}
	scores := metrics.MeasureDepth(program)
	if len(scores) != 1 {
		t.Fatalf("unsupported void effect produced unexpected scores: %+v", scores)
	}
	if scores[0].Shallow != nil && !scores[0].Estimated {
		t.Fatalf("unsupported void effect became precise: %+v", scores)
	}
	if len(scores[0].PreciseReasons) == 0 {
		t.Fatalf("unsupported void effect lost uncertainty evidence: %+v", scores)
	}
}
