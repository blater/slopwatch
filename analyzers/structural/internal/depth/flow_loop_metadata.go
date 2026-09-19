package depth

import "sort"

func combineLoopMetadata(a, b DomainValue) DomainValue {
	a.Concepts = sortedValueStrings(a.Concepts, b.Concepts)
	a.Dependencies = sortedValueStrings(a.Dependencies, b.Dependencies)
	a.Reasons = appendUniqueStrings(a.Reasons, b.Reasons...)
	a.Unknown = a.Unknown || b.Unknown
	return a
}
func propagateLoopMetadata(e *controlEvaluation, p numericRegion, frame *controlFrame, bindings []NumericLoopBinding, metadata map[string]DomainValue, condition RecipeID) {
	updates := map[string]RecipeID{}
	for _, binding := range bindings {
		updates[binding.Slot] = binding.Update
	}
	conditionValue, _ := frame.state.Value(p.body.Guard)
	keys := make([]string, 0, len(frame.state.values))
	for id := range frame.state.values {
		if !e.budget.charge(1) {
			return
		}
		keys = append(keys, id)
	}
	sort.Strings(keys)
	for _, id := range keys {
		value := frame.state.values[id]
		value = connectedLoopMetadata(e, value, updates, metadata, condition, conditionValue)
		frame.state.values[id] = value
		if e.budget.status != "" {
			return
		}
	}
}
func connectedLoopMetadata(e *controlEvaluation, value DomainValue, updates map[string]RecipeID, metadata map[string]DomainValue, condition RecipeID, conditionValue DomainValue) DomainValue {
	pending := []RecipeID{value.Recipe}
	seen := map[RecipeID]bool{}
	for len(pending) != 0 {
		if !e.budget.charge(1) {
			return value
		}
		id := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		if seen[id] {
			continue
		}
		seen[id] = true
		n, ok := e.arena.Lookup(id)
		if !ok {
			continue
		}
		if n.Kind == KindRecurrence {
			if m, ok := metadata[n.RecurrenceID]; ok {
				value = combineLoopMetadata(value, m)
				value = combineLoopMetadata(value, conditionValue)
				pending = append(pending, updates[n.RecurrenceID], condition)
			}
		}
		if !e.budget.charge(len(n.Children) + len(n.Fields)) {
			return value
		}
		pending = append(pending, n.Children...)
		for _, field := range n.Fields {
			pending = append(pending, field.Recipe)
		}
		if n.Kind == KindSelect {
			pending = append(pending, n.Predicate, n.TrueValue, n.FalseValue)
		}
	}
	return value
}

func loopConditionVaries(e *controlEvaluation, condition RecipeID, bindings []NumericLoopBinding) bool {
	changing := map[string]bool{}
	for _, binding := range bindings {
		if !e.budget.charge(1) {
			return false
		}
		ref, _ := e.arena.RecurrenceRef(binding.Type, binding.Slot, nil)
		changing[binding.Slot] = binding.Update != ref && binding.Update != binding.Initial
	}
	return recipeAny(e.arena, condition, e.budget.charge, func(node RecipeNode) bool {
		return node.Kind == KindRecurrence && changing[node.RecurrenceID]
	})
}
