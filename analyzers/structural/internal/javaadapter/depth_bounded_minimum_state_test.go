package javaadapter

import (
	"testing"

	"slopslap.dev/structural/internal/facts"
	"slopslap.dev/structural/internal/metrics"
)

func TestJavaBoundedMinimumStateValidationAndUpdate(t *testing.T) {
	root, adapter := javaTestAdapter(t)
	measure := func(body string) metrics.DepthScore {
		t.Helper()
		writeSource(t, root, "Service.java", "public final class Service { private int n; public Service() {} public void run(int x) { "+body+" } }")
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

	plain := measure("n += x;")
	guarded := measure("if (x < 0) throw new IllegalArgumentException(); n += x;")
	if guarded.H <= plain.H {
		t.Fatalf("validation did not add burden: plain=%+v guarded=%+v", plain, guarded)
	}
	for _, category := range []facts.ObligationCategory{facts.ObligationValidation, facts.ObligationState, facts.ObligationTransform} {
		found := false
		for _, obligation := range guarded.Obligations {
			if obligation.Category == category {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("guarded update missing %s: %+v", category, guarded.Obligations)
		}
	}
}

func TestJavaBoundedMinimumBranchStateHasZeroAlternative(t *testing.T) {
	root, adapter := javaTestAdapter(t)
	measure := func(body string) metrics.DepthScore {
		t.Helper()
		writeSource(t, root, "Service.java", "public final class Service { private int n; public Service() {} public void run(boolean b) { "+body+" } }")
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

	conditional := measure("if (b) { n++; b = false; }")
	loop := measure("while (b) { n++; b = false; }")
	forLoop := measure("for (; b; b = false) { n++; }")
	if conditional.H != 0 || loop.H != conditional.H || forLoop.H != conditional.H {
		t.Fatalf("branch minimum changed: if=%+v while=%+v for=%+v", conditional, loop, forLoop)
	}

	writeSource(t, root, "Service.java", "public final class Service { private int n; public Service() {} public void run(boolean b) { if (b) return; n++; } }")
	program, err := adapter.Analyze(root, []string{"Service.java"}, map[string]any{"depth_profile": "responsibility-v4"})
	if err != nil {
		t.Fatal(err)
	}
	direct := scoreForJavaBoundary(metrics.MeasureDepth(program), "Service")
	if direct == nil || !direct.Estimated || direct.Shallow == nil || direct.State != facts.KnowledgeMeasured || direct.H != 0 {
		t.Fatalf("direct zero alternative changed: %+v", direct)
	}

	writeSource(t, root, "Service.java", "public final class Service { private int n; public Service() {} private void bump() { n++; } public void run(boolean b) { if (b) return; bump(); } }")
	program, err = adapter.Analyze(root, []string{"Service.java"}, map[string]any{"depth_profile": "responsibility-v4"})
	if err != nil {
		t.Fatal(err)
	}
	helper := scoreForJavaBoundary(metrics.MeasureDepth(program), "Service")
	if helper == nil || !helper.Estimated || helper.Shallow == nil || helper.State != facts.KnowledgeMeasured || helper.H != direct.H {
		t.Fatalf("helper zero alternative changed: %+v", helper)
	}
}

func TestJavaBoundedMinimumPrivateStateHelperAndLocalAreEquivalent(t *testing.T) {
	root, adapter := javaTestAdapter(t)
	measure := func(body string) metrics.DepthScore {
		t.Helper()
		source := "public final class Service { private int n; public Service() {} private int bump() { return ++n; } public int run() { " + body + " } }"
		writeSource(t, root, "Service.java", source)
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

	direct := measure("return bump();")
	local := measure("int y = bump(); return y;")
	if direct.H != local.H {
		t.Fatalf("private state helper changed through local: direct=%+v local=%+v", direct, local)
	}
}
