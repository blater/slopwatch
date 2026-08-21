package metrics

import "slopslap.dev/structural/internal/facts"

// Cyclomatic calculates PMD-style cyclomatic complexity with a baseline of one.
func Cyclomatic(function *facts.Function) int {
	value := 1
	walkCyclomatic(function.Body, &value)
	return value
}

func booleanComplexity(expression *facts.Expression) int {
	if expression == nil {
		return 0
	}
	value := 0
	if expression.Kind == facts.ExprAnd || expression.Kind == facts.ExprOr {
		value++
	}
	for _, child := range expression.Children {
		value += booleanComplexity(child)
	}
	return value
}

func walkCyclomatic(statements []*facts.Statement, value *int) {
	for _, statement := range statements {
		switch statement.Kind {
		case facts.StmtBlock:
			walkCyclomatic(statement.Body, value)
		case facts.StmtIf, facts.StmtLoop:
			*value += 1 + booleanComplexity(statement.Condition)
			walkCyclomatic(statement.Body, value)
			walkCyclomatic(statement.Else, value)
		case facts.StmtSwitch:
			*value += booleanComplexity(statement.Condition)
			for _, branch := range statement.Cases {
				if !branch.Default {
					*value++
				}
				for _, expression := range branch.Expressions {
					*value += booleanComplexity(expression)
				}
				walkCyclomatic(branch.Body, value)
			}
		case facts.StmtBreak, facts.StmtContinue, facts.StmtGoto, facts.StmtPanic:
			*value++
		default:
			for _, expression := range statement.Expressions {
				*value += booleanComplexity(expression)
			}
		}
	}
}
