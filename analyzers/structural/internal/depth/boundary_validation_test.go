package depth

import (
	"context"
	"slopslap.dev/structural/internal/facts"
	"testing"
)

func validationFixture(t *testing.T) (*RecipeArena, FlowEvaluation) {
	a := NewRecipeArena()
	x := mustFormal(t, a, 0, "int")
	guard := mustPrimitive(t, a, "bool", ModeInteger, "<", []RecipeID{x, a.Constant("int", "0")})
	accepted := mustPrimitive(t, a, "bool", ModeBoolean, "!", []RecipeID{guard})
	return a, FlowEvaluation{Status: TransferOK, Completions: []FlowCompletion{
		{Kind: facts.EdgeThrow, Guard: guard},
		{Kind: facts.EdgeNormal, Guard: accepted, Values: []DomainValue{PrimitiveResult(x, "int", KindNumeric, nil)}},
	}}
}

func TestValidationProofEveryBudgetCutoff(t *testing.T) {
	a, evaluation := validationFixture(t)
	full := transferBudget{state: NewTransferState(), maximum: 10000}
	_, _, obligation, reason := validatedNormalFlow(a, evaluation, facts.BoundaryIdentity{}, full.charge)
	if reason != "" || obligation.Category != facts.ObligationValidation {
		t.Fatalf("missing V: %+v %s", obligation, reason)
	}
	for limit := 0; limit < full.state.work; limit++ {
		budget := transferBudget{state: NewTransferState(), maximum: limit}
		_, _, proof, why := validatedNormalFlow(a, evaluation, facts.BoundaryIdentity{}, budget.charge)
		if why == "" || proof.ID != "" || budget.state.work > limit {
			t.Fatalf("cutoff %d fabricated proof: %+v %s", limit, proof, why)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	budget := transferBudget{state: NewTransferState(), maximum: 10000, context: ctx}
	_, _, proof, why := validatedNormalFlow(a, evaluation, facts.BoundaryIdentity{}, budget.charge)
	if why == "" || proof.ID != "" {
		t.Fatal("cancel fabricated proof")
	}
}

func TestValidationRequiresCompleteDisjointDomain(t *testing.T) {
	a, evaluation := validationFixture(t)
	for _, completions := range [][]FlowCompletion{
		evaluation.Completions[:1], evaluation.Completions[1:],
		{evaluation.Completions[0], evaluation.Completions[1], evaluation.Completions[1]},
	} {
		input := evaluation
		input.Completions = completions
		budget := transferBudget{state: NewTransferState(), maximum: 10000}
		_, _, proof, reason := validatedNormalFlow(a, input, facts.BoundaryIdentity{}, budget.charge)
		if reason == "" || proof.ID != "" {
			t.Fatal("incomplete/overlapping exits fabricated V")
		}
	}
}
