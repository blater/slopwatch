package depth

import (
	"context"
	"fmt"
)

func normalizedCompileOptions(options CompileOptions) (CompileOptions, error) {
	limits := []struct {
		value    *int
		fallback int
	}{
		{&options.MaxFunctions, defaultMaxFunctions}, {&options.MaxNodes, defaultMaxNodes},
		{&options.MaxBlocks, defaultMaxBlocks}, {&options.MaxInstructions, defaultMaxInstructions},
		{&options.MaxElements, defaultMaxElements}, {&options.MaxWork, defaultMaxWork},
		{&options.MaxArtifactWork, defaultMaxArtifactWork},
	}
	for _, limit := range limits {
		if *limit.value < 0 {
			return options, fmt.Errorf("flow: invalid compile options: limits must be non-negative")
		}
		if *limit.value == 0 {
			*limit.value = limit.fallback
		}
	}
	return options, nil
}

type compilePreflight struct {
	blocks, instructions, elements, work int
}

func addPreflight(count *compilePreflight, amount int, options CompileOptions, fn, block, instruction string) error {
	count.elements += amount
	count.work = count.blocks + count.instructions + count.elements
	return checkPreflight(*count, options, fn, block, instruction)
}

func checkPreflight(count compilePreflight, options CompileOptions, fn, block, instruction string) error {
	if cancelled(options.Context) {
		return compileFailure(fn, block, instruction, "compile_cancelled", "compilation context was cancelled")
	}
	if count.blocks > options.MaxBlocks {
		return compileFailure(fn, block, instruction, "graph_limit", "block count exceeds compilation bound")
	}
	if count.blocks+count.instructions > options.MaxNodes {
		return compileFailure(fn, block, instruction, "graph_limit", "flow node count exceeds compilation bound")
	}
	if count.instructions > options.MaxInstructions {
		return compileFailure(fn, block, instruction, "graph_limit", "instruction count exceeds compilation bound")
	}
	if count.elements > options.MaxElements {
		return compileFailure(fn, block, instruction, "graph_limit", "graph element count exceeds compilation bound")
	}
	if count.work > options.MaxWork {
		return compileFailure(fn, block, instruction, "graph_limit", "compiler work exceeds compilation bound")
	}
	return nil
}

func cancelled(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	select {
	case <-ctx.Done():
		return true
	default:
		return false
	}
}

func isFormal(c *CompiledFunction, id string) bool {
	if _, ok := c.formals[id]; ok {
		return true
	}
	return c.receiverFormal != "" && c.receiverFormal == id
}

func chargeCompilerWork(c *CompiledFunction, amount int) bool {
	if cancelled(c.ctx) {
		c.cancelled = true
		return false
	}
	c.work += amount
	if c.work > c.workLimit {
		c.workExceeded = true
		return false
	}
	return true
}

func compilationAbort(c *CompiledFunction) error {
	if c.cancelled {
		return compileFailure(c.function.ID, "", "", "compile_cancelled", "compilation context was cancelled")
	}
	if c.workExceeded {
		return compileFailure(c.function.ID, "", "", "graph_limit", "compiler work exceeds compilation bound")
	}
	return nil
}
