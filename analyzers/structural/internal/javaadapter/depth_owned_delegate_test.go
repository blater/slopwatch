package javaadapter

import (
	"slopslap.dev/structural/internal/metrics"
	"testing"
)

func TestJavaBoundedOwnedSourceDelegatePreservesEffects(t *testing.T) {
	root, adapter := javaTestAdapter(t)
	writeSource(t, root, "Worker.java", `final class Worker { private int n; void bump(){ n++; } }`)
	for _, test := range []struct {
		body   string
		hidden uint64
	}{
		{`worker.bump();`, 4},
		{`if(b) return; worker.bump();`, 0},
	} {
		writeSource(t, root, "Service.java", `public final class Service {
   private final Worker worker = new Worker();
   public void run(boolean b){`+test.body+`}
  }`)
		program, err := adapter.Analyze(root, []string{"Service.java", "Worker.java"}, map[string]any{"depth_profile": "responsibility-v4"})
		if err != nil {
			t.Fatal(err)
		}
		score := scoreForJavaBoundary(metrics.MeasureDepth(program), "Service")
		if score == nil || score.Shallow == nil || score.H != test.hidden {
			t.Fatalf("delegate %s: %+v", test.body, score)
		}
	}
}
