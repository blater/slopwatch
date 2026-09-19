package depth

import "slopslap.dev/structural/internal/facts"

func transferCall(t *transferStep) {
	call := t.in.Call
	if t.summaries == nil || call == nil || len(call.Targets) != 1 {
		unknownCall(t, "unresolved_call")
		return
	}
	if !t.budget.charge(1 + len(call.Bindings) + len(call.ResultBindings) + len(call.OwnershipEffects) + len(call.ErrorContinuations)) {
		return
	}
	if len(call.OwnershipEffects) != 0 || len(call.ErrorContinuations) != 0 {
		unknownCall(t, "unsupported_call_contract")
		return
	}
	t.summaries.recordDependencies(call.Targets[0])
	function, ok := t.summaries.artifact.Function(call.Targets[0])
	if !ok {
		unknownCall(t, "unresolved_call")
		return
	}
	actuals, reason := bindCallArguments(t, function)
	if reason != "" {
		unknownCall(t, reason)
		return
	}
	result := t.summaries.evaluate(call.Targets[0])
	t.summaries.recordDependencies(result.Dependencies...)
	completion, rejections, witnesses, reason := instantiateCallCompletions(t, result, function, actuals)
	if reason != "" {
		unknownCall(t, reason)
		return
	}
	t.result.ResourceWitnesses = witnesses
	if completion.Guard == t.arena.Constant("bool", "false") {
		t.result.NormalGuard, t.result.Rejections = completion.Guard, rejections
		return
	}
	if reason := validateCallResults(t, function, completion.Values); reason != "" {
		unknownCall(t, reason)
		return
	}
	t.result.NormalGuard, t.result.Rejections = completion.Guard, rejections
	t.delta.values = completion.Values

}
func bindCallArguments(t *transferStep, function *CompiledFunction) ([]DomainValue, string) {
	formals := function.FormalsSnapshot()
	if _, ok := function.ReceiverFormal(); ok {
		return nil, "unsupported_call_receiver"
	}
	if len(formals) != len(t.in.Call.Bindings) || len(formals) > 32 {
		return nil, "call_binding_arity"
	}
	byFormal := map[string]facts.Binding{}
	for _, binding := range t.in.Call.Bindings {
		if _, exists := byFormal[binding.Formal]; exists {
			return nil, "duplicate_call_binding"
		}
		if binding.Actual == "" || binding.Value != "" {
			return nil, "invalid_call_binding"
		}
		byFormal[binding.Formal] = binding
	}
	actuals := make([]DomainValue, len(formals))
	for index, formal := range formals {
		binding, ok := byFormal[formal.ID]
		if !ok {
			return nil, "missing_call_binding"
		}
		value, ok := t.operand(binding.Actual)
		if !ok {
			return nil, "missing_call_actual"
		}
		if value.Type != formal.Type || value.Kind != suppliedKind(formal.ValueKind) || !summaryScalar(value.Kind) {
			return nil, "call_argument_type"
		}
		actuals[index] = value
	}
	return actuals, ""
}
func unknownCall(t *transferStep, reason string) {
	t.unknown(reason)
	// Until alias/effect summaries are available, unresolved calls can affect
	// prior storage and can fail even if their returned value is discarded.
	t.delta.unknownMemory = true
	t.delta.effects = append(t.delta.effects, TransferEffect{Kind: "unknown", Unknown: true, Reason: reason, Instruction: t.in.ID})
}
