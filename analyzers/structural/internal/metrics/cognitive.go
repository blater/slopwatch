package metrics

import "slopslap.dev/structural/internal/facts"

type cognitiveState struct {
	complexity int
	nesting    int
	booleanOp  facts.ExprKind
	stack      []string
}

// Cognitive calculates the PMD/Sonar cognitive complexity of a function.
func Cognitive(function *facts.Function) int {
	state := &cognitiveState{stack: []string{function.Name}}
	walkCognitiveStatements(function.Body, state)
	return state.complexity
}

func walkCognitiveStatements(statements []*facts.Statement, state *cognitiveState) {
	for _, statement := range statements {
		state.booleanOp = facts.ExprOther
		walkCognitiveStatement(statement, state)
	}
}

func walkCognitiveIf(statement *facts.Statement, elseIf bool, state *cognitiveState) {
	walkCognitiveExpression(statement.Condition, state)
	if !elseIf {
		state.complexity += 1 + state.nesting
		state.nesting++
	}
	walkCognitiveStatements(statement.Body, state)
	if !elseIf {
		state.nesting--
	}
	if len(statement.Else) > 0 {
		state.complexity++
		state.nesting++
		if len(statement.Else) == 1 && statement.Else[0].Kind == facts.StmtIf {
			walkCognitiveIf(statement.Else[0], true, state)
		} else {
			walkCognitiveStatements(statement.Else, state)
		}
		state.nesting--
	}
}

func walkCognitiveStatement(statement *facts.Statement, state *cognitiveState) {
	switch statement.Kind {
	case facts.StmtBlock:
		walkCognitiveStatements(statement.Body, state)
	case facts.StmtIf:
		walkCognitiveIf(statement, false, state)
	case facts.StmtLoop, facts.StmtSwitch:
		walkCognitiveExpression(statement.Condition, state)
		state.complexity += 1 + state.nesting
		state.nesting++
		walkCognitiveStatements(statement.Body, state)
		for _, branch := range statement.Cases {
			for _, expression := range branch.Expressions {
				walkCognitiveExpression(expression, state)
			}
			walkCognitiveStatements(branch.Body, state)
		}
		state.nesting--
	case facts.StmtBreak, facts.StmtContinue, facts.StmtGoto:
		if statement.Labeled {
			state.complexity++
		}
	default:
		for _, expression := range statement.Expressions {
			walkCognitiveExpression(expression, state)
		}
	}
}

func walkCognitiveExpression(expression *facts.Expression, state *cognitiveState) {
	if expression == nil {
		return
	}
	switch expression.Kind {
	case facts.ExprAnd, facts.ExprOr:
		if state.booleanOp != expression.Kind {
			state.complexity++
		}
		state.booleanOp = expression.Kind
	case facts.ExprNot:
		state.booleanOp = facts.ExprOther
	case facts.ExprConditional:
		state.complexity += 1 + state.nesting
	}
	for _, call := range expression.Calls {
		for _, active := range state.stack {
			if call == active {
				state.complexity++
				break
			}
		}
	}
	for _, child := range expression.Children {
		walkCognitiveExpression(child, state)
	}
	for _, nested := range expression.Nested {
		state.nesting++
		state.stack = append(state.stack, nested.Name)
		walkCognitiveStatements(nested.Body, state)
		state.stack = state.stack[:len(state.stack)-1]
		state.nesting--
	}
}
