package goadapter

import (
	"go/ast"
	"go/token"

	"slopslap.dev/structural/internal/facts"
)

func buildStatements(b *analysisContext, input []ast.Stmt) []*facts.Statement {
	output := make([]*facts.Statement, 0, len(input))
	for _, statement := range input {
		output = append(output, buildStatement(b, statement)...)
	}
	return output
}

func buildStatement(b *analysisContext, input ast.Stmt) []*facts.Statement {
	linear := func(expressions ...ast.Expr) []*facts.Statement {
		return []*facts.Statement{{Kind: facts.StmtLinear, Location: location(b, input), Expressions: buildExpressions(b, expressions)}}
	}
	switch statement := input.(type) {
	case *ast.BlockStmt:
		return []*facts.Statement{{Kind: facts.StmtBlock, Location: location(b, statement), Body: buildStatements(b, statement.List)}}
	case *ast.IfStmt:
		return buildIfStatement(b, statement)
	case *ast.ForStmt:
		return buildForStatement(b, statement)
	case *ast.RangeStmt:
		return []*facts.Statement{{Kind: facts.StmtLoop, Location: location(b, statement), Condition: buildExpression(b, statement.X), Body: buildStatements(b, statement.Body.List), MaySkip: true}}
	case *ast.SwitchStmt:
		return buildSwitchStatement(b, statement)
	case *ast.TypeSwitchStmt:
		return buildTypeSwitchStatement(b, statement)
	case *ast.SelectStmt:
		return []*facts.Statement{{Kind: facts.StmtSwitch, Location: location(b, statement), Cases: buildCommCases(b, statement.Body.List)}}
	case *ast.ReturnStmt:
		return []*facts.Statement{{Kind: facts.StmtReturn, Location: location(b, statement), Expressions: buildExpressions(b, statement.Results)}}
	case *ast.BranchStmt:
		return buildBranchStatement(b, statement)
	case *ast.ExprStmt:
		if panicStatement := buildPanicStatement(b, statement); panicStatement != nil {
			return panicStatement
		}
		return linear(statement.X)
	case *ast.AssignStmt:
		return linear(append(append([]ast.Expr{}, statement.Lhs...), statement.Rhs...)...)
	case *ast.DeclStmt:
		return linear(declarationExpressions(statement)...)
	case *ast.GoStmt:
		return linear(statement.Call)
	case *ast.DeferStmt:
		return linear(statement.Call)
	case *ast.SendStmt:
		return linear(statement.Chan, statement.Value)
	case *ast.IncDecStmt:
		return linear(statement.X)
	case *ast.LabeledStmt:
		return buildStatement(b, statement.Stmt)
	case *ast.EmptyStmt:
		return nil
	default:
		return []*facts.Statement{{Kind: facts.StmtLinear, Location: location(b, input)}}
	}
}

func buildIfStatement(b *analysisContext, statement *ast.IfStmt) []*facts.Statement {
	result := buildOptionalStatement(b, statement.Init)
	normalized := &facts.Statement{Kind: facts.StmtIf, Location: location(b, statement), Condition: buildExpression(b, statement.Cond), Body: buildStatements(b, statement.Body.List)}
	if statement.Else != nil {
		normalized.Else = buildStatement(b, statement.Else)
	}
	return append(result, normalized)
}

func buildForStatement(b *analysisContext, statement *ast.ForStmt) []*facts.Statement {
	result := buildOptionalStatement(b, statement.Init)
	body := buildStatements(b, statement.Body.List)
	body = append(body, buildOptionalStatement(b, statement.Post)...)
	return append(result, &facts.Statement{Kind: facts.StmtLoop, Location: location(b, statement), Condition: buildExpression(b, statement.Cond), Body: body})
}

func buildSwitchStatement(b *analysisContext, statement *ast.SwitchStmt) []*facts.Statement {
	result := buildOptionalStatement(b, statement.Init)
	return append(result, &facts.Statement{Kind: facts.StmtSwitch, Location: location(b, statement), Condition: buildExpression(b, statement.Tag), Cases: buildCases(b, statement.Body.List)})
}

func buildTypeSwitchStatement(b *analysisContext, statement *ast.TypeSwitchStmt) []*facts.Statement {
	result := buildOptionalStatement(b, statement.Init)
	result = append(result, buildOptionalStatement(b, statement.Assign)...)
	return append(result, &facts.Statement{Kind: facts.StmtSwitch, Location: location(b, statement), Cases: buildCases(b, statement.Body.List)})
}

func buildOptionalStatement(b *analysisContext, statement ast.Stmt) []*facts.Statement {
	if statement == nil {
		return nil
	}
	return buildStatement(b, statement)
}

func buildBranchStatement(b *analysisContext, statement *ast.BranchStmt) []*facts.Statement {
	kind := facts.StmtLinear
	switch statement.Tok {
	case token.BREAK:
		kind = facts.StmtBreak
	case token.CONTINUE:
		kind = facts.StmtContinue
	case token.GOTO:
		kind = facts.StmtGoto
	}
	return []*facts.Statement{{Kind: kind, Location: location(b, statement), Labeled: statement.Label != nil}}
}

func buildPanicStatement(b *analysisContext, statement *ast.ExprStmt) []*facts.Statement {
	call, ok := statement.X.(*ast.CallExpr)
	if !ok {
		return nil
	}
	name, ok := call.Fun.(*ast.Ident)
	if !ok || name.Name != "panic" {
		return nil
	}
	return []*facts.Statement{{Kind: facts.StmtPanic, Location: location(b, statement), Expressions: buildExpressions(b, call.Args)}}
}

func declarationExpressions(statement *ast.DeclStmt) []ast.Expr {
	declaration, ok := statement.Decl.(*ast.GenDecl)
	if !ok {
		return nil
	}
	var expressions []ast.Expr
	for _, spec := range declaration.Specs {
		if value, ok := spec.(*ast.ValueSpec); ok {
			expressions = append(expressions, value.Values...)
		}
	}
	return expressions
}

func buildCases(b *analysisContext, statements []ast.Stmt) []facts.Case {
	output := make([]facts.Case, 0, len(statements))
	for _, statement := range statements {
		clause, ok := statement.(*ast.CaseClause)
		if !ok {
			continue
		}
		falls := false
		if len(clause.Body) > 0 {
			if branch, ok := clause.Body[len(clause.Body)-1].(*ast.BranchStmt); ok && branch.Tok == token.FALLTHROUGH {
				falls = true
			}
		}
		output = append(output, facts.Case{Default: len(clause.List) == 0, FallsThrough: falls, Expressions: buildExpressions(b, clause.List), Body: buildStatements(b, clause.Body)})
	}
	return output
}

func buildCommCases(b *analysisContext, statements []ast.Stmt) []facts.Case {
	output := make([]facts.Case, 0, len(statements))
	for _, statement := range statements {
		clause, ok := statement.(*ast.CommClause)
		if !ok {
			continue
		}
		expressions := make([]*facts.Expression, 0)
		if clause.Comm != nil {
			for _, normalized := range buildStatement(b, clause.Comm) {
				expressions = append(expressions, normalized.Expressions...)
			}
		}
		output = append(output, facts.Case{Default: clause.Comm == nil, Expressions: expressions, Body: buildStatements(b, clause.Body)})
	}
	return output
}
