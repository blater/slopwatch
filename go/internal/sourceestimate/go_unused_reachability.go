package sourceestimate

import (
	"go/ast"
	gotoken "go/token"
)

// goUnusedContext carries the exact syntactic proof previously reconstructed
// at every reference. Function literals stop caller lookup but do not reset
// enclosing dead-code conditions or the declaration used for signature checks.
type goUnusedContext struct {
	caller, signature *ast.FuncDecl
	dead, fieldName   bool
}

// indexGoUnusedContext visits each AST node and block statement once. The
// returned work count includes both visits and supports deterministic scaling
// regressions without timing assertions.
func indexGoUnusedContext(parents map[ast.Node]ast.Node, nodeFile map[ast.Node]int, contexts map[ast.Node]goUnusedContext, file *ast.File, index int) int {
	stack := []ast.Node{}
	deadRoots := map[ast.Node]bool{}
	fieldNames := map[ast.Node]bool{}
	type boolValue struct{ known, value bool }
	booleans := map[ast.Expr]boolValue{}
	work := 0
	var evaluate func(ast.Expr) boolValue
	evaluate = func(expression ast.Expr) boolValue {
		if value, ok := booleans[expression]; ok {
			return value
		}
		work++
		result := boolValue{}
		switch expression := expression.(type) {
		case *ast.Ident:
			result = boolValue{expression.Name == "true" || expression.Name == "false", expression.Name == "true"}
		case *ast.ParenExpr:
			result = evaluate(expression.X)
		case *ast.UnaryExpr:
			if expression.Op == gotoken.NOT {
				result = evaluate(expression.X)
				result.value = !result.value
			}
		case *ast.BinaryExpr:
			left, right := evaluate(expression.X), evaluate(expression.Y)
			if left.known && right.known {
				switch expression.Op {
				case gotoken.LAND:
					result = boolValue{true, left.value && right.value}
				case gotoken.LOR:
					result = boolValue{true, left.value || right.value}
				}
			}
		}
		booleans[expression] = result
		return result
	}
	ast.Inspect(file, func(node ast.Node) bool {
		if node == nil {
			stack = stack[:len(stack)-1]
			return true
		}
		work++
		context := goUnusedContext{}
		if len(stack) > 0 {
			parent := stack[len(stack)-1]
			parents[node] = parent
			context = contexts[parent]
			switch parent := parent.(type) {
			case *ast.FuncDecl:
				context.caller, context.signature = parent, parent
			case *ast.FuncLit:
				context.caller = nil
			}
		}
		context.dead = context.dead || deadRoots[node]
		context.fieldName = fieldNames[node]
		contexts[node], nodeFile[node] = context, index
		switch node := node.(type) {
		case *ast.Field:
			for _, name := range node.Names {
				work++
				fieldNames[name] = true
			}
		case *ast.BlockStmt:
			terminated := false
			for _, statement := range node.List {
				work++
				deadRoots[statement] = terminated
				terminated = terminated || goUnusedTerminates(statement)
			}
		case *ast.IfStmt:
			condition := evaluate(node.Cond)
			if condition.known {
				deadRoots[node.Body] = !condition.value
				if node.Else != nil {
					deadRoots[node.Else] = condition.value
				}
			}
		case *ast.ForStmt:
			condition := evaluate(node.Cond)
			if condition.known && !condition.value {
				deadRoots[node.Body] = true
			}
		case *ast.BinaryExpr:
			condition := evaluate(node.X)
			if condition.known && ((node.Op == gotoken.LAND && !condition.value) || (node.Op == gotoken.LOR && condition.value)) {
				deadRoots[node.Y] = true
			}
		}
		stack = append(stack, node)
		return true
	})
	return work
}

func goUnusedTerminates(statement ast.Stmt) bool {
	switch statement.(type) {
	case *ast.ReturnStmt:
		return true
	case *ast.BranchStmt:
		return true
	default:
		return false
	}
}
