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
		result := make([]*facts.Statement, 0, 2)
		if statement.Init != nil {
			result = append(result, buildStatement(b, statement.Init)...)
		}
		normalized := &facts.Statement{Kind: facts.StmtIf, Location: location(b, statement), Condition: buildExpression(b, statement.Cond), Body: buildStatements(b, statement.Body.List)}
		if statement.Else != nil {
			normalized.Else = buildStatement(b, statement.Else)
		}
		return append(result, normalized)
	case *ast.ForStmt:
		result := make([]*facts.Statement, 0, 2)
		if statement.Init != nil {
			result = append(result, buildStatement(b, statement.Init)...)
		}
		body := buildStatements(b, statement.Body.List)
		if statement.Post != nil {
			body = append(body, buildStatement(b, statement.Post)...)
		}
		return append(result, &facts.Statement{Kind: facts.StmtLoop, Location: location(b, statement), Condition: buildExpression(b, statement.Cond), Body: body})
	case *ast.RangeStmt:
		return []*facts.Statement{{Kind: facts.StmtLoop, Location: location(b, statement), Condition: buildExpression(b, statement.X), Body: buildStatements(b, statement.Body.List), MaySkip: true}}
	case *ast.SwitchStmt:
		result := make([]*facts.Statement, 0, 2)
		if statement.Init != nil {
			result = append(result, buildStatement(b, statement.Init)...)
		}
		return append(result, &facts.Statement{Kind: facts.StmtSwitch, Location: location(b, statement), Condition: buildExpression(b, statement.Tag), Cases: buildCases(b, statement.Body.List)})
	case *ast.TypeSwitchStmt:
		result := make([]*facts.Statement, 0, 3)
		if statement.Init != nil {
			result = append(result, buildStatement(b, statement.Init)...)
		}
		if statement.Assign != nil {
			result = append(result, buildStatement(b, statement.Assign)...)
		}
		return append(result, &facts.Statement{Kind: facts.StmtSwitch, Location: location(b, statement), Cases: buildCases(b, statement.Body.List)})
	case *ast.SelectStmt:
		return []*facts.Statement{{Kind: facts.StmtSwitch, Location: location(b, statement), Cases: buildCommCases(b, statement.Body.List)}}
	case *ast.ReturnStmt:
		return []*facts.Statement{{Kind: facts.StmtReturn, Location: location(b, statement), Expressions: buildExpressions(b, statement.Results)}}
	case *ast.BranchStmt:
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
	case *ast.ExprStmt:
		if call, ok := statement.X.(*ast.CallExpr); ok {
			if name, ok := call.Fun.(*ast.Ident); ok && name.Name == "panic" {
				return []*facts.Statement{{Kind: facts.StmtPanic, Location: location(b, statement), Expressions: buildExpressions(b, call.Args)}}
			}
		}
		return linear(statement.X)
	case *ast.AssignStmt:
		return linear(append(append([]ast.Expr{}, statement.Lhs...), statement.Rhs...)...)
	case *ast.DeclStmt:
		expressions := make([]ast.Expr, 0)
		if declaration, ok := statement.Decl.(*ast.GenDecl); ok {
			for _, spec := range declaration.Specs {
				if value, ok := spec.(*ast.ValueSpec); ok {
					expressions = append(expressions, value.Values...)
				}
			}
		}
		return linear(expressions...)
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
