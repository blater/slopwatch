package javaadapter

import (
	"testing"

	"slopslap.dev/structural/internal/metrics"
)

// Keep the unrelated reference-valued route in every variant: these pairs must
// exercise the bounded policy rather than accidentally pass on precise scalar flow.
func TestJavaBoundedScoringPreservesBasicIncentives(t *testing.T) {
	root, adapter := javaTestAdapter(t)
	score := func(body, helper string) metrics.DepthScore {
		t.Helper()
		writeSource(t, root, "Service.java", `public final class Service {
   private Object token;
   private int n;
   public int next() { return ++n; }
   public Object token() { return token; }
   public int run(int x) { `+body+` }
   `+helper+`
  }`)
		program, err := adapter.Analyze(root, []string{"Service.java"}, map[string]any{"depth_profile": "responsibility-v4"})
		if err != nil {
			t.Fatal(err)
		}
		scores := metrics.MeasureDepth(program)
		if len(scores) != 1 || scores[0].Shallow == nil || !scores[0].Estimated {
			t.Fatalf("expected numeric bounded estimate: %+v", scores)
		}
		return scores[0]
	}
	cases := []struct{ name, before, after, helper string }{
		{"identicalConditionalArms", "return x;", "return x > 0 ? x : x;", ""},
		{"neutralHelperArgument", "return x;", "return scale(x, 1);", "private int scale(int value, int factor) { return value * factor; }"},
		{"neutralAddition", "return x;", "return x + 0;", ""},
		{"neutralMultiplication", "return x;", "return x * 1;", ""},
		{"unreachableValidation", "return x;", "if (false) { if (x < 0) throw new IllegalArgumentException(); } return x;", ""},
		{"privateExtraction", "return x * 2;", "return doubled(x);", "private int doubled(int x) { return x * 2; }"},
	}
	// Several transformations share the exact same baseline source.
	baselines := make(map[string]metrics.DepthScore)
	for _, test := range cases {
		if _, exists := baselines[test.before]; !exists {
			baselines[test.before] = score(test.before, "")
		}
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			before := baselines[test.before]
			after := score(test.after, test.helper)
			if *before.Shallow != *after.Shallow || before.H != after.H || before.B8 != after.B8 {
				t.Fatalf("refactoring changed score: before=%+v after=%+v", before, after)
			}
		})
	}
}

func TestJavaBoundedStateResponsibility(t *testing.T) {
	root, adapter := javaTestAdapter(t)
	measure := func(body, helper string) metrics.DepthScore {
		t.Helper()
		writeSource(t, root, "Service.java", `public final class Service {
   private int state;
   private boolean closed;
   public Service(int value) { state = value; }
   public int get() { return state; }
   public void run(int x) { `+body+` }
   `+helper+`
  }`)
		program, err := adapter.Analyze(root, []string{"Service.java"}, map[string]any{"depth_profile": "responsibility-v4"})
		if err != nil {
			t.Fatal(err)
		}
		scores := metrics.MeasureDepth(program)
		if len(scores) != 1 || scores[0].Shallow == nil || !scores[0].Estimated {
			t.Fatalf("expected stateful estimate: %+v", scores)
		}
		return scores[0]
	}
	for _, body := range []string{"state = x;", "state += 0;", "state = state * 1;", "if (!closed) closed = false;"} {
		if result := measure(body, ""); result.H != 0 {
			t.Fatalf("passive/neutral state earned credit: %s: %+v", body, result)
		}
	}
	for _, body := range []string{"state += x;", "if (x < 0) throw new IllegalArgumentException(); state = x;"} {
		if result := measure(body, ""); result.H == 0 {
			t.Fatalf("state responsibility missing: %s: %+v", body, result)
		}
	}
	guarded := measure("if (!closed) closed = true;", "")
	if guarded.H != 0 || len(guarded.Alternatives) < 2 || len(guarded.Obligations) == 0 {
		t.Fatalf("guarded transition must retain its responsibility and its no-op alternative: %+v", guarded)
	}
	combined := measure("state = (state + 1) * 2;", "")
	split := measure("state += 1; state *= 2;", "")
	if combined.H != split.H || *combined.Shallow != *split.Shallow {
		t.Fatalf("statement split changed responsibility: combined=%+v split=%+v", combined, split)
	}

	inline := measure("state += x;", "")
	extracted := measure("update(x);", "private void update(int x) { state += x; }")
	if inline.H != extracted.H || *inline.Shallow != *extracted.Shallow {
		t.Fatalf("state helper changed score: %+v / %+v", inline, extracted)
	}
	unused := measure("transform(x);", "private int transform(int x) { return x * 2; }")
	if unused.H != 0 {
		t.Fatalf("unused helper result earned credit: %+v", unused)
	}
}

