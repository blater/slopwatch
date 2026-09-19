package javaadapter

import (
	"testing"

	"slopslap.dev/structural/internal/facts"
	"slopslap.dev/structural/internal/metrics"
)

func TestJavaDepthSameOwnerInstanceHelperCalls(t *testing.T) {
	root, adapter := javaTestAdapter(t)
	cases := []string{
		`public final class Service { public Service() {} private int increment(int x) { return x + 1; } public int run(int x) { return increment(x); } }`,
		`public final class Service { public Service() {} private int increment(int x) { return x + 1; } public int run(int x) { return this.increment(x); } }`,
	}
	for index, source := range cases {
		writeSource(t, root, "Service.java", source)
		program, err := adapter.Analyze(root, []string{"Service.java"}, map[string]any{"depth_profile": "responsibility-v4"})
		if err != nil {
			t.Fatalf("case %d: %v", index, err)
		}
		scores := metrics.MeasureDepth(program)
		if len(scores) != 1 || scores[0].State != facts.KnowledgeMeasured || scores[0].Shallow == nil {
			t.Fatalf("instance helper case %d score = %+v", index, scores)
		}
		writeSource(t, root, "Service.java", `public final class Service { public Service() {} public int run(int x) { return x + 1; } }`)
		inline, err := adapter.Analyze(root, []string{"Service.java"}, map[string]any{"depth_profile": "responsibility-v4"})
		if err != nil {
			t.Fatalf("inline case %d: %v", index, err)
		}
		inlineScores := metrics.MeasureDepth(inline)
		if len(inlineScores) != 1 || inlineScores[0].Shallow == nil || *scores[0].Shallow != *inlineScores[0].Shallow {
			t.Fatalf("helper case %d differs from inline: helper=%+v inline=%+v", index, scores, inlineScores)
		}
	}
}

func TestJavaDepthInstanceHelperCallsRejectUnsafeDispatchAndState(t *testing.T) {
	root, adapter := javaTestAdapter(t)
	cases := []string{
		`public final class Service { private int state; public Service() {} private int increment(int x) { state += x; return state; } public int run(int x) { return increment(x); } }`,
		`public class Service { public Service() {} int increment(int x) { return x + 1; } public int run(int x) { return increment(x); } }`,
		`public final class Service { public Service() {} private int increment(int x) { return increment(x); } public int run(int x) { return increment(x); } }`,
		`public final class Service { private final Service other; public Service(Service other) { this.other = other; } private int increment(int x) { return x + 1; } public int run(int x) { return other.increment(x); } }`,
	}
	for index, source := range cases {
		writeSource(t, root, "Service.java", source)
		program, err := adapter.Analyze(root, []string{"Service.java"}, map[string]any{"depth_profile": "responsibility-v4"})
		if err != nil {
			t.Fatalf("case %d: %v", index, err)
		}
		scores := metrics.MeasureDepth(program)
		if len(scores) != 1 {
			t.Fatalf("unsafe instance helper case %d produced unexpected scores: %+v", index, scores)
		}
		if scores[0].Shallow != nil && !scores[0].Estimated {
			t.Fatalf("unsafe instance helper case %d became precise: %+v", index, scores)
		}
		if len(scores[0].PreciseReasons) == 0 {
			t.Fatalf("unsafe instance helper case %d lost uncertainty evidence: %+v", index, scores)
		}
	}
}

func TestJavaDepthInstanceArgumentErrorDoesNotInvalidatePurity(t *testing.T) {
	root, adapter := javaTestAdapter(t)
	writeSource(t, root, "Service.java", `
public final class Service {
    public Service() {}
    public int run(int x) {
        if (x < 0) throw new IllegalArgumentException();
        return x + 1;
    }
}`)
	program, err := adapter.Analyze(root, []string{"Service.java"}, map[string]any{"depth_profile": "responsibility-v4"})
	if err != nil {
		t.Fatal(err)
	}
	scores := metrics.MeasureDepth(program)
	if len(scores) != 1 || scores[0].State != facts.KnowledgeMeasured || scores[0].Shallow == nil {
		t.Fatalf("argument error invalidated pure instance method: %+v", scores)
	}
}

func TestJavaDepthPrivateUnknownHelperOnlyAffectsCallers(t *testing.T) {
	root, adapter := javaTestAdapter(t)
	for _, modifier := range []string{"", "synchronized "} {
		for _, called := range []bool{false, true} {
			body := "return x + 1;"
			if called {
				body = "return helper(x);"
			}
			writeSource(t, root, "Service.java", "public final class Service { public Service() {} private "+modifier+"int helper(int x) { System.gc(); return x; } public int run(int x) {"+body+"} }")
			program, err := adapter.Analyze(root, []string{"Service.java"}, map[string]any{"depth_profile": "responsibility-v4"})
			if err != nil {
				t.Fatal(err)
			}
			score := scoreForJavaBoundary(metrics.MeasureDepth(program), "Service")
			if score == nil || (called && score.Shallow != nil && !score.Estimated) ||
				(called && len(score.PreciseReasons) == 0) ||
				(!called && (score.Shallow == nil || *score.Shallow != 41)) {
				t.Fatalf("modifier=%q called=%v: %+v", modifier, called, score)
			}
		}
	}
}
