package sourceestimate

import (
	"go/ast"
	gotoken "go/token"
)

func (p *goUnusedInputPackage) callsFor(function *goUnusedInputFunction) ([]goUnusedInputCall, bool) {
	return p.refs[function], p.refValid[function]
}

func (p *goUnusedInputPackage) enclosingFunction(node ast.Node) (*ast.FuncDecl, int) {
	decl := p.context[node].caller
	if decl == nil {
		return nil, -1
	}
	return decl, p.nodeFile[decl]
}

func (p *goUnusedInputPackage) pureGoUnusedArgument(caller *ast.FuncDecl, expression ast.Expr) (pure, computed bool) {
	info, ok := p.callerArg[caller]
	if !ok {
		info = goUnusedCallerArgs{allowed: map[string]bool{}}
		if caller != nil && caller.Type != nil && caller.Type.Params != nil {
			for _, field := range caller.Type.Params.List {
				if _, variadic := field.Type.(*ast.Ellipsis); variadic {
					info.shadowed = true
					break
				}
				for _, ident := range field.Names {
					info.allowed[ident.Name] = true
				}
			}
		}
		if !info.shadowed {
			info.shadowed = goUnusedCallerShadowed(caller, info.allowed)
		}
		p.callerArg[caller] = info
	}
	if info.shadowed {
		return false, false
	}
	return pureGoUnusedExpression(info.allowed, expression)
}

type goUnusedCallerArgs struct {
	allowed  map[string]bool
	shadowed bool
}

func goUnusedCallerShadowed(caller *ast.FuncDecl, allowed map[string]bool) bool {
	if caller == nil || caller.Body == nil {
		return false
	}
	shadowed := false
	ast.Inspect(caller.Body, func(node ast.Node) bool {
		switch declaration := node.(type) {
		case *ast.AssignStmt:
			if declaration.Tok == gotoken.DEFINE {
				for _, left := range declaration.Lhs {
					if ident, ok := left.(*ast.Ident); ok && allowed[ident.Name] {
						shadowed = true
						return false
					}
				}
			}
		case *ast.ValueSpec:
			for _, ident := range declaration.Names {
				if allowed[ident.Name] {
					shadowed = true
					return false
				}
			}
		case *ast.RangeStmt:
			if ident, ok := declaration.Key.(*ast.Ident); ok && allowed[ident.Name] {
				shadowed = true
				return false
			}
			if ident, ok := declaration.Value.(*ast.Ident); ok && allowed[ident.Name] {
				shadowed = true
				return false
			}
		case *ast.FuncLit:
			if declaration.Type != nil && declaration.Type.Params != nil {
				for _, field := range declaration.Type.Params.List {
					for _, ident := range field.Names {
						if allowed[ident.Name] {
							shadowed = true
							return false
						}
					}
				}
			}
		}
		return !shadowed
	})
	return shadowed
}

func pureGoUnusedExpression(allowed map[string]bool, expression ast.Expr) (bool, bool) {
	switch value := expression.(type) {
	case *ast.BasicLit:
		return true, false
	case *ast.Ident:
		return allowed[value.Name], allowed[value.Name]
	case *ast.ParenExpr:
		return pureGoUnusedExpression(allowed, value.X)
	case *ast.UnaryExpr:
		if value.Op != gotoken.ADD && value.Op != gotoken.SUB {
			return false, false
		}
		pure, _ := pureGoUnusedExpression(allowed, value.X)
		return pure, pure
	case *ast.BinaryExpr:
		if value.Op != gotoken.ADD && value.Op != gotoken.SUB && value.Op != gotoken.MUL {
			return false, false
		}
		leftPure, leftComputed := pureGoUnusedExpression(allowed, value.X)
		rightPure, rightComputed := pureGoUnusedExpression(allowed, value.Y)
		return leftPure && rightPure, leftComputed || rightComputed
	default:
		return false, false
	}
}
