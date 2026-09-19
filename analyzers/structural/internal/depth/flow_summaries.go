package depth

// One boundary owns a demand-driven summary session. Each body is evaluated once;
// every summary construction and instantiation shares the same work budget.
type functionSummaries struct {
	arena        *RecipeArena
	artifact     *CompiledArtifact
	budget       transferBudget
	options      TransferOptions
	active       map[string]bool
	results      map[string]FlowEvaluation
	current      string
	dependencies map[string][]string
}

func newFunctionSummaries(arena *RecipeArena, artifact *CompiledArtifact, options TransferOptions) *functionSummaries {
	maximum := options.MaxWork
	if maximum == 0 {
		maximum = defaultMaxWork
	}
	s := &functionSummaries{arena: arena, artifact: artifact, options: options, active: map[string]bool{}, results: map[string]FlowEvaluation{}, dependencies: map[string][]string{}}
	s.budget = transferBudget{state: NewTransferState(), context: options.Context, maximum: maximum}
	s.options.summaries = s
	s.options.sharedCharge = s.budget.charge
	return s
}
func (s *functionSummaries) evaluate(id string) FlowEvaluation {
	if !s.budget.charge(1) {
		return FlowEvaluation{Status: s.budget.status, Gaps: []FlowGap{{Reason: string(s.budget.status)}}}
	}
	if result, ok := s.results[id]; ok {
		return result
	}
	if s.active[id] {
		return summaryFailure("recursive_summary_pending")
	}
	function, ok := s.artifact.Function(id)
	if !ok {
		return summaryFailure("missing_route_flow")
	}
	initial, gaps := summaryInitial(s.arena, function)
	s.active[id] = true
	previous := s.current
	s.current = id
	result := EvaluateFlow(s.arena, function, initial, s.options)
	result = withoutDirectReceiverReads(function, result)
	s.current = previous
	delete(s.active, id)
	result.Dependencies = append([]string(nil), s.dependencies[id]...)
	if len(gaps) != 0 {
		if result.Status == TransferOK {
			result.Status = TransferPartial
		}
		result.Gaps = append(result.Gaps, gaps...)
	}
	lifecycle := proveResourceLifecycleWitnesses(s, id, result)
	result.ResourceWitnesses = lifecycle.witnesses
	result.ResourceEffectsProven = lifecycle.allEffectsProven
	for _, reason := range lifecycle.reasons {
		result.Gaps = append(result.Gaps, FlowGap{Reason: reason})
	}
	if len(lifecycle.reasons) != 0 && result.Status == TransferOK {
		result.Status = TransferPartial
	}
	if s.budget.status != "" {
		result.Status = s.budget.status
	}
	s.results[id] = result
	return result
}
func (s *functionSummaries) recordDependencies(ids ...string) {
	if !s.budget.charge(len(ids)) {
		return
	}
	s.dependencies[s.current] = appendUniqueStrings(s.dependencies[s.current], ids...)
}
func summaryInitial(arena *RecipeArena, function *CompiledFunction) (*TransferState, []FlowGap) {
	initial := NewTransferState()
	var gaps []FlowGap
	for index, formal := range function.FormalsSnapshot() {
		recipe, err := arena.Formal(index, formal.Type)
		kind := suppliedKind(formal.ValueKind)
		if err != nil || !summaryScalar(kind) {
			gaps = append(gaps, FlowGap{Reason: "unsupported_formal_proof"})
			continue
		}
		initial.Seed(formal.ID, ScalarValue(recipe, formal.Type, kind))
	}
	if receiver, ok := function.ReceiverFormal(); ok {
		if receiver.Type == "" || suppliedKind(receiver.ValueKind) != KindReference {
			gaps = append(gaps, FlowGap{Reason: "unsupported_receiver_proof"})
		} else {
			root := receiverRoot(receiver.Type)
			recipe, err := arena.Storage(receiver.Type, locationString(root, nil))
			if err != nil {
				gaps = append(gaps, FlowGap{Reason: "unsupported_receiver_proof"})
			} else {
				initial.Seed(receiver.ID, NewValue(recipe, receiver.Type, KindReference, nil, NewAliasSet(root)))
			}
		}
	}
	return initial, gaps
}
func summaryScalar(kind ValueKind) bool {
	return kind == KindNumeric || kind == KindBoolean || kind == KindString
}
func summaryFailure(reason string) FlowEvaluation {
	return FlowEvaluation{Status: TransferPartial, Gaps: []FlowGap{{Reason: reason}}}
}

func summaryResultKind(kind ValueKind) bool { return summaryScalar(kind) || kind == KindError }
