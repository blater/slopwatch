package javaadapter

import (
	"slopslap.dev/structural/internal/facts"
	"slopslap.dev/structural/internal/metrics"
	"testing"
)

func TestJavaDepthStaticHelperCall(t *testing.T) {
	root, adapter := javaTestAdapter(t)
	writeSource(t, root, "Service.java", `
public final class Service {
    private Service() {}
    private static int increment(int x) { return x + 1; }
    public static int run(int x) { return increment(x); }
}`)
	program, err := adapter.Analyze(root, []string{"Service.java"}, map[string]any{"depth_profile": "responsibility-v4"})
	if err != nil {
		t.Fatal(err)
	}
	scores := metrics.MeasureDepth(program)
	if len(scores) != 1 || scores[0].State != facts.KnowledgeMeasured || scores[0].Shallow == nil || *scores[0].Shallow != 30 {
		t.Fatalf("helper call score = %+v", scores)
	}
}

func TestJavaDepthStaticHelperCallRejectionsRemainPartial(t *testing.T) {
	root, adapter := javaTestAdapter(t)
	cases := []string{
		`public final class Service { private Service() {} public static int run(int x) { return Math.abs(x); } }`,
		`public final class Service { private Service() {} private static long widen(long x) { return x; } public static long run(int x) { return widen(x); } }`,
	}
	for _, source := range cases {
		writeSource(t, root, "Service.java", source)
		program, err := adapter.Analyze(root, []string{"Service.java"}, map[string]any{"depth_profile": "responsibility-v4"})
		if err != nil {
			t.Fatal(err)
		}
		scores := metrics.MeasureDepth(program)
		if len(scores) != 1 {
			t.Fatalf("rejected helper call produced unexpected scores: %+v", scores)
		}
		if scores[0].Shallow != nil && !scores[0].Estimated {
			t.Fatalf("rejected helper call became precise: %+v", scores)
		}
		if len(scores[0].PreciseReasons) == 0 {
			t.Fatalf("rejected helper call lost uncertainty evidence: %+v", scores)
		}
	}
}
