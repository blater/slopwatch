package depth

import "slopslap.dev/structural/internal/facts"

// Control results retain guards and raw evidence. Capability proofs consume
// these facts later; observing an instruction never awards a responsibility.
type GuardedEffect struct {
	Guard  RecipeID
	Block  string
	Effect TransferEffect
}
type GuardedEdge struct {
	Guard    RecipeID
	From, To string
	Kind     facts.EdgeKind
}

// ResourceLifecycleWitness is an internal proof carried across exact scalar
// helper summaries. Governed is the canonical alias root produced by the
// callee's resource recognizer; it is deliberately independent of a caller
// boundary identity so the caller can mint its own obligation.
type ResourceLifecycleWitness struct {
	Guard    RecipeID
	Governed string
	Rule     string
	Evidence []string
}
type FlowGap struct {
	Guard                      RecipeID
	Block, Instruction, Reason string
}
type FlowCompletion struct {
	Guard         RecipeID
	Kind          facts.EdgeKind
	ErrorTag      string
	Values        []DomainValue
	Origin, Block string
}
type FlowEvaluation struct {
	Status                TransferStatus
	Completions           []FlowCompletion
	Effects               []GuardedEffect
	Edges                 []GuardedEdge
	Gaps                  []FlowGap
	ResourceWitnesses     []ResourceLifecycleWitness
	ResourceEffectsProven bool
	Work, Blocks          int
	Dependencies          []string
}
type pendingCompletion struct {
	guard       guardID
	kind        facts.EdgeKind
	tag, origin string
	values      []DomainValue
}
type controlInput struct {
	predecessor string
	edge        facts.FlowEdge
	guard       guardID
	state       *TransferState
	pending     []pendingCompletion
}
type controlFrame struct {
	state   *TransferState
	guard   guardID
	pending []pendingCompletion
}
type controlEvaluation struct {
	callRejectionSupport uint8
	arena                *RecipeArena
	function             *CompiledFunction
	budget               transferBudget
	guards               *controlGuards
	options              TransferOptions
	result               FlowEvaluation
	inputs               map[string][]controlInput
}

// EvaluateDAG retains the acyclic-only entrypoint for callers requiring it.
func EvaluateDAG(arena *RecipeArena, function *CompiledFunction, initial *TransferState, options TransferOptions) FlowEvaluation {
	return evaluateControl(arena, function, initial, options, false)
}

// EvaluateFlow evaluates acyclic control and supported numeric recurrences.
// Inputs explicitly supply formal/receiver values, including aliases.
func EvaluateFlow(arena *RecipeArena, function *CompiledFunction, initial *TransferState, options TransferOptions) FlowEvaluation {
	return evaluateControl(arena, function, initial, options, true)
}

func evaluateControl(arena *RecipeArena, function *CompiledFunction, initial *TransferState, options TransferOptions, loops bool) FlowEvaluation {
	if arena == nil || function == nil || initial == nil {
		return FlowEvaluation{Status: TransferUnsupported, Gaps: []FlowGap{{Reason: "missing_control_input"}}}
	}
	maximum := options.MaxWork
	if maximum == 0 {
		maximum = defaultMaxWork
	}
	e := &controlEvaluation{arena: arena, function: function, options: options, result: FlowEvaluation{Status: TransferOK}, inputs: map[string][]controlInput{}}
	e.budget = transferBudget{state: NewTransferState(), context: options.Context, maximum: maximum, sharedCharge: options.sharedCharge}
	e.guards = newControlGuards(arena, e.budget.charge)
	if loops {
		return evaluateControlRegions(e, initial)
	}
	order, acyclic := topologicalControlOrder(function, e.budget.charge)
	if !acyclic {
		rejectControlCycles(e)
		return e.finish()
	}
	e.inputs[function.function.Entry] = []controlInput{{guard: guardTrue, state: initial}}
	for _, id := range order {
		if !e.budget.charge(1) {
			break
		}
		incoming := e.inputs[id]
		if len(incoming) == 0 {
			continue
		}
		frame := e.merge(id, incoming)
		if e.budget.status != "" {
			break
		}
		e.result.Blocks++
		e.block(id, frame, incoming)
		if e.budget.status != "" {
			break
		}
		delete(e.inputs, id)
	}
	return e.finish()
}
func (e *controlEvaluation) gap(block, instruction string, guard guardID, reason string) {
	e.result.Status = TransferPartial
	e.result.Gaps = append(e.result.Gaps, FlowGap{Guard: e.guards.recipe(guard), Block: block, Instruction: instruction, Reason: reason})
}
func (e *controlEvaluation) finish() FlowEvaluation {
	if e.budget.status != "" {
		e.result.Status = e.budget.status
		e.result.Gaps = append(e.result.Gaps, FlowGap{Reason: string(e.budget.status)})
	}
	e.result.Work = e.budget.state.work
	return e.result
}
