package depth

import "slopslap.dev/structural/internal/facts"

type transferDelta struct {
	values        []DomainValue
	value         *DomainValue
	records       map[string]map[string]DomainValue
	writes        []memoryWrite
	effects       []TransferEffect
	escapes       []AliasRoot
	invalid       []AliasRoot
	unknownMemory bool
	allocations   []AliasRoot
}
type transferStep struct {
	arena     *RecipeArena
	state     *TransferState
	in        facts.Instruction
	budget    transferBudget
	delta     transferDelta
	result    TransferResult
	startWork int
	summaries *functionSummaries
}

func newTransferStep(a *RecipeArena, s *TransferState, in facts.Instruction, o TransferOptions) *transferStep {
	maximum := o.MaxWork
	if maximum == 0 {
		maximum = defaultMaxWork
	}
	return &transferStep{arena: a, state: s, in: in, budget: transferBudget{state: s, context: o.Context, maximum: maximum, sharedCharge: o.sharedCharge}, result: TransferResult{Status: TransferOK}, startWork: s.work, summaries: o.summaries}
}
func (t *transferStep) partial(reason string) {
	t.result.Status = TransferPartial
	t.result.Reasons = appendUniqueStrings(t.result.Reasons, reason)
}
func (t *transferStep) output(v DomainValue) {
	t.delta.value = &v
	if v.Unknown {
		t.partial(firstReason(v.Reasons, "unknown_value"))
	}
}
func (t *transferStep) unknown(reason string) {
	t.partial(reason)
	v := typedUnknown(t.in.Type, suppliedKind(t.in.ValueKind), reason)
	t.delta.value = &v
}
func (t *transferStep) operand(id string) (DomainValue, bool) {
	v, ok := t.state.values[id]
	if !ok {
		return UnknownValue(UnknownRecipeID, "", ReasonUnknownOperand), false
	}
	if !t.budget.charge(valueWork(v)) {
		return DomainValue{}, false
	}
	return effectiveValue(t.state, v), true
}
func (t *transferStep) effect(kind string, root AliasRoot, value RecipeID) {
	t.delta.effects = append(t.delta.effects, TransferEffect{Kind: kind, RootID: root.ID, RootKind: root.Kind, Ownership: root.Ownership, Mutable: root.Mutable, Path: append([]PathSegment(nil), root.Path...), Value: value, Instruction: t.in.ID})
}
func (t *transferStep) finish() TransferResult {
	t.budget.charge(0)
	if t.budget.status != "" {
		reason := string(t.budget.status)
		if t.budget.status == TransferLimit {
			reason = "work_limit"
		}
		return TransferResult{Status: t.budget.status, Work: t.state.work - t.startWork, Reasons: []string{reason}}
	}
	commitTransfer(t)
	t.result.Work = t.state.work - t.startWork
	return t.result
}
func commitTransfer(t *transferStep) {
	s := t.state
	for _, root := range t.delta.allocations {
		key := aliasKey(root)
		if s.allocations[key] {
			s.multiple[key] = true
		}
		s.allocations[key] = true
	}
	for _, w := range t.delta.writes {
		s.memory.Write(w.Root, nil, w.Value, w.Strong)
	}
	for _, root := range t.delta.escapes {
		s.escaped[aliasKey(root)] = true
	}
	for _, root := range t.delta.invalid {
		s.invalidRoots[aliasKey(root)] = true
	}
	s.unknownMemory = s.unknownMemory || t.delta.unknownMemory
	for id, fields := range t.delta.records {
		s.records[id] = fields
	}
	commitResultValues(t)
	for _, effect := range t.delta.effects {
		s.effects = append(s.effects, effect)
		t.result.Effects = append(t.result.Effects, effect.Copy())
	}
	s.reasons = appendUniqueStrings(s.reasons, t.result.Reasons...)
}

func commitResultValues(t *transferStep) {
	for index, id := range t.in.Results {
		var value DomainValue
		if t.delta.values != nil {
			value = t.delta.values[index]
		} else if t.delta.value != nil {
			value = *t.delta.value
		} else {
			continue
		}
		t.state.values[id] = value.Copy()
		t.result.Values = append(t.result.Values, effectiveValue(t.state, value))
	}
}
