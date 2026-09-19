package goadapter

import (
	"fmt"
	"go/ast"
	"go/types"
	"slopslap.dev/structural/internal/facts"
	"sort"
)

func (b *depthLowering) newBlock() int {
	index := len(b.function.Blocks)
	b.function.Blocks = append(b.function.Blocks, facts.FlowBlock{ID: fmt.Sprintf("b%d", index)})
	return index
}
func (b *depthLowering) connect(from, to int) {
	b.function.Blocks[from].Edges = append(b.function.Blocks[from].Edges, facts.FlowEdge{From: b.function.Blocks[from].ID, To: b.function.Blocks[to].ID, Kind: facts.EdgeNormal})
}
func lowerDepthLoop(b *depthLowering, node *ast.ForStmt) {
	if node.Init != nil {
		lowerDepthStatement(b, node.Init)
	}
	before := b.current
	changed := depthLoopVariables(b, node)
	initial := map[types.Object]string{}
	for _, object := range changed {
		initial[object] = b.values[object]
	}
	header, body, exit := b.newBlock(), b.newBlock(), b.newBlock()
	b.connect(before, header)
	b.current = header
	phis := map[types.Object]int{}
	for index, object := range changed {
		typ, kind, _ := depthType(object.Type())
		phis[object] = len(b.function.Blocks[header].Instructions)
		value := b.emit(facts.Instruction{Opcode: facts.OpPhi, Type: typ, ValueKind: kind})
		b.values[object] = value
		b.function.Recurrences = append(b.function.Recurrences, facts.Recurrence{ID: fmt.Sprintf("%s/slot%d", b.function.Blocks[header].ID, index), LoopHeader: b.function.Blocks[header].ID, PhiSlot: value})
	}
	var guard string
	if node.Cond == nil {
		guard = b.emit(facts.Instruction{Opcode: facts.OpConstant, Type: "bool", ValueKind: facts.FlowKindBoolean, Value: &facts.Value{Type: "bool", ValueKind: facts.FlowKindBoolean, Constant: "true"}})
	} else {
		guard = lowerDepthExpression(b, node.Cond)
	}
	conditionBlock := b.current
	b.function.Blocks[conditionBlock].Edges = []facts.FlowEdge{
		{From: b.function.Blocks[conditionBlock].ID, To: b.function.Blocks[body].ID, Kind: facts.EdgeTrue, Guard: guard, GuardPolarity: "true"},
		{From: b.function.Blocks[conditionBlock].ID, To: b.function.Blocks[exit].ID, Kind: facts.EdgeFalse, Guard: guard, GuardPolarity: "false"},
	}
	headerValues := copyDepthValues(b.values)
	b.current = body
	lowerDepthStatements(b, node.Body.List)
	if node.Post != nil {
		lowerDepthStatement(b, node.Post)
	}
	latch := b.current
	b.connect(latch, header)
	for _, object := range changed {
		phi := &b.function.Blocks[header].Instructions[phis[object]]
		phi.PhiInputs = []facts.PhiInput{{Predecessor: b.function.Blocks[before].ID, Value: initial[object]}, {Predecessor: b.function.Blocks[latch].ID, Value: b.values[object]}}
	}
	b.values = headerValues
	b.current = exit
}
func depthLoopVariables(b *depthLowering, node *ast.ForStmt) []types.Object {
	changed := map[types.Object]bool{}
	mark := func(expression ast.Expr) {
		identifier, ok := expression.(*ast.Ident)
		if !ok {
			return
		}
		object := b.source.typeInfo.ObjectOf(identifier)
		if _, exists := b.values[object]; exists {
			changed[object] = true
		}
	}
	ast.Inspect(node, func(node ast.Node) bool {
		switch item := node.(type) {
		case *ast.FuncLit:
			return false
		case *ast.AssignStmt:
			for _, left := range item.Lhs {
				mark(left)
			}
		case *ast.IncDecStmt:
			mark(item.X)
		}
		return true
	})
	output := make([]types.Object, 0, len(changed))
	for object := range changed {
		output = append(output, object)
	}
	sort.Slice(output, func(i, j int) bool { return b.values[output[i]] < b.values[output[j]] })
	return output
}
func copyDepthValues(input map[types.Object]string) map[types.Object]string {
	output := make(map[types.Object]string, len(input))
	for key, value := range input {
		output[key] = value
	}
	return output
}
