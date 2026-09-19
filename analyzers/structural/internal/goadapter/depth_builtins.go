package goadapter

import (
	"fmt"
	"go/ast"
	"go/types"
	"slopslap.dev/structural/internal/facts"
)

func depthCalledObject(b *depthLowering, expression ast.Expr) types.Object {
	switch node := expression.(type) {
	case *ast.Ident:
		return b.source.typeInfo.ObjectOf(node)
	case *ast.SelectorExpr:
		if b.source.typeInfo.Selections[node] == nil {
			return b.source.typeInfo.ObjectOf(node.Sel)
		}
	}
	return nil
}
func lowerDepthBuiltin(b *depthLowering, function *types.Func, actuals []string) (string, bool) {
	if b.source.depthImports == nil {
		return "", false
	}
	entry, ok := b.source.depthImports.bindings[function]
	if !ok {
		return "", false
	}
	if entry.ID != "go.error" || len(actuals) != 1 {
		return b.unknown(), true
	}
	root := fmt.Sprintf("%s/error%d", b.function.ID, b.next+1)
	value := b.emit(facts.Instruction{Opcode: facts.OpAllocate, Type: "error", ValueKind: facts.FlowKindError,
		Roots:         []facts.AliasRoot{{ID: root, Kind: "allocation", Ownership: "owned"}},
		FieldBindings: []facts.FieldBinding{{Field: "error.message", Value: actuals[0]}},
		Provenance:    []facts.Provenance{{RuleID: entry.ID}},
	})
	b.errorValues[value] = true
	return value, true
}
