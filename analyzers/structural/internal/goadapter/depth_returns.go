package goadapter

import (
	"go/ast"
	"go/types"
	"slopslap.dev/structural/internal/facts"
)

func depthErrorType(typ types.Type) bool {
	return typ != nil && types.Identical(typ, types.Universe.Lookup("error").Type())
}
func depthErrorResult(signature *types.Signature) bool {
	n := signature.Results().Len()
	return n > 0 && depthErrorType(signature.Results().At(n-1).Type())
}
func depthNil(b *depthLowering, expression ast.Expr) bool {
	if parenthesized, ok := expression.(*ast.ParenExpr); ok {
		return depthNil(b, parenthesized.X)
	}
	identifier, ok := expression.(*ast.Ident)
	return ok && b.source.typeInfo.ObjectOf(identifier) == types.Universe.Lookup("nil")
}
func lowerDepthReturn(b *depthLowering, node *ast.ReturnStmt) {
	operands := []string{}
	if len(node.Results) == 0 {
		if !b.allNamed {
			b.unknown()
			return
		}
		for index, result := range b.results {
			if index < len(b.resultObjs) {
				if value := b.values[b.resultObjs[index]]; value != "" {
					operands = append(operands, value)
					continue
				}
			}
			operands = append(operands, result)
		}
	}
	for _, expression := range node.Results {
		value := lowerDepthExpression(b, expression)
		_, tupleExpression := b.source.typeInfo.TypeOf(expression).(*types.Tuple)
		if tuple, ok := b.tuples[value]; ok && len(node.Results) == 1 && tupleExpression {
			operands = append(operands, tuple...)
		} else {
			operands = append(operands, value)
		}
	}
	in := facts.Instruction{Opcode: facts.OpReturn, Operands: operands}
	if b.errorResult && len(operands) > 0 && b.errorValues[operands[len(operands)-1]] {
		in.CompletionKind = facts.EdgeReturnError
	}
	b.emit(in)
}
