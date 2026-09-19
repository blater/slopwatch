package goadapter

import (
	"go/ast"
	"go/token"
	"go/types"
)

func selectedDepthServiceCall(group *depthNamespace, item source, decl *ast.FuncDecl, owner *types.Func) *ast.CallExpr {
	if decl == nil || decl.Body == nil || owner == nil {
		return nil
	}
	statements := decl.Body.List
	if len(statements) == 0 {
		return nil
	}
	last, ok := statements[len(statements)-1].(*ast.ReturnStmt)
	if !ok || len(last.Results) == 0 {
		return nil
	}
	var serviceCall *ast.CallExpr
	for _, result := range last.Results {
		call, direct := result.(*ast.CallExpr)
		if !direct {
			continue
		}
		if depthLocalFunctionID(group, depthFunction{item: item, object: owner}, depthCallObject(item, call)) != "" {
			if serviceCall != nil {
				return nil
			}
			serviceCall = call
		}
	}
	if serviceCall == nil {
		return nil
	}
	if !depthForwardServiceReturn(item, owner, last, serviceCall) {
		return nil
	}
	for _, statement := range statements[:len(statements)-1] {
		guard, ok := statement.(*ast.IfStmt)
		if !ok || !depthRejectionIf(item, owner, guard) {
			return nil
		}
	}
	return serviceCall
}

func depthForwardServiceReturn(item source, owner *types.Func, result *ast.ReturnStmt, serviceCall *ast.CallExpr) bool {
	if owner == nil || result == nil || serviceCall == nil {
		return false
	}
	for _, argument := range serviceCall.Args {
		if !depthForwardArgumentAllowed(item, owner, argument) {
			return false
		}
	}
	for _, expression := range result.Results {
		if expression == serviceCall {
			continue
		}
		ident, ok := expression.(*ast.Ident)
		if !ok || ident.Name != "nil" {
			return false
		}
	}
	return true
}

func depthForwardArgumentAllowed(item source, owner *types.Func, expression ast.Expr) bool {
	for {
		paren, ok := expression.(*ast.ParenExpr)
		if !ok {
			break
		}
		expression = paren.X
	}
	if ident, ok := expression.(*ast.Ident); ok {
		signature, _ := owner.Type().(*types.Signature)
		if signature != nil {
			for index := 0; index < signature.Params().Len(); index++ {
				if item.typeInfo.ObjectOf(ident) == signature.Params().At(index) {
					return true
				}
			}
		}
	}
	if item.typeInfo != nil {
		if value, ok := item.typeInfo.Types[expression]; ok && value.Value != nil {
			return true
		}
	}
	return false
}

func depthRejectionIf(item source, owner *types.Func, statement *ast.IfStmt) bool {
	if statement == nil || statement.Init != nil || statement.Else != nil || !depthPurePredicate(item, owner, statement.Cond) {
		return false
	}
	if statement.Body == nil || len(statement.Body.List) != 1 {
		return false
	}
	switch value := statement.Body.List[0].(type) {
	case *ast.ReturnStmt:
		return depthKnownRejectionReturn(item, owner, value)
	case *ast.ExprStmt:
		call, ok := value.X.(*ast.CallExpr)
		if !ok || len(call.Args) != 1 || !depthForwardArgumentAllowed(item, owner, call.Args[0]) {
			return false
		}
		ident, ok := call.Fun.(*ast.Ident)
		if !ok {
			return false
		}
		builtin, ok := item.typeInfo.ObjectOf(ident).(*types.Builtin)
		return ok && builtin.Name() == "panic"
	default:
		return false
	}
}

func depthKnownRejectionReturn(item source, owner *types.Func, result *ast.ReturnStmt) bool {
	if !depthRejectedReturn(item, owner, result) {
		return false
	}
	if len(result.Results) == 0 {
		return false
	}
	last := result.Results[len(result.Results)-1]
	call, ok := last.(*ast.CallExpr)
	if !ok || !depthForwardErrorCall(item, call) {
		return false
	}
	for _, expression := range result.Results[:len(result.Results)-1] {
		if !depthPurePredicate(item, owner, expression) {
			return false
		}
	}
	for _, argument := range call.Args {
		if !depthPurePredicate(item, owner, argument) {
			return false
		}
	}
	return true
}

func depthForwardErrorCall(item source, call *ast.CallExpr) bool {
	if item.depthImports == nil || call == nil {
		return false
	}
	function := depthCallObject(item, call)
	entry, ok := item.depthImports.bindings[function]
	return ok && entry.ID == "go.error"
}

func depthPurePredicate(item source, owner *types.Func, expression ast.Expr) bool {
	if expression == nil {
		return false
	}
	switch value := expression.(type) {
	case *ast.ParenExpr:
		return depthPurePredicate(item, owner, value.X)
	case *ast.Ident:
		return depthForwardArgumentAllowed(item, owner, value)
	case *ast.BasicLit:
		return true
	case *ast.UnaryExpr:
		switch value.Op {
		case token.ADD, token.SUB, token.NOT:
			return depthPurePredicate(item, owner, value.X)
		default:
			return false
		}
	case *ast.BinaryExpr:
		switch value.Op {
		case token.ADD, token.SUB, token.MUL, token.QUO, token.REM,
			token.EQL, token.NEQ, token.LSS, token.LEQ, token.GTR, token.GEQ,
			token.LAND, token.LOR:
			return depthPurePredicate(item, owner, value.X) && depthPurePredicate(item, owner, value.Y)
		default:
			return false
		}
	default:
		return false
	}
}

func depthRejectedReturn(item source, owner *types.Func, result *ast.ReturnStmt) bool {
	if owner == nil || result == nil {
		return false
	}
	signature, _ := owner.Type().(*types.Signature)
	if signature == nil || signature.Results().Len() == 0 || !depthErrorType(signature.Results().At(signature.Results().Len()-1).Type()) || len(result.Results) == 0 {
		return false
	}
	last := result.Results[len(result.Results)-1]
	if ident, ok := last.(*ast.Ident); ok && ident.Name == "nil" {
		return false
	}
	call, ok := last.(*ast.CallExpr)
	return ok && depthForwardErrorCall(item, call)
}
