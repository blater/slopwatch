package javaadapter

import (
	"slopslap.dev/structural/internal/metrics"
	"strings"
	"testing"
)

func TestJavaBoundedShortCircuitEffects(t *testing.T) {
	root, adapter := javaTestAdapter(t)
	measure := func(expression string) metrics.DepthScore {
		t.Helper()
		writeSource(t, root, "Service.java", `public class Service {
   private int n;
   private Service() {}
   public void run(boolean b) { boolean ignored = `+expression+`; }
   private boolean bump() { n++; return true; }
  }`)
		program, err := adapter.Analyze(root, []string{"Service.java"}, map[string]any{"depth_profile": "responsibility-v4"})
		if err != nil {
			t.Fatal(err)
		}
		scores := metrics.MeasureDepth(program)
		if len(scores) != 1 || scores[0].Shallow == nil || !scores[0].Estimated {
			t.Fatalf("expected bounded result: %+v", scores)
		}
		return scores[0]
	}
	base := measure("false")
	for _, expression := range []string{"false && bump()", "true || bump()", "b && bump()", "b || bump()", "false ? bump() : false", "b ? bump() : false"} {
		got := measure(expression)
		if got.H != base.H || *got.Shallow != *base.Shallow {
			t.Fatalf("skipped call earned credit: %s: %+v", expression, got)
		}
	}
	for _, expression := range []string{"true && bump()", "false || bump()", "true ? bump() : false"} {
		got := measure(expression)
		if got.H <= base.H {
			t.Fatalf("executed call lost credit: %s: %+v", expression, got)
		}
	}
}

func TestJavaBoundedUnhandledControlRetainsUncertainty(t *testing.T) {
	root, adapter := javaTestAdapter(t)
	writeSource(t, root, "Service.java", `public class Service {
  private int n; private Service() {}
  public void run(boolean b) { stop: { if(b) return; } n++; }
 }`)
	program, err := adapter.Analyze(root, []string{"Service.java"}, map[string]any{"depth_profile": "responsibility-v4"})
	if err != nil {
		t.Fatal(err)
	}
	scores := metrics.MeasureDepth(program)
	if len(scores) != 1 || scores[0].Shallow == nil || !scores[0].Estimated || scores[0].H != 0 {
		t.Fatalf("unsupported control assumed sequential completion: %+v", scores)
	}
	found := false
	for _, reason := range scores[0].Reasons {
		if strings.Contains(reason.Message, "unsupported control transfer LABELED_STATEMENT") {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing explicit control-flow uncertainty: %+v", scores)
	}
}
