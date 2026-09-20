package javaadapter

import (
	"reflect"
	"slopslap.dev/structural/internal/facts"
	"slopslap.dev/structural/internal/metrics"
	"testing"
)

func TestJavaDepthScalarSourceToScore(t *testing.T) {
	root, adapter := javaTestAdapter(t)
	cases := []struct {
		name, body string
		score      int
		known      bool
	}{
		{"identity", "return x;", 100, true},
		{"transform", "return x+1;", 30, true},
		{"local", "int result = x+1; return result;", 30, true},
		{"division", "return x/2;", 30, true},
		{"unresolved", "return missing(x);", 0, false},
		{"branch", "if (x>0) return x; return -x;", 30, true},
		{"conditional", "return x>0 ? x : -x;", 30, true},
		{"longPromotion", "if (x<=0) return 0; return x-1;", 30, true},
	}
	// Valid independent packages share one JVM per profile. Keep unresolved
	// source isolated so compiler recovery cannot affect the other fixtures.
	var paths []string
	for _, test := range cases {
		if test.known {
			path := test.name + "/Service.java"
			writeSource(t, root, path, "package "+test.name+"; public final class Service { private Service() {} public static int run(int x) {"+test.body+"} }")
			paths = append(paths, path)
		}
	}
	analyze := func(paths []string) []metrics.DepthScore {
		t.Helper()
		legacy, err := adapter.Analyze(root, paths, nil)
		if err != nil {
			t.Fatal(err)
		}
		program, err := adapter.Analyze(root, paths, map[string]any{"depth_profile": "responsibility-v4"})
		if err != nil {
			t.Fatal(err)
		}
		if len(program.Failures) != 0 {
			t.Fatalf("semantic failure treated as syntax: %+v", program.Failures)
		}
		depth := program.Depth
		program.Depth = nil
		if !reflect.DeepEqual(program, legacy) {
			t.Fatal("opt-in attribution changed legacy facts")
		}
		program.Depth = depth
		scores := metrics.MeasureDepth(program)
		if len(scores) != len(paths) {
			t.Fatalf("scores %+v", scores)
		}
		return scores
	}
	scores := analyze(paths)
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if !test.known {
				writeSource(t, root, "Service.java", "public final class Service { private Service() {} public static int run(int x) {"+test.body+"} }")
				score := analyze([]string{"Service.java"})[0]
				if score.Shallow != nil || score.State != facts.KnowledgePartial {
					t.Fatalf("unsupported source measured: %+v", score)
				}
				return
			}
			score := scoreForJavaBoundary(scores, test.name+".Service")
			if score == nil || score.State != facts.KnowledgeMeasured || score.Shallow == nil || *score.Shallow != test.score {
				t.Fatalf("score %+v", score)
			}
		})
	}
}

func TestJavaDepthShortCircuitAndUnknownBranchesRemainExplicit(t *testing.T) {
	root, adapter := javaTestAdapter(t)
	cases := []struct {
		name, body string
		known      bool
	}{
		{"shortCircuit", "if (enabled && x>0) return x; return -x;", true},
		{"unknownCondition", "if (missing(x)) return x; return -x;", false},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			writeSource(t, root, "Service.java", "public final class Service { private Service() {} public static int run(int x, boolean enabled) {"+test.body+"} }")
			program, err := adapter.Analyze(root, []string{"Service.java"}, map[string]any{"depth_profile": "responsibility-v4"})
			if err != nil {
				t.Fatal(err)
			}
			scores := metrics.MeasureDepth(program)
			if len(scores) != 1 {
				t.Fatalf("scores %+v", scores)
			}
			if test.known {
				if scores[0].State != facts.KnowledgeMeasured || scores[0].Shallow == nil {
					t.Fatalf("supported branch was not measured: %+v", scores[0])
				}
			} else if scores[0].Shallow != nil || scores[0].State != facts.KnowledgePartial {
				t.Fatalf("unknown branch was measured: %+v", scores[0])
			}
		})
	}
}

func TestJavaDepthBooleanShortCircuitReturn(t *testing.T) {
	root, adapter := javaTestAdapter(t)
	writeSource(t, root, "Service.java", "public final class Service { private Service() {} public static boolean run(boolean a, boolean b) { return a && b; } }")
	program, err := adapter.Analyze(root, []string{"Service.java"}, map[string]any{"depth_profile": "responsibility-v4"})
	if err != nil {
		t.Fatal(err)
	}
	scores := metrics.MeasureDepth(program)
	if len(scores) != 1 || scores[0].State != facts.KnowledgeMeasured || scores[0].Shallow == nil {
		t.Fatalf("short-circuit return was not measured: %+v", scores)
	}
}

func TestJavaDepthLongPromotionInBranches(t *testing.T) {
	root, adapter := javaTestAdapter(t)
	writeSource(t, root, "Service.java", "public final class Service { private Service() {} public static long run(long x) { if (x <= 0) return 0; return x - 1; } }")
	program, err := adapter.Analyze(root, []string{"Service.java"}, map[string]any{"depth_profile": "responsibility-v4"})
	if err != nil {
		t.Fatal(err)
	}
	scores := metrics.MeasureDepth(program)
	if len(scores) != 1 || scores[0].State != facts.KnowledgeMeasured || scores[0].Shallow == nil {
		t.Fatalf("long promotion branch was not measured: %+v", scores)
	}
}

func TestJavaDepthConstantBitwiseSource(t *testing.T) {
	root, adapter := javaTestAdapter(t)
	writeSource(t, root, "Service.java", "public final class Service { private static final int ALL = 15; private Service() {} public static boolean valid(int permissions) { return (permissions & ~ALL) == 0; } }")
	program, err := adapter.Analyze(root, []string{"Service.java"}, map[string]any{"depth_profile": "responsibility-v4"})
	if err != nil {
		t.Fatal(err)
	}
	scores := metrics.MeasureDepth(program)
	if len(scores) != 1 || scores[0].State != facts.KnowledgeMeasured || scores[0].Shallow == nil {
		t.Fatalf("constant bitwise source was not measured: %+v", scores)
	}
}

func TestJavaDepthIncompleteAndInstanceSurfacesRemainPartial(t *testing.T) {
	root, adapter := javaTestAdapter(t)
	cases := []string{
		"public class Service { private Service(){} public int run(int x) {return x+1;} }",
		"public class Service { private Service(){} public static long run(int x) {return x+1L;} }",
		"public class Service { private Service(){} static { System.out.println(1); } public static int run(int x) {return x+1;} }",
	}
	for _, source := range cases {
		writeSource(t, root, "Service.java", source)
		program, err := adapter.Analyze(root, []string{"Service.java"}, map[string]any{"depth_profile": "responsibility-v4"})
		if err != nil {
			t.Fatal(err)
		}
		scores := metrics.MeasureDepth(program)
		if len(scores) != 1 {
			t.Fatalf("unsupported surface produced unexpected scores: %+v", scores)
		}
		if scores[0].Shallow != nil && !scores[0].Estimated {
			t.Fatalf("unsupported surface became precise: %+v", scores)
		}
		if len(scores[0].PreciseReasons) == 0 {
			t.Fatalf("unsupported surface lost uncertainty evidence: %+v", scores)
		}
	}
}
