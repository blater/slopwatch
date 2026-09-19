package depth

import "sort"

func (e *controlEvaluation) merge(block string, incoming []controlInput) *controlFrame {
	out := &controlFrame{state: NewTransferState()}
	for _, input := range incoming {
		if !e.budget.charge(1) {
			return out
		}
		out.guard = e.guards.or(out.guard, input.guard)
		e.mergeMetadata(out.state, input.state)
		e.mergePending(out, input)
		out.state.memory.parents = append(out.state.memory.parents, memoryParent{input.state.memory, e.guards.recipe(input.guard)})
	}
	out.state.memory.arena = e.arena
	out.state.memory.records = out.state.records
	// Only dominating SSA values can be used without a phi. Branch-local values
	// remain available through each incoming predecessor for simultaneous phis.
	first := incoming[0].state
	keys := make([]string, 0, len(first.values))
	for id := range first.values {
		if !e.budget.charge(1) {
			return out
		}
		keys = append(keys, id)
	}
	sort.Strings(keys)
	for _, id := range keys {
		value, ok := e.mergeValue(out.state, id, incoming)
		if ok {
			out.state.values[id] = value
		}
	}
	return out
}
func (e *controlEvaluation) mergeValue(state *TransferState, id string, incoming []controlInput) (DomainValue, bool) {
	var value DomainValue
	for i, input := range incoming {
		raw, ok := input.state.values[id]
		if !ok {
			return DomainValue{}, false
		}
		if !e.budget.charge(valueWork(raw) + valueWork(value)) {
			return DomainValue{}, false
		}
		next := effectiveValue(input.state, raw)
		if i == 0 {
			value = next
			continue
		}
		value = selectControlRecord(e.arena, state.records, e.guards.recipe(input.guard), next, value, e.budget.charge)
	}
	return value, true
}
func (e *controlEvaluation) mergeMetadata(out, in *TransferState) {
	for _, maps := range [][2]map[string]bool{{out.escaped, in.escaped}, {out.invalidRoots, in.invalidRoots}, {out.allocations, in.allocations}, {out.multiple, in.multiple}} {
		for key, value := range maps[1] {
			if !e.budget.charge(1) {
				return
			}
			maps[0][key] = maps[0][key] || value
		}
	}
	for id, fields := range in.records {
		if !e.budget.charge(1) {
			return
		}
		out.records[id] = fields
	}
	out.unknownMemory = out.unknownMemory || in.unknownMemory
}
func (e *controlEvaluation) mergePending(out *controlFrame, input controlInput) {
	for _, pending := range input.pending {
		if !e.budget.charge(1) {
			return
		}
		pending.guard = e.guards.and(pending.guard, input.guard)
		if pending.guard != guardFalse {
			out.pending = append(out.pending, pending)
		}
	}
	// One pending origin can reconverge along many graph edges. Intern by origin
	// and union guards, rather than carrying one copy for every execution path.
	merged := map[string]int{}
	unique := out.pending[:0]
	for _, pending := range out.pending {
		if !e.budget.charge(1) {
			return
		}
		key := string(appendString(appendString(nil, pending.origin), string(pending.kind))) + string(appendString(nil, pending.tag))
		if index, ok := merged[key]; ok {
			unique[index].guard = e.guards.or(unique[index].guard, pending.guard)
			continue
		}
		merged[key] = len(unique)
		unique = append(unique, pending)
	}
	out.pending = unique
}
