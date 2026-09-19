package javaadapter

import (
	"slopslap.dev/structural/internal/metrics"
	"testing"
)

func TestJavaBoundedReviewRegressions(t *testing.T) {
	root, adapter := javaTestAdapter(t)
	measure := func(body, extra, modifier, returned string) metrics.DepthScore {
		t.Helper()
		writeSource(t, root, "Service.java", "public final class Service { private int n,m; private final Object lock = new Object(); private Service() {} public "+modifier+returned+" run(boolean b, String x) { "+body+" } "+extra+" }")
		program, err := adapter.Analyze(root, []string{"Service.java"}, map[string]any{"depth_profile": "responsibility-v4"})
		if err != nil {
			t.Fatal(err)
		}
		scores := metrics.MeasureDepth(program)
		if len(scores) != 1 || scores[0].Shallow == nil || !scores[0].Estimated {
			t.Fatalf("expected bounded numeric result: %+v", scores)
		}
		return scores[0]
	}
	equal := func(first, second metrics.DepthScore) {
		t.Helper()
		if first.H != second.H || first.B8 != second.B8 || *first.Shallow != *second.Shallow {
			t.Fatalf("equivalent responsibility differs: %+v / %+v", first, second)
		}
	}
	t.Run("monitorIdentity", func(t *testing.T) {
		plain := measure("n++; return n;", "", "", "int")
		fresh := measure("synchronized(new Object()) { n++; } return n;", "", "", "int")
		equal(plain, fresh)
		guarded := measure("n++; return n;", "", "synchronized ", "int")
		if guarded.H <= plain.H {
			t.Fatalf("shared instance monitor lost credit: %+v / %+v", plain, guarded)
		}
		stable := measure("synchronized(lock) { n++; } return 0;", "", "", "int")
		if stable.H <= plain.H {
			t.Fatalf("stable shared field monitor lost credit: %+v", stable)
		}
		unsafe := measure("n++; return n;", "public int unguarded() { return n; }", "synchronized ", "int")
		for _, obligation := range unsafe.Obligations {
			if obligation.Category == "Y" {
				t.Fatalf("unguarded field earned Y: %+v", unsafe)
			}
		}
	})
	t.Run("exclusiveOutcomes", func(t *testing.T) {
		same := measure("if(b) n++; else n++; return 0;", "", "", "int")
		different := measure("if(b) n++; else m++; return 0;", "", "", "int")
		equal(same, different)
		if len(different.Alternatives) != 2 {
			t.Fatalf("lost branch alternatives: %+v", different)
		}
		sequential := measure("if(b) n++; else m++; n++; return 0;", "", "", "int")
		if sequential.H != same.H {
			t.Fatalf("wrong minimum after common suffix: %+v", sequential)
		}
	})
	t.Run("helperLocal", func(t *testing.T) {
		direct := measure("return twice();", "private int twice() { return n*2; }", "", "int")
		local := measure("return twice();", "private int twice() { int y=n*2; return y; }", "", "int")
		equal(direct, local)
		if direct.H == 0 {
			t.Fatal("both helper variants lost credit")
		}
	})
	t.Run("stringConcatenation", func(t *testing.T) {
		zero := measure("return x+0;", "", "", "String")
		one := measure("return x+1;", "", "", "String")
		equal(zero, one)
		if zero.H == 0 {
			t.Fatal("string concatenation lost transformation credit")
		}
	})
}
