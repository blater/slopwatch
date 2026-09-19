package depth

import (
	"context"
	"slopslap.dev/structural/internal/facts"
	"testing"
)

func conditionalValidationFixture(t *testing.T) (*RecipeArena, FlowEvaluation) {
	a, result := validationFixture(t)
	mode := mustFormal(t, a, 1, "bool")
	rejection := mustPrimitive(t, a, "bool", ModeBoolean, "&&", []RecipeID{mode, result.Completions[0].Guard})
	accepted := mustPrimitive(t, a, "bool", ModeBoolean, "!", []RecipeID{rejection})
	result.Completions[0].Guard = rejection
	result.Completions[1].Guard = accepted
	return a, result
}

func TestConditionalValidationAllWorkCutoffs(t *testing.T) {
	a, input := conditionalValidationFixture(t)
	full := transferBudget{state: NewTransferState(), maximum: 100000}
	variants, policies, reason := validationFlowVariants(a, input, full.charge)
	if reason != "" || len(variants) != 2 || len(policies) != 0 {
		t.Fatalf("wrong split: %d %v %s", len(variants), policies, reason)
	}
	for limit := 0; limit < full.state.work; limit++ {
		budget := transferBudget{state: NewTransferState(), maximum: limit}
		_, _, reason := validationFlowVariants(a, input, budget.charge)
		if reason == "" || budget.state.work > limit {
			t.Fatalf("cutoff %d passed", limit)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	budget := transferBudget{state: NewTransferState(), maximum: 100000, context: ctx}
	_, _, reason = validationFlowVariants(a, input, budget.charge)
	if reason == "" {
		t.Fatal("cancelled split passed")
	}
}

func TestFailureOnlyVariantIsNotNormalAlternative(t *testing.T) {
	a, input := validationFixture(t)
	input.Completions = []FlowCompletion{{Kind: facts.EdgeThrow, Guard: a.Constant("bool", "true")}}
	budget := transferBudget{state: NewTransferState(), maximum: 10000}
	if !supportedFailureOnly(a, input, budget.charge) {
		t.Fatal("supported rejection not recognized")
	}
	input.Gaps = []FlowGap{{Reason: "missing_effect"}}
	if supportedFailureOnly(a, input, budget.charge) {
		t.Fatal("unknown rejected variant dropped")
	}
}
