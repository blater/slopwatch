package depth

import (
	"slopslap.dev/structural/internal/facts"
	"sort"
)

func compileRecurrenceSlot(c *CompiledFunction, r facts.Recurrence) (CompiledRecurrence, error) {
	out := CompiledRecurrence{ID: r.ID, Header: r.LoopHeader, ValueID: r.PhiSlot, Initial: map[string]string{}, Updates: map[string]string{}}
	phi, ok := c.definitions[r.PhiSlot]
	if !ok || phi.Opcode != facts.OpPhi || c.definitionAt[r.PhiSlot] != r.LoopHeader {
		return out, recurrenceFailure(c, r, "invalid_recurrence_phi", r.PhiSlot)
	}
	out.PhiID = phi.ID
	out.Type = phi.Type
	out.Kind = phi.ValueKind
	for _, input := range phi.PhiInputs {
		if !chargeCompilerWork(c, 1) {
			return out, compilationAbort(c)
		}
		if dominates(c, r.LoopHeader, input.Predecessor) {
			if r.Definition != "" && input.Value != r.Definition {
				return out, recurrenceFailure(c, r, "recurrence_update_mismatch", input.Value)
			}
			out.Updates[input.Predecessor] = input.Value
		} else {
			out.Initial[input.Predecessor] = input.Value
		}
	}
	if len(out.Initial) == 0 || len(out.Updates) == 0 {
		return out, recurrenceFailure(c, r, "incomplete_recurrence_edges", "requires an entry operand and a backedge operand")
	}
	if !chargeCompilerWork(c, len(r.References)) {
		return out, compilationAbort(c)
	}
	out.References = append([]string(nil), r.References...)
	sort.Strings(out.References)
	for i, ref := range out.References {
		if !validFlowID(ref) {
			return out, recurrenceFailure(c, r, "invalid_recurrence_reference", ref)
		}
		if i > 0 && out.References[i-1] == ref {
			return out, recurrenceFailure(c, r, "duplicate_recurrence_reference", ref)
		}
	}
	return out, nil
}
func validateRecurrenceReferences(c *CompiledFunction) error {
	ids := make([]string, 0, len(c.recurrences))
	for id := range c.recurrences {
		if !chargeCompilerWork(c, 1) {
			return compilationAbort(c)
		}
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		recurrence := c.recurrences[id]
		for _, ref := range recurrence.References {
			if !chargeCompilerWork(c, 1) {
				return compilationAbort(c)
			}
			if _, ok := c.recurrences[ref]; !ok {
				return compileFailure(c.function.ID, recurrence.Header, recurrence.PhiID, "unbound_recurrence_reference", ref)
			}
		}
	}
	return nil
}
