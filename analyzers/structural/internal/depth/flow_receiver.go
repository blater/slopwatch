package depth

// A receiver identifies prior storage, not newly acquired ownership. Writes,
// escaping references and calls still need their own effect proofs.
func receiverRoot(typ string) AliasRoot {
	return AliasRoot{ID: "receiver:" + typ, Kind: "receiver", Mutable: true, Ownership: OwnershipBorrowed}
}

// Direct field loads on a non-null receiver are observations rather than hidden
// responsibilities. Their storage dependencies remain in the returned recipes.
// Adapters must emit unknown operations for volatile/accessor/possibly failing
// reads; only an explicit field_read has this transfer contract.
func withoutDirectReceiverReads(function *CompiledFunction, evaluation FlowEvaluation) FlowEvaluation {
	receiver, ok := function.ReceiverFormal()
	if !ok {
		return evaluation
	}
	root := receiverRoot(receiver.Type)
	effects := make([]GuardedEffect, 0, len(evaluation.Effects))
	for _, guarded := range evaluation.Effects {
		effect := guarded.Effect
		direct := !effect.Unknown && effect.Kind == "memory_read" && effect.RootID == root.ID &&
			effect.RootKind == root.Kind && len(effect.Path) == 1 && effect.Path[0].Kind == "field" && !effect.Path[0].Dynamic
		if !direct {
			effects = append(effects, guarded)
		}
	}
	evaluation.Effects = effects
	return evaluation
}
