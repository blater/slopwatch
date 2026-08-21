package goadapter

import (
	"fmt"
	"go/ast"

	"slopslap.dev/structural/internal/facts"
)

func location(b *analysisContext, node ast.Node) facts.Location {
	start := b.fset.PositionFor(node.Pos(), false)
	end := b.fset.PositionFor(node.End(), false)
	return facts.Location{Path: b.current.rel, Line: start.Line, Column: start.Column, EndLine: end.Line, EndColumn: end.Column}
}

func receiver(expr ast.Expr) string {
	switch value := expr.(type) {
	case *ast.Ident:
		return value.Name
	case *ast.StarExpr:
		return receiver(value.X)
	case *ast.IndexExpr:
		return receiver(value.X)
	case *ast.IndexListExpr:
		return receiver(value.X)
	case *ast.ParenExpr:
		return receiver(value.X)
	default:
		return ""
	}
}

func receiverVariable(declaration *ast.FuncDecl) string {
	if declaration.Recv == nil || len(declaration.Recv.List) == 0 || len(declaration.Recv.List[0].Names) == 0 {
		return ""
	}
	return declaration.Recv.List[0].Names[0].Name
}

func functionDeclaration(b *analysisContext, declaration *ast.FuncDecl) *facts.Function {
	receiverType := ""
	if declaration.Recv != nil && len(declaration.Recv.List) > 0 {
		receiverType = receiver(declaration.Recv.List[0].Type)
	}
	function := &facts.Function{
		Name: declaration.Name.Name, Receiver: receiverType,
		ReceiverVar: receiverVariable(declaration), Location: location(b, declaration),
	}
	previousReceiver := b.activeReceiver
	b.activeReceiver = function.ReceiverVar
	function.Body = buildStatements(b, declaration.Body.List)
	b.activeReceiver = previousReceiver
	return function
}

func functionLiteral(b *analysisContext, literal *ast.FuncLit) *facts.Function {
	position := b.fset.PositionFor(literal.Pos(), false)
	function := &facts.Function{
		Name:     fmt.Sprintf("<anonymous@%d:%d>", position.Line, position.Column),
		Location: location(b, literal), Body: buildStatements(b, literal.Body.List),
	}
	b.functions = append(b.functions, function)
	return function
}
