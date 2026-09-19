package sourceestimate

import (
	"go/ast"
	gotoken "go/token"
)

// goUnusedCallReachable rejects only syntactically certain dead witnesses. It
// intentionally leaves ordinary conditional calls usable; those still have a
// live path unless a literal condition or prior unconditional transfer proves
// otherwise.
func goUnusedCallReachable(parents map[ast.Node]ast.Node, call *ast.CallExpr) bool {
	for parent := parents[call]; parent != nil; parent = parents[parent] {
		switch enclosing := parent.(type) {
		case *ast.IfStmt:
			if goUnusedDescendant(parents, enclosing.Body, call) {
				if known, value := goUnusedBool(enclosing.Cond); known && !value {
					return false
				}
			} else if enclosing.Else != nil && goUnusedDescendant(parents, enclosing.Else, call) {
				if known, value := goUnusedBool(enclosing.Cond); known && value {
					return false
				}
			}
		case *ast.ForStmt:
			if enclosing.Body != nil && goUnusedDescendant(parents, enclosing.Body, call) {
				if known, value := goUnusedBool(enclosing.Cond); known && !value {
					return false
				}
			}
		case *ast.BinaryExpr:
			if goUnusedDescendant(parents, enclosing.Y, call) {
				switch enclosing.Op {
				case gotoken.LAND:
					if known, value := goUnusedBool(enclosing.X); known && !value {
						return false
					}
				case gotoken.LOR:
					if known, value := goUnusedBool(enclosing.X); known && value {
						return false
					}
				}
			}
		case *ast.BlockStmt:
			for index, statement := range enclosing.List {
				if !goUnusedDescendant(parents, statement, call) {
					continue
				}
				for _, previous := range enclosing.List[:index] {
					if goUnusedTerminates(previous) {
						return false
					}
				}
				break
			}
		}
	}
	return true
}

func goUnusedDescendant(parents map[ast.Node]ast.Node, root, target ast.Node) bool {
	for node := target; node != nil; node = parents[node] {
		if node == root {
			return true
		}
	}
	return false
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

func goUnusedBool(expression ast.Expr) (known, value bool) {
	switch expression := expression.(type) {
	case *ast.Ident:
		if expression.Name == "true" {
			return true, true
		}
		if expression.Name == "false" {
			return true, false
		}
	case *ast.ParenExpr:
		return goUnusedBool(expression.X)
	case *ast.UnaryExpr:
		if expression.Op == gotoken.NOT {
			known, value := goUnusedBool(expression.X)
			return known, !value
		}
	case *ast.BinaryExpr:
		leftKnown, left := goUnusedBool(expression.X)
		rightKnown, right := goUnusedBool(expression.Y)
		if leftKnown && rightKnown {
			switch expression.Op {
			case gotoken.LAND:
				return true, left && right
			case gotoken.LOR:
				return true, left || right
			}
		}
	}
	return false, false
}
