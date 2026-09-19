package javaadapter

import (
	"strings"
	"testing"

	"slopslap.dev/structural/internal/facts"
	"slopslap.dev/structural/internal/metrics"
)

func TestJavaDepthSourceHelpersShareWorkBudget(t *testing.T) {
	root, adapter := javaTestAdapter(t)
	// Each helper fits the budget alone; expanding both must consume the same
	// boundary budget rather than resetting it for each recursive scanner.
	body := strings.Repeat("n++;", 5000)
	writeSource(t, root, "Service.java", `public final class Service {
    private static int n;
    private Service() {}
    private static void first() { `+body+` }
    private static void second() { `+body+` }
    public static void run() { first(); second(); }
}`)
	program, err := adapter.Analyze(root, []string{"Service.java"}, map[string]any{"depth_profile": "responsibility-v4"})
	if err != nil {
		t.Fatal(err)
	}
	bounded := javaDepthBoundaryBySymbol(t, program, "Service").BoundedAssessment
	if bounded == nil {
		t.Fatal("missing bounded assessment")
	}
	for _, reason := range bounded.Reasons {
		if reason.Code == "bounded_source_work_limit" {
			return
		}
	}
	t.Fatalf("source helpers did not share their work budget: %+v", bounded.Reasons)
}

func TestJavaDepthSourceStaticHelperMatchesInlineComputation(t *testing.T) {
	root, adapter := javaTestAdapter(t)
	writeSource(t, root, "Service.java", `
public final class Service {
    private static int state;
    private Service() {}
    private static void touch() { state++; }
    public static int run(int x) { touch(); return x * 2; }
}`)
	program, err := adapter.Analyze(root, []string{"Service.java"}, map[string]any{"depth_profile": "responsibility-v4"})
	if err != nil {
		t.Fatal(err)
	}
	direct := scoreForJavaBoundary(metrics.MeasureDepth(program), "Service")
	if direct == nil || direct.State != facts.KnowledgeMeasured || direct.Shallow == nil {
		t.Fatalf("inline source was not measured: %+v", direct)
	}

	writeSource(t, root, "Helper.java", `
final class Helper {
    static int twice(int x) { return x * 2; }
}`)
	writeSource(t, root, "Service.java", `
public final class Service {
    private static int state;
    private Service() {}
    private static void touch() { state++; }
    public static int run(int x) { touch(); return Helper.twice(x); }
}`)
	program, err = adapter.Analyze(root, []string{"Service.java", "Helper.java"}, map[string]any{"depth_profile": "responsibility-v4"})
	if err != nil {
		t.Fatal(err)
	}
	delegated := scoreForJavaBoundary(metrics.MeasureDepth(program), "Service")
	if delegated == nil || delegated.State != facts.KnowledgeMeasured || delegated.Shallow == nil {
		t.Fatalf("source static helper was not measured: %+v", delegated)
	}
	if !direct.Estimated || !delegated.Estimated || delegated.H != direct.H || delegated.B8 != direct.B8 || delegated.Shallow == nil || direct.Shallow == nil || *delegated.Shallow != *direct.Shallow {
		t.Fatalf("source helper changed bounded score: direct=%+v delegated=%+v", direct, delegated)
	}
}

func TestJavaDepthSourceStaticHelperIgnoresUnusedDynamicArgument(t *testing.T) {
	root, adapter := javaTestAdapter(t)
	writeSource(t, root, "Helper.java", `final class Helper { static int twice(int y, int ignored) { return y * 2; } }`)
	var baseline *metrics.DepthScore
	for _, expression := range []string{"0", "Helper.twice(0, x)"} {
		writeSource(t, root, "Service.java", `public final class Service {
   private Service() {} private static int state;
   private static void touch() { state++; }
   public static int run(int x) { touch(); return `+expression+`; }
  }`)
		program, err := adapter.Analyze(root, []string{"Service.java", "Helper.java"}, map[string]any{"depth_profile": "responsibility-v4"})
		if err != nil {
			t.Fatal(err)
		}
		score := scoreForJavaBoundary(metrics.MeasureDepth(program), "Service")
		if score == nil || score.Shallow == nil || !score.Estimated {
			t.Fatalf("missing bounded result: %+v", score)
		}
		if baseline == nil {
			baseline = score
			continue
		}
		if score.H != baseline.H || score.B8 != baseline.B8 || *score.Shallow != *baseline.Shallow {
			t.Fatalf("unused argument earns responsibility: direct=%+v, delegated=%+v", baseline, score)
		}
	}
}
