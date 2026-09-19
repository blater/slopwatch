package depth

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"unicode"

	"slopslap.dev/structural/internal/facts"
)

// CompileError identifies malformed normalized transport without depending on
// map iteration or source order for its text.
type CompileError struct {
	FunctionID    string
	BlockID       string
	InstructionID string
	Code          string
	Message       string
}

func (e *CompileError) Error() string {
	parts := []string{"flow"}
	if e.FunctionID != "" {
		parts = append(parts, "function="+e.FunctionID)
	}
	if e.BlockID != "" {
		parts = append(parts, "block="+e.BlockID)
	}
	if e.InstructionID != "" {
		parts = append(parts, "instruction="+e.InstructionID)
	}
	return strings.Join(parts, ":") + ": " + e.Code + ": " + e.Message
}

type CompiledBlock struct {
	Block facts.FlowBlock
	Index int
}

// CompiledFunction is an immutable canonical graph index. Maps are never
// returned to callers; the evaluator only reads this value.
type CompiledFunction struct {
	recurrences     map[string]CompiledRecurrence
	function        facts.FlowFunction
	blocks          map[string]CompiledBlock
	order           []string
	reachable       []string
	reachableSet    map[string]bool
	predecessors    map[string][]string
	predecessorSet  map[string]map[string]bool
	definitions     map[string]facts.Instruction
	definitionAt    map[string]string
	definitionIndex map[string]int
	formals         map[string]int
	receiverFormal  string
	phiInputs       map[string]map[string]string
	guardGaps       map[string]string
	work            int
	workLimit       int
	workExceeded    bool
	cancelled       bool
	ctx             context.Context
	instructionIDs  map[string]bool
	idom            map[string]string
	rpoIndex        map[string]int
	domPre          map[string]int
	domPost         map[string]int
}

type CompiledArtifact struct {
	artifact  facts.FlowArtifact
	functions map[string]*CompiledFunction
}

type CompilationDiagnostic struct {
	FunctionID string
	Error      error
}

type CompileOptions struct {
	MaxFunctions    int
	MaxNodes        int
	MaxBlocks       int
	MaxInstructions int
	MaxElements     int
	MaxWork         int
	MaxArtifactWork int
	Context         context.Context
}

const (
	defaultMaxFunctions    = 2000
	defaultMaxNodes        = 50000
	defaultMaxBlocks       = 50000
	defaultMaxInstructions = 50000
	defaultMaxElements     = 1000000
	defaultMaxWork         = 1000000
	defaultMaxArtifactWork = 20000000
)

func (c *CompiledFunction) ReceiverFormal() (facts.Formal, bool) {
	if c.function.ReceiverFormal == nil {
		return facts.Formal{}, false
	}
	return *c.function.ReceiverFormal, true
}
func (c *CompiledFunction) CompilerWork() int { return c.work }
func (c *CompiledFunction) FormalsSnapshot() []facts.Formal {
	out := append([]facts.Formal(nil), c.function.Formals...)
	return out
}

