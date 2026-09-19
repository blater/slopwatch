package depth

import "slopslap.dev/structural/internal/facts"

type transferHandler func(*transferStep)

var valueHandlers = map[facts.Opcode]transferHandler{
	facts.OpErrorPresent: transferErrorPresent,
	facts.OpConstant:     transferConstant, facts.OpBind: transferBind,
	facts.OpPrimitive: transferPrimitive, facts.OpPack: transferPack,
	facts.OpAllocate: transferAllocate, facts.OpFieldRead: transferRead,
	facts.OpFieldWrite: transferWrite, facts.OpEscape: transferEscape,
	facts.OpUnknown: transferUnknown,
	facts.OpAcquire: transferResource, facts.OpUseResource: transferResource,
	facts.OpCleanupAttempt: transferResource, facts.OpLock: transferResource,
	facts.OpUnlock: transferResource,
}

// Apply evaluates one validated instruction. The affected state is committed
// only after the instruction finishes within its work and cancellation limits.
func Apply(arena *RecipeArena, state *TransferState, in facts.Instruction, options TransferOptions) TransferResult {
	if arena == nil || state == nil {
		return TransferResult{Status: TransferUnsupported, Reasons: []string{"missing_transfer_state"}}
	}
	step := newTransferStep(arena, state, in, options)
	if !step.budget.charge(1) {
		return step.finish()
	}
	if !chargeInstruction(step) {
		return step.finish()
	}
	handler, ok := valueHandlers[in.Opcode]
	if in.Opcode == facts.OpCall {
		handler, ok = transferCall, true
	}
	if !ok {
		step.partial("unsupported_opcode")
		step.result.Status = TransferUnsupported
		return step.finish()
	}
	if in.ID == "" {
		step.partial("invalid_instruction")
		step.result.Status = TransferUnsupported
		return step.finish()
	}
	handler(step)
	return step.finish()
}

func (s *TransferState) Apply(arena *RecipeArena, in facts.Instruction, options TransferOptions) TransferResult {
	return Apply(arena, s, in, options)
}
