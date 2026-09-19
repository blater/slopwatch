package depth

import "context"

type transferBudget struct {
	state        *TransferState
	context      context.Context
	maximum      int
	status       TransferStatus
	sharedCharge func(int) bool
}

func (b *transferBudget) charge(n int) bool {
	if b.status != "" {
		return false
	}
	if cancelled(b.context) {
		b.status = TransferCancelled
		return false
	}
	if n < 0 || b.maximum < 0 {
		b.status = TransferUnsupported
		return false
	}
	if n > b.maximum-b.state.work {
		if b.state.work < b.maximum {
			b.state.work = b.maximum
		}
		b.status = TransferLimit
		return false
	}
	if b.sharedCharge != nil && !b.sharedCharge(n) {
		b.status = TransferLimit
		return false
	}
	b.state.work += n
	return true
}
func chargeInstruction(t *transferStep) bool {
	in := t.in
	for _, n := range []int{len(in.Operands), len(in.Results), len(in.Roots), len(in.FieldBindings), len(in.AccessPath), len(in.Effects)} {
		if !t.budget.charge(n) {
			return false
		}
	}
	for _, root := range in.Roots {
		if !t.budget.charge(len(root.PathSegments)) {
			return false
		}
	}
	if in.Value != nil {
		return t.budget.charge(1 + len(in.Value.Concepts) + len(in.Value.MayRoots))
	}
	return true
}
func valueWork(v DomainValue) int {
	n := 1 + len(v.Concepts) + len(v.Dependencies) + len(v.Reasons)
	for _, roots := range [][]AliasRoot{v.Aliases.roots, v.Aliases.evidence} {
		for _, root := range roots {
			n += 1 + len(root.Path)
		}
	}
	return n + len(v.Aliases.reasons)
}
