package goadapter

import "go/ast"

func lowerDepthAssignedValues(b *depthLowering, node *ast.AssignStmt) []string {
	values := make([]string, len(node.Rhs))
	for index, expression := range node.Rhs {
		values[index] = lowerDepthExpression(b, expression)
	}
	if len(values) == 1 && len(node.Lhs) > 1 {
		if tuple, ok := b.tuples[values[0]]; ok {
			return tuple
		}
	}
	return values
}
