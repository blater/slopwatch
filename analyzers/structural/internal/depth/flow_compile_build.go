package depth

import "slopslap.dev/structural/internal/facts"

func CompileFunction(fn facts.FlowFunction) (*CompiledFunction, error) {
	return CompileFunctionWithOptions(fn, CompileOptions{})
}

func CompileFunctionWithOptions(fn facts.FlowFunction, options CompileOptions) (*CompiledFunction, error) {
	normalized, err := normalizedCompileOptions(options)
	if err != nil {
		return nil, err
	}
	if err := validateFunctionIdentity(fn); err != nil {
		return nil, err
	}
	work, err := preflightFunction(fn, normalized)
	if err != nil {
		return nil, err
	}
	compiled, err := buildCompiledFunction(fn, normalized, work)
	if err != nil {
		return nil, err
	}
	return compiled, nil
}
