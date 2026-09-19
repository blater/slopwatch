package depth

import (
	"slopslap.dev/structural/internal/facts"
	"sort"
)

func validateReachableUses(c *CompiledFunction) error {
	discoverBlocks(c)
	if err := compilationAbort(c); err != nil {
		return err
	}
	rebuildReachablePredecessors(c)
	if err := compilationAbort(c); err != nil {
		return err
	}
	pruneDefinitions(c)
	if err := compilationAbort(c); err != nil {
		return err
	}
	computeDominators(c)
	if err := compilationAbort(c); err != nil {
		return err
	}
	for _, id := range c.reachable {
		block := c.blocks[id].Block
		for index, in := range block.Instructions[:activeInstructionCount(block)] {
			if err := validateInstructionRefs(c, id, index, in); err != nil {
				return err
			}
		}
	}
	return validateEdgeRefs(c)
}
func discoverBlocks(c *CompiledFunction) {
	seen := map[string]bool{c.function.Entry: true}
	queue := []string{c.function.Entry}
	for len(queue) > 0 {
		if !chargeCompilerWork(c, 1) {
			return
		}
		id := queue[0]
		queue = queue[1:]
		c.reachable = append(c.reachable, id)
		for _, edge := range effectiveEdges(c.blocks[id].Block) {
			if !chargeCompilerWork(c, 1) {
				return
			}
			if !seen[edge.To] {
				seen[edge.To] = true
				queue = append(queue, edge.To)
			}
		}
	}
	sort.Strings(c.reachable)
	c.reachableSet = seen
}
func pruneDefinitions(c *CompiledFunction) {
	// Compute completion once per block, rather than rescanning its instructions
	// for every definition in the same block.
	completion := make(map[string]int, len(c.reachable))
	for _, id := range c.reachable {
		block := c.blocks[id].Block
		end := len(block.Instructions)
		for index, in := range block.Instructions {
			if !chargeCompilerWork(c, 1) {
				return
			}
			if in.Opcode == facts.OpReturn || in.Opcode == facts.OpThrow {
				end = index
				break
			}
		}
		completion[id] = end
	}
	for id, block := range c.definitionAt {
		if !chargeCompilerWork(c, 1) {
			return
		}
		if !c.reachableSet[block] || c.definitionIndex[id] >= completion[block] {
			delete(c.definitions, id)
			delete(c.definitionAt, id)
			delete(c.definitionIndex, id)
		}
	}
}
func validateInstructionRefs(c *CompiledFunction, blockID string, index int, in facts.Instruction) error {
	refs := instructionReferences(in)
	if in.Opcode == facts.OpPhi {
		if err := validatePhiShape(c, blockID, in); err != nil {
			return err
		}
		refs = phiReferences(in)
	}
	for _, ref := range refs {
		if !chargeCompilerWork(c, 1) {
			return compilationAbort(c)
		}
		if ref == "" {
			continue
		}
		if isFormal(c, ref) {
			continue
		}
		defBlock, ok := c.definitionAt[ref]
		if !ok {
			return compileFailure(c.function.ID, blockID, in.ID, "undefined_operand", ref)
		}
		if in.Opcode == facts.OpPhi {
			continue
		}
		if defBlock == blockID {
			if c.definitionIndex[ref] >= index {
				return compileFailure(c.function.ID, blockID, in.ID, "use_before_definition", ref)
			}
		} else if !dominates(c, defBlock, blockID) {
			return compileFailure(c.function.ID, blockID, in.ID, "non_dominating_definition", ref)
		}
	}
	return validatePhiValues(c, blockID, in)
}
func instructionReferences(in facts.Instruction) []string {
	refs := append([]string(nil), in.Operands...)
	for _, field := range in.FieldBindings {
		refs = appendReference(refs, field.Value)
	}
	if in.Call != nil {
		for _, binding := range in.Call.Bindings {
			refs = appendReference(refs, binding.Actual)
			refs = appendReference(refs, binding.Value)
		}
	}
	for _, binding := range in.Bindings {
		refs = appendReference(refs, binding.Actual)
		refs = appendReference(refs, binding.Value)
	}
	return refs
}
func phiReferences(in facts.Instruction) []string {
	refs := make([]string, 0, len(in.PhiInputs))
	for _, input := range in.PhiInputs {
		refs = append(refs, input.Value)
	}
	return refs
}
func validatePhiShape(c *CompiledFunction, blockID string, in facts.Instruction) error {
	if len(in.PhiInputs) == 0 {
		return compileFailure(c.function.ID, blockID, in.ID, "missing_phi_inputs", "phi requires explicit predecessor/value bindings")
	}
	if len(in.PhiInputs) != len(c.predecessors[blockID]) {
		return compileFailure(c.function.ID, blockID, in.ID, "incomplete_phi_inputs", "phi must name every reachable predecessor")
	}
	seen := map[string]bool{}
	values := map[string]string{}
	for _, input := range in.PhiInputs {
		if !c.predecessorSet[blockID][input.Predecessor] {
			return compileFailure(c.function.ID, blockID, in.ID, "invalid_phi_predecessor", input.Predecessor)
		}
		if seen[input.Predecessor] {
			return compileFailure(c.function.ID, blockID, in.ID, "duplicate_phi_predecessor", input.Predecessor)
		}
		seen[input.Predecessor] = true
		values[input.Predecessor] = input.Value
	}
	c.phiInputs[in.ID] = values
	return nil
}
func validatePhiValues(c *CompiledFunction, blockID string, in facts.Instruction) error {
	if in.Opcode != facts.OpPhi {
		return nil
	}
	for _, input := range in.PhiInputs {
		if isFormal(c, input.Value) {
			continue
		}
		defBlock, ok := c.definitionAt[input.Value]
		if !ok {
			return compileFailure(c.function.ID, blockID, in.ID, "undefined_phi_value", input.Value)
		}
		if !dominates(c, defBlock, input.Predecessor) {
			return compileFailure(c.function.ID, blockID, in.ID, "non_dominating_phi_value", input.Value)
		}
		if defBlock == input.Predecessor && !definitionBeforeCompletion(c.blocks[defBlock].Block, c.definitionIndex[input.Value]) {
			return compileFailure(c.function.ID, blockID, in.ID, "unreachable_phi_value", input.Value)
		}
	}
	return nil
}
func validateEdgeRefs(c *CompiledFunction) error {
	for _, blockID := range c.reachable {
		if !chargeCompilerWork(c, 1) {
			return compilationAbort(c)
		}
		for _, edge := range effectiveEdges(c.blocks[blockID].Block) {
			if !chargeCompilerWork(c, 1) {
				return compilationAbort(c)
			}
			for _, ref := range []string{edge.Guard, edge.Payload} {
				if !chargeCompilerWork(c, 1) {
					return compilationAbort(c)
				}
				if ref == "" {
					continue
				}
				if isFormal(c, ref) {
					continue
				}
				if _, ok := c.definitions[ref]; !ok {
					return compileFailure(c.function.ID, blockID, "", "undefined_edge_value", ref)
				}
				if !dominates(c, c.definitionAt[ref], blockID) {
					return compileFailure(c.function.ID, blockID, "", "non_dominating_edge_value", ref)
				}
			}
		}
	}
	return validateGuardMetadata(c)
}
func appendReference(refs []string, value string) []string {
	if value == "" {
		return refs
	}
	return append(refs, value)
}
