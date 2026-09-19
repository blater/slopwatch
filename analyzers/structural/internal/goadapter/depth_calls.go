package goadapter

import (
	"fmt"
	"go/ast"
	"go/types"
	"slopslap.dev/structural/internal/facts"
)

func lowerDepthCall(b *depthLowering, node *ast.CallExpr) string {
	if depthNilErrorConversion(b, node) {
		return lowerDepthNilError(b)
	}
	actuals := make([]string, len(node.Args))
	for index, argument := range node.Args {
		actuals[index] = lowerDepthExpression(b, argument)
	}
	resolved := depthCalledObject(b, node.Fun)
	if builtin, ok := resolved.(*types.Builtin); ok && builtin.Name() == "panic" && len(actuals) == 1 {
		return b.emit(facts.Instruction{Opcode: facts.OpThrow, Operands: actuals})
	}
	object, ok := resolved.(*types.Func)
	if ok {
		if value, matched := lowerDepthBuiltin(b, object, actuals); matched {
			return value
		}
	}
	return lowerDepthLocalCall(b, object, actuals)
}

func depthNilErrorConversion(b *depthLowering, node *ast.CallExpr) bool {
	value, ok := b.source.typeInfo.Types[node.Fun]
	return ok && value.IsType() && depthErrorType(value.Type) && len(node.Args) == 1 && depthNil(b, node.Args[0])
}

func lowerDepthLocalCall(b *depthLowering, object *types.Func, actuals []string) string {
	if object == nil || object.Pkg() != b.pkg || object.Parent() != b.pkg.Scope() {
		return b.unknown()
	}
	signature := object.Type().(*types.Signature)
	if signature.Recv() != nil || signature.Variadic() || signature.TypeParams().Len() != 0 || signature.Params().Len() != len(actuals) || signature.Results().Len() > 32 {
		return b.unknown()
	}
	call := &facts.CallBinding{Targets: []string{packageID(b.source) + "/" + object.Name()}}
	for index, actual := range actuals {
		call.Bindings = append(call.Bindings, facts.Binding{Formal: fmt.Sprintf("arg%d", index), Actual: actual})
	}
	return emitDepthCall(b, signature, call, actuals)
}

func emitDepthCall(b *depthLowering, signature *types.Signature, call *facts.CallBinding, actuals []string) string {
	typ, kind := "void", facts.FlowKindUnknown
	if signature.Results().Len() == 1 {
		typ, kind, _ = depthType(signature.Results().At(0).Type())
	}
	instruction := facts.Instruction{Opcode: facts.OpCall, Type: typ, ValueKind: kind, Call: call, Operands: actuals}
	if signature.Results().Len() > 1 {
		for index := 0; index < signature.Results().Len(); index++ {
			result := fmt.Sprintf("n%d_result%d", b.next+1, index)
			instruction.Results = append(instruction.Results, result)
			call.ResultBindings = append(call.ResultBindings, facts.Binding{Formal: fmt.Sprintf("result%d", index), Actual: result})
		}
		b.emit(instruction)
		b.tuples[instruction.Results[0]] = instruction.Results
		return instruction.Results[0]
	}
	return b.emit(instruction)
}