func (c *CompiledFunction) FlowFunction() facts.FlowFunction { return cloneFlowFunction(c.function) }
func (c *CompiledFunction) Block(id string) (facts.FlowBlock, bool) {
	item, ok := c.blocks[id]
	if !ok {
		return facts.FlowBlock{}, false
	}
	return cloneFlowBlock(item.Block), true
}
func (c *CompiledFunction) BlocksSnapshot() []facts.FlowBlock {
	out := make([]facts.FlowBlock, 0, len(c.order))
	for _, id := range c.order {
		if item, ok := c.blocks[id]; ok {
			out = append(out, cloneFlowBlock(item.Block))
		}
	}
	return out
}
func (c *CompiledFunction) ReachableBlocks() []string { return append([]string(nil), c.reachable...) }
func (c *CompiledFunction) Predecessors(id string) []string {
	return append([]string(nil), c.predecessors[id]...)
}
func (c *CompiledFunction) Successors(id string) []facts.FlowEdge {
	item, ok := c.blocks[id]
	if !ok {
		return nil
	}
	edges := effectiveEdges(item.Block)
	sort.Slice(edges, func(i, j int) bool { return flowEdgeLess(edges[i], edges[j]) })
	return cloneEdges(edges)
}
func (c *CompiledFunction) PhiInputs(id string) map[string]string {
	out := map[string]string{}
	for predecessor, value := range c.phiInputs[id] {
		out[predecessor] = value
	}
	return out
}
func (c *CompiledFunction) GuardGaps() []string {
	out := make([]string, 0, len(c.guardGaps))
	for id := range c.guardGaps {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}
func (c *CompiledArtifact) FlowArtifact() facts.FlowArtifact { return cloneFlowArtifact(c.artifact) }
func (c *CompiledArtifact) Function(id string) (*CompiledFunction, bool) {
	fn, ok := c.functions[id]
	return fn, ok
}
func (c *CompiledArtifact) FunctionsSnapshot() []string {
	out := make([]string, 0, len(c.functions))
	for id := range c.functions {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

func cloneFlowBlock(block facts.FlowBlock) facts.FlowBlock {
	out := block
	if block.Instructions != nil {
		out.Instructions = make([]facts.Instruction, len(block.Instructions))
		for i := range block.Instructions {
			out.Instructions[i] = cloneInstruction(block.Instructions[i])
		}
	}
	out.Edges = cloneEdges(block.Edges)
	return out
}
func cloneEdges(edges []facts.FlowEdge) []facts.FlowEdge {
	if edges == nil {
		return nil
	}
	out := make([]facts.FlowEdge, len(edges))
	copy(out, edges)
	return out
}
func cloneFlowFunction(fn facts.FlowFunction) facts.FlowFunction {
	out := fn
	out.Formals = cloneSlice(fn.Formals)
	out.Results = cloneSlice(fn.Results)
	if fn.ReceiverFormal != nil {
		receiver := *fn.ReceiverFormal
		out.ReceiverFormal = &receiver
	}
	if fn.Blocks != nil {
		out.Blocks = make([]facts.FlowBlock, len(fn.Blocks))
		for i := range fn.Blocks {
			out.Blocks[i] = cloneFlowBlock(fn.Blocks[i])
		}
	}
	out.Exits = cloneSlice(fn.Exits)
	out.ReturnConcepts = cloneStrings(fn.ReturnConcepts)
	if fn.Recurrences != nil {
		out.Recurrences = make([]facts.Recurrence, len(fn.Recurrences))
		for i := range fn.Recurrences {
			out.Recurrences[i] = fn.Recurrences[i]
			out.Recurrences[i].References = cloneStrings(fn.Recurrences[i].References)
		}
	}
	out.Provenance = cloneProvenance(fn.Provenance)
	return out
}
func cloneFlowArtifact(artifact facts.FlowArtifact) facts.FlowArtifact {
	out := artifact
	if artifact.Functions != nil {
		out.Functions = make([]facts.FlowFunction, len(artifact.Functions))
		for i := range artifact.Functions {
			out.Functions[i] = cloneFlowFunction(artifact.Functions[i])
		}
	}
	if artifact.Types != nil {
		out.Types = make([]facts.FlowType, len(artifact.Types))
		for i := range artifact.Types {
			out.Types[i] = cloneFlowType(artifact.Types[i])
		}
	}
	out.PublicRoutes = cloneRoutes(artifact.PublicRoutes)
	out.Provenance = cloneProvenance(artifact.Provenance)
	return out
}

func validFlowID(id string) bool {
	if id == "" {
		return false
	}
	for _, r := range id {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return false
		}
	}
	return true
}

func compileFailure(fn, block, instruction, code, message string) error {
	return &CompileError{FunctionID: fn, BlockID: block, InstructionID: instruction, Code: code, Message: message}
}

func CompileFlowArtifact(artifact facts.FlowArtifact) (*CompiledArtifact, error) {
	return CompileFlowArtifactWithOptions(artifact, CompileOptions{})
}

func CompileFlowArtifactWithOptions(artifact facts.FlowArtifact, options CompileOptions) (*CompiledArtifact, error) {
	out, diagnostics, err := CompileFlowArtifactPartialWithOptions(artifact, options)
	if err != nil {
		return nil, err
	}
	if len(diagnostics) != 0 {
		return nil, diagnostics[0].Error
	}
	return out, nil
}

// CompileFlowArtifactPartial retains independently valid functions while
// returning located diagnostics for source-local malformed functions.
func CompileFlowArtifactPartial(artifact facts.FlowArtifact) (*CompiledArtifact, []CompilationDiagnostic, error) {
	return CompileFlowArtifactPartialWithOptions(artifact, CompileOptions{})
}

func CompileFlowArtifactPartialWithOptions(artifact facts.FlowArtifact, rawOptions CompileOptions) (*CompiledArtifact, []CompilationDiagnostic, error) {
	if artifact.Artifact == "" {
		return nil, nil, fmt.Errorf("flow: invalid artifact: artifact ID is empty")
	}
	options, err := normalizedCompileOptions(rawOptions)
	if err != nil {
		return nil, nil, err
	}
	if len(artifact.Functions) > options.MaxFunctions {
		return nil, nil, compileFailure("", "", "", "graph_limit", "function count exceeds compilation bound")
	}
	metadataOptions := options
	metadataOptions.MaxWork = options.MaxArtifactWork
	metadataWork, err := preflightArtifactMetadata(artifact, metadataOptions)
	if err != nil {
		return nil, nil, err
	}
	out := &CompiledArtifact{artifact: artifactHeader(artifact), functions: make(map[string]*CompiledFunction)}
	var diagnostics []CompilationDiagnostic
	seenFunctions := map[string]bool{}
	functions := append([]facts.FlowFunction(nil), artifact.Functions...)
	sort.SliceStable(functions, func(i, j int) bool { return functions[i].ID < functions[j].ID })
	artifactWork := metadataWork
	for i := range functions {
		fn := functions[i]
		if seenFunctions[fn.ID] {
			return nil, nil, fmt.Errorf("flow: duplicate function: %s", fn.ID)
		}
		seenFunctions[fn.ID] = true
		if err := validateFunctionIdentity(fn); err != nil {
			diagnostics = append(diagnostics, CompilationDiagnostic{FunctionID: fn.ID, Error: err})
			continue
		}
		remaining := options.MaxArtifactWork - artifactWork
		if remaining <= 0 {
			diagnostics = append(diagnostics, CompilationDiagnostic{FunctionID: fn.ID, Error: compileFailure(fn.ID, "", "", "graph_limit", "artifact compiler work exceeds compilation bound")})
			continue
		}
		localOptions := options
		if localOptions.MaxWork > remaining {
			localOptions.MaxWork = remaining
		}
		work, preflightErr := preflightFunction(fn, localOptions)
		if preflightErr != nil {
			artifactWork += work
			diagnostics = append(diagnostics, CompilationDiagnostic{FunctionID: fn.ID, Error: preflightErr})
			continue
		}
		{
			artifactWork += work
			admitted := cloneFlowFunction(fn)
			var compiled *CompiledFunction
			compiled, err := buildCompiledFunctionOwned(admitted, localOptions, work)
			if compiled != nil {
				artifactWork += compiled.CompilerWork() - work
			}
			if err == nil {
				if artifactWork > options.MaxArtifactWork {
					diagnostics = append(diagnostics, CompilationDiagnostic{FunctionID: fn.ID, Error: compileFailure(fn.ID, "", "", "graph_limit", "artifact compiler work exceeds compilation bound")})
					continue
				}
				out.functions[fn.ID] = compiled
				out.artifact.Functions = append(out.artifact.Functions, admitted)
				continue
			}
			diagnostics = append(diagnostics, CompilationDiagnostic{FunctionID: fn.ID, Error: err})
		}
	}
	out.artifact.Types = cloneFlowTypes(artifact.Types)
	out.artifact.PublicRoutes = cloneRoutes(artifact.PublicRoutes)
	out.artifact.Provenance = cloneProvenance(artifact.Provenance)
	sort.Slice(diagnostics, func(i, j int) bool {
		if diagnostics[i].FunctionID == diagnostics[j].FunctionID {
			return diagnostics[i].Error.Error() < diagnostics[j].Error.Error()
		}
		return diagnostics[i].FunctionID < diagnostics[j].FunctionID
	})
	return out, diagnostics, nil
}
