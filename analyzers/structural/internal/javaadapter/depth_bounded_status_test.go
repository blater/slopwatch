package javaadapter

import (
	"testing"

	"slopslap.dev/structural/internal/facts"
	"slopslap.dev/structural/internal/metrics"
)

func TestJavaBoundedStatusReturnValidationAndStaticHelper(t *testing.T) {
	root, adapter := javaTestAdapter(t)
	writeSource(t, root, "Status.java", `
package demo;
enum Status { OK, INVALID }
`)
	writeSource(t, root, "Validator.java", `
package demo;
public final class Validator {
    private Validator() {}
    public static Status check(int value) {
        if (value < 0) return Status.INVALID;
        return Status.OK;
    }
}`)
	program, err := adapter.Analyze(root, []string{"Validator.java", "Status.java"}, map[string]any{"depth_profile": "responsibility-v4"})
	if err != nil {
		t.Fatal(err)
	}
	direct := scoreForJavaBoundary(metrics.MeasureDepth(program), "demo.Validator")
	assertBoundedStatus(t, direct)

	writeSource(t, root, "Helper.java", `
package demo;
final class Helper {
    static Status check(int value) {
        if (value < 0) return Status.INVALID;
        return Status.OK;
    }
}`)
	writeSource(t, root, "Validator.java", `
package demo;
public final class Validator {
    private Validator() {}
    public static Status check(int value) { return Helper.check(value); }
}`)
	program, err = adapter.Analyze(root, []string{"Validator.java", "Helper.java", "Status.java"}, map[string]any{"depth_profile": "responsibility-v4"})
	if err != nil {
		t.Fatal(err)
	}
	delegated := scoreForJavaBoundary(metrics.MeasureDepth(program), "demo.Validator")
	assertBoundedStatus(t, delegated)
	if delegated.H != direct.H || delegated.B8 != direct.B8 || *delegated.Shallow != *direct.Shallow {
		t.Fatalf("static status helper changed bounded score: direct=%+v delegated=%+v", direct, delegated)
	}
}

func assertBoundedStatus(t *testing.T, score *metrics.DepthScore) {
	t.Helper()
	if score == nil || score.State != facts.KnowledgeMeasured || !score.Estimated || score.Shallow == nil {
		t.Fatalf("status return was not measured by bounded policy: %+v", score)
	}
	for _, obligation := range score.Obligations {
		if obligation.Category == facts.ObligationValidation {
			return
		}
	}
	t.Fatalf("status return has no validation obligation: %+v", score.Obligations)
}
