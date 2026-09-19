package javaadapter

import (
	"testing"

	"slopslap.dev/structural/internal/facts"
	"slopslap.dev/structural/internal/metrics"
)

func TestJavaBoundedMinimumTryFinallyRetainsPendingReturn(t *testing.T) {
	root, adapter := javaTestAdapter(t)
	measure := func(body string) metrics.DepthScore {
		t.Helper()
		writeSource(t, root, "Service.java", "public final class Service { private int n; public Service() {} public int run(boolean b) { "+body+" } }")
		program, err := adapter.Analyze(root, []string{"Service.java"}, map[string]any{"depth_profile": "responsibility-v4"})
		if err != nil {
			t.Fatal(err)
		}
		score := scoreForJavaBoundary(metrics.MeasureDepth(program), "Service")
		if score == nil || !score.Estimated || score.Shallow == nil || score.State != facts.KnowledgeMeasured {
			t.Fatalf("expected bounded numeric score: %+v", score)
		}
		return *score
	}

	withFinally := measure("try { return 1; } finally { n++; }")
	direct := measure("n++; return 1;")
	if withFinally.H != direct.H || *withFinally.Shallow != *direct.Shallow {
		t.Fatalf("finally state effect changed pending return: try=%+v direct=%+v", withFinally, direct)
	}
	nested := measure("try { return 1; } finally { try {} finally { n++; } }")
	if nested.H != direct.H {
		t.Fatalf("nested finally lost pending completion: %+v", nested)
	}
	early := measure("if(b) return 1; try { return 1; } finally { n++; }")
	if early.H != 0 {
		t.Fatalf("finally executed on a path that never entered try: %+v", early)
	}
}

func TestJavaBoundedMinimumTryCatchMatchesKnownThrow(t *testing.T) {
	root, adapter := javaTestAdapter(t)
	writeSource(t, root, "Service.java", "public final class Service { private int n; public Service() {} public void run() { try { throw new IllegalArgumentException(); } catch (IllegalArgumentException e) { n++; } } }")
	program, err := adapter.Analyze(root, []string{"Service.java"}, map[string]any{"depth_profile": "responsibility-v4"})
	if err != nil {
		t.Fatal(err)
	}
	score := scoreForJavaBoundary(metrics.MeasureDepth(program), "Service")
	if score == nil || !score.Estimated || score.Shallow == nil || score.State != facts.KnowledgeMeasured || score.H != 4 {
		t.Fatalf("expected caught throw to retain the increment: %+v", score)
	}
}

func TestJavaBoundedMinimumTryCatchLeavesUnmatchedExceptional(t *testing.T) {
	root, adapter := javaTestAdapter(t)
	writeSource(t, root, "Service.java", "public final class Service { private int n; public Service() {} public void run() { try { throw new IllegalArgumentException(); } catch (NullPointerException e) { n++; } } }")
	program, err := adapter.Analyze(root, []string{"Service.java"}, map[string]any{"depth_profile": "responsibility-v4"})
	if err != nil {
		t.Fatal(err)
	}
	score := scoreForJavaBoundary(metrics.MeasureDepth(program), "Service")
	if score == nil || score.Shallow == nil || !score.Estimated || score.H != 0 {
		t.Fatalf("unmatched handler must not earn responsibility: %+v", score)
	}
}
