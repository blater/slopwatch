package javaadapter

import (
	"strconv"
	"strings"
	"testing"

	"slopslap.dev/structural/internal/facts"
	"slopslap.dev/structural/internal/metrics"
)

func TestJavaDepthOversizedBoundaryDoesNotEraseHealthyBoundary(t *testing.T) {
	root, adapter := javaTestAdapter(t)
	var source strings.Builder
	source.WriteString("package demo; public final class Huge { private Huge() {}")
	// Large identifiers exceed the real transport budget without requiring
	// thousands of method bodies to exercise the same payload-limit path.
	namePadding := strings.Repeat("x", 2048)
	for index := 0; index < 128; index++ {
		source.WriteString(" public static int method")
		source.WriteString(strconv.Itoa(index))
		source.WriteString(namePadding)
		source.WriteString("(int x) { return x; }")
	}
	source.WriteString(" }")
	writeSource(t, root, "Huge.java", source.String())
	writeSource(t, root, "Healthy.java", "package demo; public final class Healthy { private Healthy() {} public static int run(int x) { return x; } }")
	program, err := adapter.Analyze(root, []string{"Huge.java", "Healthy.java"}, map[string]any{"depth_profile": "responsibility-v4"})
	if err != nil {
		t.Fatal(err)
	}
	huge := javaDepthBoundaryBySymbol(t, program, "demo.Huge")
	if huge.State != facts.KnowledgePartial || !hasJavaDepthReasonPrefix(huge, "depth_payload_limit") {
		t.Fatalf("oversized boundary was not explicit partial: %#v", huge)
	}
	healthy := javaDepthBoundaryBySymbol(t, program, "demo.Healthy")
	if healthy.State != facts.KnowledgeMeasured {
		t.Fatalf("healthy boundary was erased by oversized sibling: %#v", healthy)
	}
	scores := metrics.MeasureDepth(program)
	for _, score := range scores {
		if strings.Contains(score.Boundary.Symbol, healthy.Identity.Symbol) && (score.Shallow == nil || score.State != facts.KnowledgeMeasured) {
			t.Fatalf("healthy boundary lost numeric measurement: %+v", score)
		}
	}
}
