package depth

import (
	"fmt"
	"sort"

	"slopslap.dev/structural/internal/facts"
)

func validateInstruction(fn, block string, in facts.Instruction) error {
	if in.CompletionKind != "" && (in.Opcode != facts.OpReturn || in.CompletionKind != facts.EdgeReturnError) {
		return compileFailure(fn, block, in.ID, "invalid_completion_kind", string(in.CompletionKind))
	}
	if in.Opcode == "" {
		return compileFailure(fn, block, in.ID, "missing_opcode", "opcode is empty")
	}
	if !validOperandArity(in.Opcode, len(in.Operands), len(in.PhiInputs)) {
		return compileFailure(fn, block, in.ID, "invalid_arity", fmt.Sprintf("opcode %s has %d operands", in.Opcode, len(in.Operands)))
	}
	if !validResultCount(in.Opcode, len(in.Results)) {
		return compileFailure(fn, block, in.ID, "invalid_result_arity", fmt.Sprintf("opcode %s has %d results", in.Opcode, len(in.Results)))
	}
	if in.Opcode == facts.OpCall && in.Call != nil {
		if err := validateCallTargets(fn, block, in); err != nil {
			return err
		}
	}
	if in.Opcode == facts.OpPack {
		seen := map[string]bool{}
		for _, field := range in.FieldBindings {
			if field.Field == "" || seen[field.Field] {
				return compileFailure(fn, block, in.ID, "duplicate_field", field.Field)
			}
			seen[field.Field] = true
		}
	}
	return nil
}

func validOperandArity(op facts.Opcode, arity, phi int) bool {
	switch op {
	case facts.OpConstant, facts.OpAllocate, facts.OpLock, facts.OpUnlock:
		return arity == 0
	case facts.OpErrorPresent, facts.OpBind, facts.OpUseResource, facts.OpCleanupAttempt, facts.OpEscape:
		return arity == 1
	case facts.OpFieldRead, facts.OpAcquire:
		return arity <= 1
	case facts.OpFieldWrite:
		return arity == 1 || arity == 2
	case facts.OpPhi:
		return arity > 0 || phi > 0
	case facts.OpPrimitive, facts.OpAtomicRMW:
		return arity > 0
	case facts.OpBranch:
		return arity == 1
	case facts.OpReturn, facts.OpThrow, facts.OpPack, facts.OpCall, facts.OpSelectTarget, facts.OpUnknown:
		return true
	default:
		return false
	}
}

func validateCallTargets(fn, block string, in facts.Instruction) error {
	seen := map[string]bool{}
	for _, target := range in.Call.Targets {
		if !validFlowID(target) || seen[target] {
			return compileFailure(fn, block, in.ID, "invalid_call_target", target)
		}
		seen[target] = true
	}
	return nil
}

func validResultCount(op facts.Opcode, count int) bool {
	switch op {
	case facts.OpCall:
		return true
	case facts.OpErrorPresent, facts.OpBind, facts.OpPhi, facts.OpPrimitive, facts.OpFieldRead, facts.OpConstant:
		return count == 1
	case facts.OpReturn, facts.OpThrow, facts.OpFieldWrite, facts.OpUseResource, facts.OpCleanupAttempt, facts.OpLock, facts.OpUnlock, facts.OpEscape, facts.OpBranch:
		return count == 0
	default:
		return count <= 1
	}
}

func validEdgeKind(kind facts.EdgeKind) bool {
	switch kind {
	case facts.EdgeNormal, facts.EdgeTrue, facts.EdgeFalse, facts.EdgeThrow, facts.EdgeReturnError, facts.EdgePanic, facts.EdgeCleanup, facts.EdgeSuspendResume:
		return true
	default:
		return false
	}
}

func effectiveEdges(block facts.FlowBlock) []facts.FlowEdge {
	if !hasCompletion(block) {
		return append([]facts.FlowEdge(nil), block.Edges...)
	}
	out := make([]facts.FlowEdge, 0, len(block.Edges))
	for _, edge := range block.Edges {
		switch edge.Kind {
		case facts.EdgeCleanup, facts.EdgeThrow, facts.EdgeReturnError, facts.EdgePanic, facts.EdgeSuspendResume:
			out = append(out, edge)
		}
	}
	return out
}

func hasCompletion(block facts.FlowBlock) bool {
	for _, in := range block.Instructions {
		if in.Opcode == facts.OpReturn || in.Opcode == facts.OpThrow {
			return true
		}
	}
	return false
}

func definitionBeforeCompletion(block facts.FlowBlock, index int) bool {
	for i, in := range block.Instructions {
		if (in.Opcode == facts.OpReturn || in.Opcode == facts.OpThrow) && i <= index {
			return false
		}
	}
	return true
}

func activeInstructionCount(block facts.FlowBlock) int {
	for i, in := range block.Instructions {
		if in.Opcode == facts.OpReturn || in.Opcode == facts.OpThrow {
			return i + 1
		}
	}
	return len(block.Instructions)
}

func rebuildReachablePredecessors(c *CompiledFunction) {
	c.predecessors = map[string][]string{}
	c.predecessorSet = map[string]map[string]bool{}
	for _, source := range c.reachable {
		if !chargeCompilerWork(c, 1) {
			return
		}
		for _, edge := range effectiveEdges(c.blocks[source].Block) {
			if !chargeCompilerWork(c, 1) {
				return
			}
			if !c.reachableSet[edge.To] || c.predecessorSet[edge.To][source] {
				continue
			}
			c.predecessors[edge.To] = append(c.predecessors[edge.To], source)
			if c.predecessorSet[edge.To] == nil {
				c.predecessorSet[edge.To] = map[string]bool{}
			}
			c.predecessorSet[edge.To][source] = true
		}
	}
	for target := range c.predecessors {
		sort.Strings(c.predecessors[target])
	}
}
