package sourceestimate

type transformationParser struct {
	state     *transformationState
	operation *operation
	bindings  map[string]*transformationExpr
	tokens    []token
	position  int
	depth     int
}

func (p *transformationParser) parse(minPrecedence int) (*transformationExpr, bool) {
	left, ok := p.primary()
	if !ok {
		return nil, false
	}
	for p.position < len(p.tokens) {
		operator := p.tokens[p.position].text
		precedence := transformationPrecedence(operator)
		if precedence < minPrecedence {
			break
		}
		p.position++
		right, ok := p.parse(precedence + 1)
		if !ok {
			return nil, false
		}
		left = combineTransformation(operator, left, right)
	}
	return left, true
}

func (p *transformationParser) primary() (*transformationExpr, bool) {
	if p.position >= len(p.tokens) {
		return nil, false
	}
	item := p.tokens[p.position]
	if item.text == "(" {
		return p.parenthesized()
	}
	if item.text == "+" || item.text == "-" || item.text == "!" || item.text == "~" {
		return p.unary(item.text)
	}
	if item.kind == "number" || item.kind == "string" || item.text == "true" || item.text == "false" || item.text == "nil" || item.text == "null" {
		p.position++
		value := &transformationExpr{form: item.text}
		if p.position < len(p.tokens) && p.tokens[p.position].text == "(" {
			return nil, false
		}
		return value, true
	}
	if !isIdentifier(item.text) {
		return nil, false
	}

	name := item.text
	p.position++
	if p.position+1 < len(p.tokens) && p.tokens[p.position].text == "." && isIdentifier(p.tokens[p.position+1].text) {
		name += "." + p.tokens[p.position+1].text
		p.position += 2
	}
	if p.position < len(p.tokens) && p.tokens[p.position].text == "(" {
		return p.call(name)
	}
	if value, ok := p.bindings[name]; ok {
		return value, true
	}
	return nil, false
}

func transformationPrecedence(operator string) int {
	switch operator {
	case "||":
		return 1
	case "&&":
		return 2
	case "==", "!=", "<", "<=", ">", ">=":
		return 3
	case "|":
		return 4
	case "^":
		return 5
	case "&":
		return 6
	case "<<", ">>":
		return 7
	case "+", "-":
		return 8
	case "*", "/", "%":
		return 9
	default:
		return -1
	}
}

func (p *transformationParser) parenthesized() (*transformationExpr, bool) {
	close := matching(p.tokens, p.position, "(", ")")
	if close < 0 || close == p.position+1 {
		return nil, false
	}
	value, ok := (&transformationParser{state: p.state, operation: p.operation, bindings: p.bindings, tokens: p.tokens[p.position+1 : close], depth: p.depth}).parse(0)
	if !ok {
		return nil, false
	}
	p.position = close + 1
	return value, true
}

func (p *transformationParser) unary(operator string) (*transformationExpr, bool) {
	p.position++
	value, ok := p.parse(transformationPrecedence("unary"))
	if !ok {
		return nil, false
	}
	if operator == "+" {
		return value, true
	}
	return &transformationExpr{form: operator, args: []*transformationExpr{value}, dependsOnInput: value.dependsOnInput, transformed: value.transformed || value.dependsOnInput && isTransformationOperator(operator), stringLike: value.stringLike}, true
}

func (p *transformationParser) call(name string) (*transformationExpr, bool) {
	open := p.position
	close := matching(p.tokens, open, "(", ")")
	if close < 0 {
		return nil, false
	}
	actualTokens := p.tokens[open+1 : close]
	actuals := splitArguments(actualTokens)
	value, ok := p.state.helper(p.operation, call{name: name, actuals: actuals, signature: name + "/" + joinTokens(actualTokens)}, p.bindings, p.depth)
	if !ok {
		return nil, false
	}
	p.position = close + 1
	return value, true
}
