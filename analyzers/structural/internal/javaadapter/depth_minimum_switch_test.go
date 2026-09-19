package javaadapter

import (
	"testing"

	"slopslap.dev/structural/internal/facts"
	"slopslap.dev/structural/internal/metrics"
)

func TestJavaDepthMinimumSwitchRetainsCaseAlternatives(t *testing.T) {
	root, adapter := javaTestAdapter(t)
	writeSource(t, root, "Counter.java", `public final class Counter {
    private int n;
    private int m;
    public int choose(boolean flag) {
        switch (flag ? 1 : 0) {
            case 1: n++; break;
            default: m++; break;
        }
        return 0;
    }
}`)
	program, err := adapter.Analyze(root, []string{"Counter.java"}, map[string]any{"depth_profile": "responsibility-v4"})
	if err != nil {
		t.Fatal(err)
	}
	scores := metrics.MeasureDepth(program)
	if len(scores) != 1 || scores[0].State != facts.KnowledgeMeasured || !scores[0].Estimated || scores[0].H != 4 {
		t.Fatalf("switch bounded score = %#v", scores)
	}
	if len(scores[0].Alternatives) != 2 || len(scores[0].Alternatives[0]) != 2 {
		t.Fatalf("switch alternatives = %#v", scores[0].Alternatives)
	}
	for _, body := range []struct {
		source string
		h      uint64
	}{
		{"case 1: n++; default: m++; break;", 4},
		{"case 1: n++; break;", 0},
	} {
		writeSource(t, root, "Counter.java", "public final class Counter { private int n,m; public int choose(boolean flag) { switch(flag?1:0) { "+body.source+" } return 0; } }")
		program, err := adapter.Analyze(root, []string{"Counter.java"}, map[string]any{"depth_profile": "responsibility-v4"})
		if err != nil {
			t.Fatal(err)
		}
		scores := metrics.MeasureDepth(program)
		if len(scores) != 1 || scores[0].Shallow == nil || !scores[0].Estimated || scores[0].H != body.h {
			t.Fatalf("fallthrough/no-match transfer: %+v", scores)
		}
	}

	writeSource(t, root, "Counter.java", "public final class Counter { private int n,m; public int choose(boolean flag) { switch(1) { case 1: n++; break; default: break; } return 0; } }")
	constantProgram, err := adapter.Analyze(root, []string{"Counter.java"}, map[string]any{"depth_profile": "responsibility-v4"})
	if err != nil {
		t.Fatal(err)
	}
	constantScores := metrics.MeasureDepth(constantProgram)
	if len(constantScores) != 1 || constantScores[0].H != 4 {
		t.Fatalf("unreachable default diluted constant switch: %+v", constantScores)
	}

}
