package depth

import "slopslap.dev/structural/internal/facts"

func validateGuardMetadata(c *CompiledFunction) error {
	for _, id := range c.reachable {
		if !chargeCompilerWork(c, 1) {
			return compilationAbort(c)
		}
		block := c.blocks[id].Block
		var condition string
		for _, in := range block.Instructions {
			if in.Opcode == facts.OpBranch && len(in.Operands) == 1 {
				condition = in.Operands[0]
			}
		}
		for _, edge := range effectiveEdges(block) {
			if !chargeCompilerWork(c, 1) {
				return compilationAbort(c)
			}
			if edge.Kind != facts.EdgeTrue && edge.Kind != facts.EdgeFalse {
				continue
			}
			if edge.GuardPolarity != "" && edge.GuardPolarity != string(edge.Kind) {
				return compileFailure(c.function.ID, id, "", "incoherent_guard_polarity", edge.GuardPolarity)
			}
			if edge.Guard == "" {
				c.guardGaps[id+":"+edge.To] = "missing_guard"
				continue
			}
			if edge.GuardPolarity == "" {
				c.guardGaps[id+":"+edge.To] = "missing_guard_polarity"
			}
			if condition != "" && edge.Guard != condition {
				return compileFailure(c.function.ID, id, "", "incoherent_edge_guard", edge.Guard)
			}
		}
	}
	return nil
}
