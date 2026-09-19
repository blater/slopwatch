package depth

func validateCallResults(t *transferStep, function *CompiledFunction, values []DomainValue) string {
	if len(values) != len(t.in.Results) || len(values) > 32 {
		return "call_result_arity"
	}
	bindings := t.in.Call.ResultBindings
	if len(bindings) == 0 {
		if len(values) > 1 {
			return "missing_result_bindings"
		}
		if len(values) == 1 && (values[0].Type != t.in.Type || values[0].Kind != suppliedKind(t.in.ValueKind) || !summaryResultKind(values[0].Kind)) {
			return "call_result_type"
		}
		return ""
	}
	results := function.function.Results
	if len(bindings) != len(values) || len(results) != len(values) {
		return "call_result_binding_arity"
	}
	seen := map[string]bool{}
	for index, binding := range bindings {
		if !t.budget.charge(1) {
			return "work_limit"
		}
		formal := results[index]
		if formal.ID == "" || seen[formal.ID] || binding.Formal != formal.ID || binding.Actual != t.in.Results[index] || binding.Value != "" {
			return "invalid_result_binding"
		}
		seen[formal.ID] = true
		value := values[index]
		if value.Type != formal.Type || value.Kind != suppliedKind(formal.ValueKind) || !summaryResultKind(value.Kind) {
			return "call_result_type"
		}
	}
	return ""
}
