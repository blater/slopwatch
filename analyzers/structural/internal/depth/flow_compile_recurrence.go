package depth

import (
	"slopslap.dev/structural/internal/facts"
	"sort"
)

// CompiledRecurrence binds one loop-carried phi to its actual entry and
// backedge operands. Metadata cannot substitute an unrelated update expression.
type CompiledRecurrence struct {
	ID, Header, PhiID, ValueID, Type string
	Kind                             facts.FlowValueKind
	Initial, Updates                 map[string]string
	References                       []string
}

func (c *CompiledFunction) RecurrencesSnapshot() []CompiledRecurrence {
	ids := make([]string, 0, len(c.recurrences))
	for id := range c.recurrences {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]CompiledRecurrence, 0, len(ids))
	for _, id := range ids {
		r := c.recurrences[id]
		r.Initial = copyStringBindings(r.Initial)
		r.Updates = copyStringBindings(r.Updates)
		r.References = append([]string(nil), r.References...)
		out = append(out, r)
	}
	return out
}
func copyStringBindings(input map[string]string) map[string]string {
	out := make(map[string]string, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}
func compileRecurrences(c *CompiledFunction) error {
	c.recurrences = map[string]CompiledRecurrence{}
	descriptors := append([]facts.Recurrence(nil), c.function.Recurrences...)
	sort.Slice(descriptors, func(i, j int) bool { return descriptors[i].ID < descriptors[j].ID })
	seenIDs, seenSlots := map[string]bool{}, map[string]bool{}
	for _, r := range descriptors {
		if !chargeCompilerWork(c, 1) {
			return compilationAbort(c)
		}
		if !validFlowID(r.ID) || seenIDs[r.ID] {
			return recurrenceFailure(c, r, "invalid_recurrence_id", r.ID)
		}
		seenIDs[r.ID] = true
		if _, ok := c.blocks[r.LoopHeader]; !ok {
			return recurrenceFailure(c, r, "missing_recurrence_header", r.LoopHeader)
		}
		if !c.reachableSet[r.LoopHeader] {
			continue
		}
		if seenSlots[r.PhiSlot] {
			return recurrenceFailure(c, r, "duplicate_recurrence_slot", r.PhiSlot)
		}
		compiled, err := compileRecurrenceSlot(c, r)
		if err != nil {
			return err
		}
		seenSlots[r.PhiSlot] = true
		c.recurrences[r.ID] = compiled
	}
	return validateRecurrenceReferences(c)
}
func recurrenceFailure(c *CompiledFunction, r facts.Recurrence, code, message string) error {
	return compileFailure(c.function.ID, r.LoopHeader, r.PhiSlot, code, message)
}
