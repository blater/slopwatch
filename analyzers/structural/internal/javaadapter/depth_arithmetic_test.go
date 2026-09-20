package javaadapter

import (
	"slopslap.dev/structural/internal/facts"
	"slopslap.dev/structural/internal/metrics"
	"testing"
)

func TestJavaDepthArithmeticDivisionAndRemainder(t *testing.T) {
	root, adapter := javaTestAdapter(t)
	cases := []struct {
		name, source string
	}{
		{"remainder", `public final class Service { private Service() {} public static int run(int x) { return x % 2; } }`},
		{"signedLiteral", `public final class Service { private Service() {} public static int run(int x) { return x % -2; } }`},
		{"constantDivisor", `public final class Service { private static final int TWO = 2; private Service() {} public static int run(int x) { return x / TWO; } }`},
		{"longMinWrap", `public final class Service { private Service() {} public static long run(long x) { return x / -1L; } }`},
	}
	// The scalar source-to-score test already covers division by two.
	var paths []string
	for _, test := range cases {
		path := test.name + "/Service.java"
		writeSource(t, root, path, "package "+test.name+"; "+test.source)
		paths = append(paths, path)
	}
	program, err := adapter.Analyze(root, paths, map[string]any{"depth_profile": "responsibility-v4"})
	if err != nil {
		t.Fatal(err)
	}
	scores := metrics.MeasureDepth(program)
	if len(scores) != len(cases) {
		t.Fatalf("arithmetic scores = %+v", scores)
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			score := scoreForJavaBoundary(scores, test.name+".Service")
			if score == nil || score.State != facts.KnowledgeMeasured || score.Shallow == nil || *score.Shallow != 30 {
				t.Fatalf("arithmetic score = %+v", score)
			}
		})
	}
}

func TestJavaDepthArithmeticUnknownDivisorsRemainPartial(t *testing.T) {
	root, adapter := javaTestAdapter(t)
	cases := []string{
		`public final class Service { private Service() {} public static int run(int x) { return x / 0; } }`,
		`public final class Service { private Service() {} public static int run(int x) { int zero = 0; return x / zero; } }`,
		`public final class Service { private Service() {} public static int run(int x, int divisor) { return x % divisor; } }`,
		`public final class Service { private static final int TWO = 2; private Service() {} private static Service factory() { return new Service(); } public static int run(int x) { return x / factory().TWO; } }`,
	}
	for _, source := range cases {
		writeSource(t, root, "Service.java", source)
		program, err := adapter.Analyze(root, []string{"Service.java"}, map[string]any{"depth_profile": "responsibility-v4"})
		if err != nil {
			t.Fatal(err)
		}
		scores := metrics.MeasureDepth(program)
		if len(scores) != 1 {
			t.Fatalf("unknown divisor produced unexpected scores: %+v", scores)
		}
		if scores[0].Shallow != nil && !scores[0].Estimated {
			t.Fatalf("unknown divisor became precise: %+v", scores)
		}
		if len(scores[0].PreciseReasons) == 0 {
			t.Fatalf("unknown divisor lost uncertainty evidence: %+v", scores)
		}
	}
}
