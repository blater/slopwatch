package goadapter

import (
	"go/ast"
	"go/token"

	"slopslap.dev/structural/internal/facts"
)

func buildExpressions(b *analysisContext, input []ast.Expr) []*facts.Expression {
	output := make([]*facts.Expression, 0, len(input))
	for _, expression := range input {
		if expression != nil {
			output = append(output, buildExpression(b, expression))
		}
	}
	return output
}

func buildExpression(b *analysisContext, input ast.Expr) *facts.Expression {
	if input == nil {
		return nil
	}
	result := &facts.Expression{Kind: facts.ExprOther}
	children := func(expressions ...ast.Expr) {
		for _, expression := range expressions {
			if expression != nil {
				result.Children = append(result.Children, buildExpression(b, expression))
			}
		}
	}
	switch expression := input.(type) {
	case *ast.ParenExpr:
		return buildExpression(b, expression.X)
	case *ast.BinaryExpr:
		if expression.Op == token.LAND {
			result.Kind = facts.ExprAnd
		}
		if expression.Op == token.LOR {
			result.Kind = facts.ExprOr
		}
		children(expression.X, expression.Y)
	case *ast.UnaryExpr:
		if expression.Op == token.NOT {
			result.Kind = facts.ExprNot
		}
		children(expression.X)
	case *ast.CallExpr:
		switch function := expression.Fun.(type) {
		case *ast.Ident:
			result.Calls = append(result.Calls, function.Name)
		case *ast.SelectorExpr:
			if base, ok := function.X.(*ast.Ident); ok && base.Name == b.activeReceiver {
				result.Calls = append(result.Calls, function.Sel.Name)
			}
		}
		children(expression.Fun)
		for _, argument := range expression.Args {
			children(argument)
		}
	case *ast.FuncLit:
		result.Nested = append(result.Nested, functionLiteral(b, expression))
	case *ast.SelectorExpr:
		children(expression.X)
	case *ast.IndexExpr:
		children(expression.X, expression.Index)
	case *ast.IndexListExpr:
		children(expression.X)
		for _, index := range expression.Indices {
			children(index)
		}
	case *ast.SliceExpr:
		children(expression.X, expression.Low, expression.High, expression.Max)
	case *ast.TypeAssertExpr:
		children(expression.X, expression.Type)
	case *ast.StarExpr:
		children(expression.X)
	case *ast.CompositeLit:
		children(expression.Type)
		for _, element := range expression.Elts {
			if value, ok := element.(ast.Expr); ok {
				children(value)
			}
		}
	case *ast.KeyValueExpr:
		children(expression.Key, expression.Value)
	case *ast.ArrayType:
		children(expression.Len, expression.Elt)
	case *ast.MapType:
		children(expression.Key, expression.Value)
	case *ast.ChanType:
		children(expression.Value)
	case *ast.Ellipsis:
		children(expression.Elt)
	}
	return result
}
