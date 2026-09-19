package goadapter

import (
	"go/ast"
	"go/types"
	"slopslap.dev/structural/internal/facts"
	"sort"
)

type depthBranchEnd struct {
	block  int
	values map[types.Object]string
	live   bool
}

func lowerDepthIf(b *depthLowering, node *ast.IfStmt) {
	if node.Init != nil {
		lowerDepthStatement(b, node.Init)
	}
	predicate := lowerDepthExpression(b, node.Cond)
	branch := b.current
	yes, no, join := b.newBlock(), b.newBlock(), b.newBlock()
	b.function.Blocks[branch].Edges = []facts.FlowEdge{
		{From: b.function.Blocks[branch].ID, To: b.function.Blocks[yes].ID, Kind: facts.EdgeTrue, Guard: predicate, GuardPolarity: "true"},
		{From: b.function.Blocks[branch].ID, To: b.function.Blocks[no].ID, Kind: facts.EdgeFalse, Guard: predicate, GuardPolarity: "false"},
	}
	initial := copyDepthValues(b.values)
	left := lowerDepthBranch(b, yes, join, node.Body, initial)
	right := lowerDepthBranch(b, no, join, node.Else, initial)
	b.current = join
	b.values = initial
	if !left.live {
		b.values = right.values
		return
	}
	if !right.live {
		b.values = left.values
		return
	}
	objects := make([]types.Object, 0, len(initial))
	for object := range initial {
		objects = append(objects, object)
	}
	sort.Slice(objects, func(i, j int) bool { return initial[objects[i]] < initial[objects[j]] })
	for _, object := range objects {
		a, z := left.values[object], right.values[object]
		if a == z {
			b.values[object] = a
			continue
		}
		typ, kind, _ := depthType(object.Type())
		b.values[object] = b.emit(facts.Instruction{Opcode: facts.OpPhi, Type: typ, ValueKind: kind, PhiInputs: []facts.PhiInput{{Predecessor: b.function.Blocks[left.block].ID, Value: a}, {Predecessor: b.function.Blocks[right.block].ID, Value: z}}})
	}
}
func lowerDepthBranch(b *depthLowering, start, join int, statement ast.Stmt, initial map[types.Object]string) depthBranchEnd {
	b.current = start
	b.values = copyDepthValues(initial)
	if statement != nil {
		lowerDepthStatement(b, statement)
	}
	live := !depthTerminated(b)
	if live {
		b.connect(b.current, join)
	}
	return depthBranchEnd{block: b.current, values: b.values, live: live}
}
