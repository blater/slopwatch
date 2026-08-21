package metrics

import (
	"math/big"

	"slopslap.dev/structural/internal/facts"
)

type flow struct {
	next, returns, panics, breaks, continues, loopbacks *big.Int
}

type conditionPaths struct {
	end, whenTrue, whenFalse *big.Int
}

func number(value int64) *big.Int { return big.NewInt(value) }

func copyNumber(value *big.Int) *big.Int { return new(big.Int).Set(value) }

func sum(values ...*big.Int) *big.Int {
	result := new(big.Int)
	for _, value := range values {
		result.Add(result, value)
	}
	return result
}

func emptyFlow(next *big.Int) flow {
	return flow{copyNumber(next), number(0), number(0), number(0), number(0), number(0)}
}

func combineFlow(left, right flow) flow {
	return flow{
		sum(left.next, right.next), sum(left.returns, right.returns),
		sum(left.panics, right.panics), sum(left.breaks, right.breaks),
		sum(left.continues, right.continues), sum(left.loopbacks, right.loopbacks),
	}
}

func expressionPaths(expression *facts.Expression, incoming *big.Int) conditionPaths {
	if expression == nil {
		return conditionPaths{copyNumber(incoming), copyNumber(incoming), copyNumber(incoming)}
	}
	if expression.Kind == facts.ExprNot && len(expression.Children) > 0 {
		operand := expressionPaths(expression.Children[0], incoming)
		return conditionPaths{operand.end, operand.whenFalse, operand.whenTrue}
	}
	if (expression.Kind == facts.ExprAnd || expression.Kind == facts.ExprOr) && len(expression.Children) >= 2 {
		left := expressionPaths(expression.Children[0], incoming)
		if expression.Kind == facts.ExprAnd {
			right := expressionPaths(expression.Children[1], left.whenTrue)
			return conditionPaths{
				sum(left.whenFalse, right.end), right.whenTrue,
				sum(left.whenFalse, right.whenFalse),
			}
		}
		right := expressionPaths(expression.Children[1], left.whenFalse)
		return conditionPaths{
			sum(left.whenTrue, right.end), sum(left.whenTrue, right.whenTrue),
			right.whenFalse,
		}
	}
	return conditionPaths{copyNumber(incoming), copyNumber(incoming), copyNumber(incoming)}
}

func sequence(statements []*facts.Statement, incoming *big.Int) flow {
	result := emptyFlow(incoming)
	for _, statement := range statements {
		current := statementFlow(statement, result.next)
		result = flow{
			current.next, sum(result.returns, current.returns),
			sum(result.panics, current.panics), sum(result.breaks, current.breaks),
			sum(result.continues, current.continues),
			sum(result.loopbacks, current.loopbacks),
		}
	}
	return result
}

func expressionsEnd(expressions []*facts.Expression, incoming *big.Int) *big.Int {
	next := copyNumber(incoming)
	for _, expression := range expressions {
		next = expressionPaths(expression, next).end
	}
	return next
}

func statementFlow(statement *facts.Statement, incoming *big.Int) flow {
	if incoming.Sign() == 0 {
		return emptyFlow(number(0))
	}
	switch statement.Kind {
	case facts.StmtBlock:
		return sequence(statement.Body, incoming)
	case facts.StmtIf:
		condition := expressionPaths(statement.Condition, incoming)
		thenFlow := sequence(statement.Body, condition.whenTrue)
		elseFlow := emptyFlow(condition.whenFalse)
		if len(statement.Else) > 0 {
			elseFlow = sequence(statement.Else, condition.whenFalse)
		}
		return combineFlow(thenFlow, elseFlow)
	case facts.StmtReturn:
		exits := expressionsEnd(statement.Expressions, incoming)
		result := emptyFlow(number(0))
		result.returns = exits
		return result
	case facts.StmtPanic:
		result := emptyFlow(number(0))
		result.panics = expressionsEnd(statement.Expressions, incoming)
		return result
	case facts.StmtBreak:
		result := emptyFlow(number(0))
		result.breaks = copyNumber(incoming)
		return result
	case facts.StmtContinue:
		result := emptyFlow(number(0))
		result.continues = copyNumber(incoming)
		return result
	case facts.StmtGoto:
		result := emptyFlow(number(0))
		result.returns = copyNumber(incoming)
		return result
	case facts.StmtLoop:
		condition := conditionPaths{copyNumber(incoming), copyNumber(incoming), number(0)}
		if statement.Condition != nil {
			condition = expressionPaths(statement.Condition, incoming)
		} else if statement.MaySkip {
			condition.whenFalse = copyNumber(incoming)
		}
		body := sequence(statement.Body, condition.whenTrue)
		return flow{
			sum(condition.whenFalse, body.breaks), body.returns, body.panics,
			number(0), number(0), sum(body.loopbacks, body.next, body.continues),
		}
	case facts.StmtSwitch:
		return switchFlow(statement, incoming)
	default:
		return emptyFlow(expressionsEnd(statement.Expressions, incoming))
	}
}

func switchFlow(statement *facts.Statement, incoming *big.Int) flow {
	result := emptyFlow(number(0))
	falling := number(0)
	hasDefault := false
	for _, branch := range statement.Cases {
		if branch.Default {
			hasDefault = true
		}
		start := copyNumber(incoming)
		if falling.Sign() > 0 {
			start = sum(start, falling)
		}
		body := sequence(branch.Body, start)
		result.next = sum(result.next, body.breaks)
		result.returns = sum(result.returns, body.returns)
		result.panics = sum(result.panics, body.panics)
		result.continues = sum(result.continues, body.continues)
		result.loopbacks = sum(result.loopbacks, body.loopbacks)
		if branch.FallsThrough {
			falling = body.next
		} else {
			result.next = sum(result.next, body.next)
			falling = number(0)
		}
	}
	result.next = sum(result.next, falling)
	if !hasDefault {
		result.next = sum(result.next, incoming)
	}
	return result
}

// NPath calculates exact acyclic execution paths without integer truncation.
func NPath(function *facts.Function) *big.Int {
	result := sequence(function.Body, number(1))
	return sum(result.next, result.returns, result.panics, result.loopbacks)
}
