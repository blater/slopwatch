package javaadapter

import (
	"testing"

	"slopslap.dev/structural/internal/facts"
	"slopslap.dev/structural/internal/metrics"
)

func TestJavaDepthPrivateScalarFieldReadIsPrimary(t *testing.T) {
	root, adapter := javaTestAdapter(t)
	writeSource(t, root, "Service.java", `
public final class Service {
    private int n;
    public Service() {}
    public int twice() { return n * 2; }
}`)
	program, err := adapter.Analyze(root, []string{"Service.java"}, map[string]any{"depth_profile": "responsibility-v4"})
	if err != nil {
		t.Fatal(err)
	}
	scores := metrics.MeasureDepth(program)
	if len(scores) != 1 || scores[0].State != facts.KnowledgeMeasured || scores[0].Shallow == nil {
		t.Fatalf("private field read score = %#v", scores)
	}
	if scores[0].Estimated {
		t.Fatalf("private field read used bounded estimate: %#v", scores[0])
	}
	boundary := javaDepthBoundaryBySymbol(t, program, "Service")
	if boundary.State != facts.KnowledgeMeasured {
		t.Fatalf("private field read boundary = %#v", boundary)
	}
}

