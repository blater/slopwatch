package goadapter

import (
	"go/ast"
	"go/token"
	"slopslap.dev/structural/internal/facts"
)

func lowerDepthNilError(b *depthLowering) string {
	return b.emit(facts.Instruction{Opcode: facts.OpConstant, Type: "error", ValueKind: facts.FlowKindError, Value: &facts.Value{Type: "error", ValueKind: facts.FlowKindError, Constant: "nil"}})
}
func lowerDepthErrorComparison(b *depthLowering, node *ast.BinaryExpr) (string, bool) {
	if node.Op != token.EQL && node.Op != token.NEQ {
		return "", false
	}
	var operand ast.Expr
	if depthNil(b, node.X) {
		operand = node.Y
	} else if depthNil(b, node.Y) {
		operand = node.X
	}
	if operand == nil || !depthErrorType(b.source.typeInfo.TypeOf(operand)) {
		return "", false
	}
	value := lowerDepthExpression(b, operand)
	present := b.emit(facts.Instruction{Opcode: facts.OpErrorPresent, Type: "bool", ValueKind: facts.FlowKindBoolean, Operands: []string{value}})
	if node.Op == token.EQL {
		present = b.emit(facts.Instruction{Opcode: facts.OpPrimitive, Type: "bool", ValueKind: facts.FlowKindBoolean, ArithmeticMode: "boolean", Operator: "!", Operands: []string{present}})
	}
	return present, true
}
