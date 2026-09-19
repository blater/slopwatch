package javaadapter

import (
	"testing"

	"slopslap.dev/structural/internal/facts"
	"slopslap.dev/structural/internal/metrics"
)

func TestJavaDepthValidationBypassSharesForwardedServiceFamily(t *testing.T) {
	root, adapter := javaTestAdapter(t)
	writeSource(t, root, "Probe.java", `
public class Probe {
    private Probe() {}
    private static int core(int x) { return x + 1; }
    public static int checked(int x) { if (x < 0) throw new IllegalArgumentException(); return core(x); }
    public static int raw(int x) { return core(x); }
}`)
	program, err := adapter.Analyze(root, []string{"Probe.java"}, map[string]any{"depth_profile": "responsibility-v4"})
	if err != nil {
		t.Fatal(err)
	}
	score := scoreForJavaBoundary(metrics.MeasureDepth(program), "Probe")
	if score == nil || score.State != facts.KnowledgeMeasured || score.Burden.O != 1 ||
		score.Burden.A != 1 || score.Burden.E != 0 || score.Burden.P != 0 || score.H != 2 {
		t.Fatalf("forwarded validation routes were not normalized: %+v", score)
	}
}

func TestJavaDepthDefaultForwardingNormalizesConstantArgument(t *testing.T) {
	root, adapter := javaTestAdapter(t)
	writeSource(t, root, "Probe.java", `
public class Probe {
    private Probe() {}
    public static int calc(int x, boolean mode) { return mode ? x + 1 : x * 2; }
    public static int calc(int x) { return calc(x, false); }
}`)
	program, err := adapter.Analyze(root, []string{"Probe.java"}, map[string]any{"depth_profile": "responsibility-v4"})
	if err != nil {
		t.Fatal(err)
	}
	score := scoreForJavaBoundary(metrics.MeasureDepth(program), "Probe")
	if score == nil || score.State != facts.KnowledgeMeasured || score.Burden.O != 1 ||
		score.Burden.A != 1 || score.Burden.E != 1 || score.Burden.P != 0 ||
		score.Shallow == nil || *score.Shallow != 37 {
		t.Fatalf("default forwarding route was not normalized: %+v", score)
	}
}

func TestJavaDepthForwardingRejectsComputedUnusedAndEffectfulGuards(t *testing.T) {
	root, adapter := javaTestAdapter(t)
	writeSource(t, root, "Probe.java", `
public class Probe {
    private Probe() {}
    private static int core(int x) { return x + 1; }
    private static boolean audit(int x) { return x < 0; }
    public static int computed(int x) { return core(x + 1); }
    public static int unused(int x, int ignored) { return core(x); }
    public static int assigned(int x) { if ((x=1)<0) throw new IllegalArgumentException(); return core(x); }
    public static int incremented(int x) { if (x++<0) throw new IllegalArgumentException(); return core(x); }
    public static int effectfulGuard(int x) {
        if (audit(x)) throw new IllegalArgumentException();
        return core(x);
    }
}`)
	program, err := adapter.Analyze(root, []string{"Probe.java"}, map[string]any{"depth_profile": "responsibility-v4"})
	if err != nil {
		t.Fatal(err)
	}
	score := scoreForJavaBoundary(metrics.MeasureDepth(program), "Probe")
	if score == nil || score.Burden.O != 5 {
		t.Fatalf("unsafe forwarding routes were grouped: %+v", score)
	}
}
