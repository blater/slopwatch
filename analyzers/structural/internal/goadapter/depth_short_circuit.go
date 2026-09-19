package goadapter

import (
	"go/ast"
	"go/token"
	"slopslap.dev/structural/internal/facts"
)

func lowerDepthShortCircuit(b *depthLowering, node *ast.BinaryExpr) string {
	left := lowerDepthExpression(b, node.X)
	branch := b.current
	right, short, join := b.newBlock(), b.newBlock(), b.newBlock()
	yes, no := right, short
	if node.Op == token.LOR {
		yes, no = short, right
	}
	b.function.Blocks[branch].Edges = []facts.FlowEdge{
		{From: b.function.Blocks[branch].ID, To: b.function.Blocks[yes].ID, Kind: facts.EdgeTrue, Guard: left, GuardPolarity: "true"},
		{From: b.function.Blocks[branch].ID, To: b.function.Blocks[no].ID, Kind: facts.EdgeFalse, Guard: left, GuardPolarity: "false"},
	}
	b.current = right
	rightValue := lowerDepthExpression(b, node.Y)
	rightEnd := b.current
	b.connect(rightEnd, join)
	b.current = short
	typ, kind, _ := depthType(b.source.typeInfo.TypeOf(node))
	literal := "false"
	if node.Op == token.LOR {
		literal = "true"
	}
	shortValue := b.emit(facts.Instruction{Opcode: facts.OpConstant, Type: typ, ValueKind: kind, Value: &facts.Value{Type: typ, ValueKind: kind, Constant: literal}})
	b.connect(short, join)
	b.current = join
	return b.emit(facts.Instruction{Opcode: facts.OpPhi, Type: typ, ValueKind: kind, PhiInputs: []facts.PhiInput{
		{Predecessor: b.function.Blocks[rightEnd].ID, Value: rightValue},
		{Predecessor: b.function.Blocks[short].ID, Value: shortValue},
	}})
}
