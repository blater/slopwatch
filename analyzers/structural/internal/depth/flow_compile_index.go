package depth

import (
	"fmt"
	"sort"

	"slopslap.dev/structural/internal/facts"
)

func validateFunctionIdentity(fn facts.FlowFunction) error {
	if !validFlowID(fn.ID) {
		return compileFailure(fn.ID, "", "", "invalid_function_id", "function ID is empty or non-canonical")
	}
	if !validFlowID(fn.Entry) {
		return compileFailure(fn.ID, "", "", "invalid_entry", "entry block is empty or non-canonical")
	}
	if len(fn.Results) > 32 {
		return compileFailure(fn.ID, "", "", "result_limit", "more than 32 result bindings")
	}
	seen := map[string]bool{}
	for _, result := range fn.Results {
		if !validFlowID(result.ID) || seen[result.ID] {
			return compileFailure(fn.ID, "", "", "invalid_result_id", result.ID)
		}
		seen[result.ID] = true
	}
	return nil
}

func buildCompiledFunction(source facts.FlowFunction, options CompileOptions, work int) (*CompiledFunction, error) {
	return buildCompiledFunctionOwned(cloneFlowFunction(source), options, work)
}

func buildCompiledFunctionOwned(fn facts.FlowFunction, options CompileOptions, work int) (*CompiledFunction, error) {
	sort.Slice(fn.Blocks, func(i, j int) bool { return fn.Blocks[i].ID < fn.Blocks[j].ID })
	c := newCompiledFunction(fn, options, work)
	if err := indexFormals(c); err != nil {
		return c, err
	}
	if err := indexBlocks(c); err != nil {
		return c, err
	}
	if err := indexBlockContents(c); err != nil {
		return c, err
	}
	if err := validateReachableUses(c); err != nil {
		return c, err
	}
	if err := compileRecurrences(c); err != nil {
		return c, err
	}
	sort.Strings(c.order)
	return c, nil
}

func newCompiledFunction(fn facts.FlowFunction, options CompileOptions, work int) *CompiledFunction {
	return &CompiledFunction{function: fn, work: work, workLimit: options.MaxWork, ctx: options.Context,
		blocks: make(map[string]CompiledBlock), predecessors: make(map[string][]string),
		predecessorSet: make(map[string]map[string]bool),
		definitions:    make(map[string]facts.Instruction), definitionAt: make(map[string]string),
		definitionIndex: make(map[string]int), formals: make(map[string]int),
		phiInputs: make(map[string]map[string]string), guardGaps: make(map[string]string),
		instructionIDs: make(map[string]bool)}
}

func indexFormals(c *CompiledFunction) error {
	for i, formal := range c.function.Formals {
		if !validFlowID(formal.ID) {
			return compileFailure(c.function.ID, "", "", "invalid_formal", fmt.Sprintf("formal %d has no canonical ID", i))
		}
		if _, ok := c.formals[formal.ID]; ok {
			return compileFailure(c.function.ID, "", "", "duplicate_formal", formal.ID)
		}
		c.formals[formal.ID] = i
	}
	if c.function.ReceiverFormal == nil {
		return nil
	}
	formal := *c.function.ReceiverFormal
	if !validFlowID(formal.ID) {
		return compileFailure(c.function.ID, "", "", "invalid_receiver_formal", "receiver formal has no canonical ID")
	}
	if _, ok := c.formals[formal.ID]; ok {
		return compileFailure(c.function.ID, "", "", "receiver_formal_collision", formal.ID)
	}
	c.receiverFormal = formal.ID
	return nil
}

func indexBlocks(c *CompiledFunction) error {
	for i, block := range c.function.Blocks {
		if !validFlowID(block.ID) {
			return compileFailure(c.function.ID, block.ID, "", "invalid_block_id", "block ID is empty or non-canonical")
		}
		if _, ok := c.blocks[block.ID]; ok {
			return compileFailure(c.function.ID, block.ID, "", "duplicate_block", block.ID)
		}
		c.blocks[block.ID] = CompiledBlock{Block: block, Index: i}
		c.order = append(c.order, block.ID)
	}
	if _, ok := c.blocks[c.function.Entry]; !ok {
		return compileFailure(c.function.ID, c.function.Entry, "", "missing_entry", c.function.Entry)
	}
	return nil
}

func indexBlockContents(c *CompiledFunction) error {
	for _, block := range c.function.Blocks {
		if err := indexEdges(c, block); err != nil {
			return err
		}
		if err := indexInstructions(c, block); err != nil {
			return err
		}
	}
	return nil
}

func indexEdges(c *CompiledFunction, block facts.FlowBlock) error {
	for _, edge := range block.Edges {
		if edge.From != block.ID {
			return compileFailure(c.function.ID, block.ID, "", "edge_source_mismatch", edge.From)
		}
		if _, ok := c.blocks[edge.To]; !ok {
			return compileFailure(c.function.ID, block.ID, "", "missing_edge_target", edge.To)
		}
		if !validEdgeKind(edge.Kind) {
			return compileFailure(c.function.ID, block.ID, "", "invalid_edge_kind", string(edge.Kind))
		}
	}
	for _, edge := range effectiveEdges(block) {
		c.predecessors[edge.To] = append(c.predecessors[edge.To], block.ID)
	}
	return nil
}

func indexInstructions(c *CompiledFunction, block facts.FlowBlock) error {
	active := activeInstructionCount(block)
	for index, in := range block.Instructions {
		if !validFlowID(in.ID) {
			return compileFailure(c.function.ID, block.ID, in.ID, "invalid_instruction_id", "instruction ID is empty or non-canonical")
		}
		if c.instructionIDs[in.ID] {
			return compileFailure(c.function.ID, block.ID, in.ID, "duplicate_instruction", in.ID)
		}
		c.instructionIDs[in.ID] = true
		if index >= active {
			continue
		}
		if err := indexResults(c, block, index, in); err != nil {
			return err
		}
		if err := validateInstruction(c.function.ID, block.ID, in); err != nil {
			return err
		}
	}
	return nil
}

func indexResults(c *CompiledFunction, block facts.FlowBlock, index int, in facts.Instruction) error {
	for _, result := range in.Results {
		if isFormal(c, result) {
			return compileFailure(c.function.ID, block.ID, in.ID, "formal_result_collision", result)
		}
		if !validFlowID(result) {
			return compileFailure(c.function.ID, block.ID, in.ID, "invalid_result_id", result)
		}
		if _, exists := c.definitions[result]; exists {
			return compileFailure(c.function.ID, block.ID, in.ID, "duplicate_result", result)
		}
		c.definitions[result], c.definitionAt[result], c.definitionIndex[result] = in, block.ID, index
	}
	return nil
}