func TestJavaBoundedDuplicateComputationSharesResponsibility(t *testing.T) {
	root, adapter := javaTestAdapter(t)
	measure := func(extra string) metrics.DepthScore {
		t.Helper()
		writeSource(t, root, "Service.java", `public final class Service {
   private Object token;
   public Object token() { return token; }
   public int first(int x) { return x * 2; }
   `+extra+`
  }`)
		program, err := adapter.Analyze(root, []string{"Service.java"}, map[string]any{"depth_profile": "responsibility-v4"})
		if err != nil {
			t.Fatal(err)
		}
		scores := metrics.MeasureDepth(program)
		if len(scores) != 1 || scores[0].Shallow == nil || !scores[0].Estimated {
			t.Fatalf("expected bounded score: %+v", scores)
		}
		return scores[0]
	}
	single := measure("")
	duplicate := measure("public int second(int renamed) { return renamed * 2; }")
	if duplicate.H != single.H || *duplicate.Shallow < *single.Shallow {
		t.Fatalf("duplicate computation rewarded: single=%+v duplicate=%+v", single, duplicate)
	}
	distinct := measure("public int second(int x) { return x * 3; }")
	if distinct.H <= single.H {
		t.Fatalf("distinct computations collapsed: single=%+v distinct=%+v", single, distinct)
	}
}

func TestJavaPublicInventorySurvivesUnsupportedBehavior(t *testing.T) {
	root, adapter := javaTestAdapter(t)
	inventory := func(extra string) metrics.DepthScore {
		t.Helper()
		writeSource(t, root, "Service.java", `public class Service {
   private int n;
   private int[] data;
   public int doubled(int x) { return x * 2; }
   `+extra+`
  }`)
		program, err := adapter.Analyze(root, []string{"Service.java"}, map[string]any{"depth_profile": "responsibility-v4"})
		if err != nil {
			t.Fatal(err)
		}
		scores := metrics.MeasureDepth(program)
		if len(scores) != 1 || scores[0].Shallow == nil {
			t.Fatalf("missing bounded score: %+v", scores)
		}
		return scores[0]
	}
	base := inventory("")
	for _, method := range []string{
		"public int next() { return ++n; }",
		"public int[] data() { return data; }",
		"public int[] data() { return data.clone(); }",
		"public synchronized int next() { return ++n; }",
	} {
		scored := inventory(method)
		if scored.Burden.O != base.Burden.O+1 || scored.B8 <= base.B8 {
			t.Fatalf("public operation vanished: %s: base=%+v scored=%+v", method, base, scored)
		}
	}
}

func TestJavaFalseTransformationsInBothEvaluators(t *testing.T) {
	root, adapter := javaTestAdapter(t)
	for _, bounded := range []bool{false, true} {
		mode := "precise"
		touch := ""
		if bounded {
			mode = "bounded"
			touch = "private int n; public void touch() { n++; }"
		}
		t.Run(mode, func(t *testing.T) {
			measure := func(body, helper string) metrics.DepthScore {
				t.Helper()
				writeSource(t, root, "Service.java", "public final class Service { private Service() {} "+touch+
					" public static int run(int x) { "+body+" } "+helper+" }")
				program, err := adapter.Analyze(root, []string{"Service.java"}, map[string]any{"depth_profile": "responsibility-v4"})
				if err != nil {
					t.Fatal(err)
				}
				scores := metrics.MeasureDepth(program)
				if len(scores) != 1 || scores[0].Shallow == nil || scores[0].Estimated != bounded {
					t.Fatalf("wrong evaluator or unavailable score: %+v", scores)
				}
				return scores[0]
			}
			for _, pair := range []struct{ name, before, after, helper string }{
				{"identicalAlternatives", "return x;", "return x > 0 ? x : x;", ""},
				{"unusedConnectedArgument", "return 0;", "return twice(0,x);", "private static int twice(int y, int ignored) { return y * 2; }"},
			} {
				t.Run(pair.name, func(t *testing.T) {
					before := measure(pair.before, "")
					after := measure(pair.after, pair.helper)
					if before.H != after.H || before.B8 != after.B8 || *before.Shallow != *after.Shallow {
						t.Fatalf("false transformation: before=%+v after=%+v", before, after)
					}
				})
			}
		})
	}
}

func TestJavaNoArgumentHelperRetainsOwnedStateDependency(t *testing.T) {
	root, adapter := javaTestAdapter(t)
	for _, pair := range []struct{ name, direct, extracted string }{
		{"returnedState", "return n * 2;", "return twice();"},
		{"explicitReceiver", "return n * 2;", "return this.twice();"},
		{"updatedState", "n = n * 2; return n;", "n = twice(); return n;"},
	} {
		t.Run(pair.name, func(t *testing.T) {
			measure := func(body, helper string) metrics.DepthScore {
				t.Helper()
				writeSource(t, root, "Service.java", "public final class Service { private int n; private Service() {} public int run() { "+body+" } "+helper+" }")
				program, err := adapter.Analyze(root, []string{"Service.java"}, map[string]any{"depth_profile": "responsibility-v4"})
				if err != nil {
					t.Fatal(err)
				}
				scores := metrics.MeasureDepth(program)
				if len(scores) != 1 || scores[0].Shallow == nil || scores[0].H == 0 {
					t.Fatalf("missing state dependency: %+v", scores)
				}
				return scores[0]
			}
			direct := measure(pair.direct, "")
			extracted := measure(pair.extracted, "private int twice() { return n * 2; }")
			if direct.H != extracted.H || direct.B8 != extracted.B8 || *direct.Shallow != *extracted.Shallow {
				t.Fatalf("state helper extraction changed score: direct=%+v extracted=%+v", direct, extracted)
			}
		})
	}
}
