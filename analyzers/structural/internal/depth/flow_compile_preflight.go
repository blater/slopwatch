package depth

import "slopslap.dev/structural/internal/facts"

func preflightFunction(fn facts.FlowFunction, options CompileOptions) (int, error) {
	count := compilePreflight{blocks: len(fn.Blocks), work: len(fn.Blocks)}
	if err := checkPreflight(count, options, fn.ID, "", ""); err != nil {
		return count.work, err
	}
	for _, block := range fn.Blocks {
		count.instructions += len(block.Instructions)
		if err := addPreflight(&count, len(block.Edges), options, fn.ID, block.ID, ""); err != nil {
			return count.work, err
		}
		if err := checkPreflight(count, options, fn.ID, block.ID, ""); err != nil {
			return count.work, err
		}
		for _, in := range block.Instructions {
			if err := checkPreflight(count, options, fn.ID, block.ID, in.ID); err != nil {
				return count.work, err
			}
			if err := preflightInstruction(&count, in, options, fn.ID, block.ID); err != nil {
				return count.work, err
			}
		}
	}
	if err := addPreflight(&count, len(fn.Formals)+len(fn.Results)+len(fn.Exits)+len(fn.Recurrences)+len(fn.ReturnConcepts), options, fn.ID, "", ""); err != nil {
		return count.work, err
	}
	if fn.ReceiverFormal != nil {
		if err := addPreflight(&count, 1, options, fn.ID, "", ""); err != nil {
			return count.work, err
		}
	}
	for _, recurrence := range fn.Recurrences {
		if err := addPreflight(&count, len(recurrence.References), options, fn.ID, "", ""); err != nil {
			return count.work, err
		}
	}
	for _, provenance := range fn.Provenance {
		if err := addPreflight(&count, 1+len(provenance.FactIDs), options, fn.ID, "", ""); err != nil {
			return count.work, err
		}
	}
	return count.work, nil
}

func preflightInstruction(count *compilePreflight, in facts.Instruction, options CompileOptions, fn, block string) error {
	for _, n := range []int{len(in.Operands), len(in.Results), len(in.Roots), len(in.Effects), len(in.Bindings), len(in.FieldBindings), len(in.PhiInputs), len(in.AccessPath)} {
		if err := addPreflight(count, n, options, fn, block, in.ID); err != nil {
			return err
		}
	}
	for _, provenance := range in.Provenance {
		if err := addPreflight(count, 1+len(provenance.FactIDs), options, fn, block, in.ID); err != nil {
			return err
		}
	}
	if in.Call != nil {
		for _, n := range []int{len(in.Call.Targets), len(in.Call.Bindings), len(in.Call.ResultBindings), len(in.Call.ErrorContinuations), len(in.Call.OwnershipEffects)} {
			if err := addPreflight(count, n, options, fn, block, in.ID); err != nil {
				return err
			}
		}
	}
	if in.Value != nil {
		if err := addPreflight(count, 1+len(in.Value.Concepts)+len(in.Value.MayRoots), options, fn, block, in.ID); err != nil {
			return err
		}
	}
	for _, root := range in.Roots {
		if err := addPreflight(count, len(root.PathSegments), options, fn, block, in.ID); err != nil {
			return err
		}
	}
	return nil
}
