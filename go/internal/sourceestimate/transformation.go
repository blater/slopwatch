package sourceestimate

import "strings"

// returnedTransformation normalizes the value which reaches an operation's
// return. It is intentionally narrower than the general source estimate: only
// straight-line bindings and resolvable same-workspace helper returns are
// followed. key is empty when the returned value carries no transformation.
// complete is false whenever the bounded proof cannot account for the value.
func returnedTransformation(op *operation, units []unit, byKey map[string][]*operation) (key string, transformed, complete bool) {
	if op == nil {
		return "", false, false
	}
	state := transformationState{units: units, byKey: byKey, active: map[string]bool{}}
	expression, ok := state.operation(op, nil, 0)
	if !ok {
		return "", false, false
	}
	if !expression.dependsOnInput {
		return "", false, !state.incomplete
	}
	if !expression.transformed {
		return "", false, !state.incomplete
	}
	return expression.key(), expression.transformed, !state.incomplete
}

type transformationState struct {
	units  []unit
	byKey  map[string][]*operation
	active map[string]bool
	calls  int
	tokens int
	// incomplete records unresolved side-effect statements that occur before
	// the returned value. Their uncertainty is reported by the normal call
	// evidence path; it must not erase independently recognized return work.
	incomplete bool
}

type transformationExpr struct {
	form           string
	args           []*transformationExpr
	dependsOnInput bool
	transformed    bool
	stringLike     bool
}

func (e *transformationExpr) key() string {
	if e == nil {
		return ""
	}
	if len(e.args) == 0 {
		return e.form
	}
	parts := make([]string, len(e.args))
	for i, arg := range e.args {
		parts[i] = arg.key()
	}
	return e.form + "(" + strings.Join(parts, ",") + ")"
}

func (s *transformationState) operation(op *operation, bindings map[string]*transformationExpr, depth int) (*transformationExpr, bool) {
	if op == nil || depth > maxCallDepth || s.active[op.id] {
		return nil, false
	}
	s.active[op.id] = true
	defer delete(s.active, op.id)
	if len(op.body) > maxTokensPerFile || s.tokens+len(op.body) > maxTokensPerFile {
		return nil, false
	}
	s.tokens += len(op.body)

	local := make(map[string]*transformationExpr, len(op.paramNames)+len(bindings))
	for index, name := range op.paramNames {
		if value, ok := bindings[name]; ok {
			local[name] = value
		} else {
			local[name] = &transformationExpr{form: "p" + itoa(index), dependsOnInput: true, stringLike: op.stringParams[name]}
		}
	}
	for name, value := range bindings {
		if _, exists := local[name]; !exists {
			local[name] = value
		}
	}

	body := pruneDeadFalseBranches(op.body)
	returnIndex := -1
	for index, item := range body {
		if item.text == "return" {
			returnIndex = index
			break
		}
	}
	if returnIndex >= 0 {
		if !straightLinePrefix(body[:returnIndex]) {
			return nil, false
		}
		for _, statement := range splitTransformationStatements(body[:returnIndex]) {
			if len(statement) == 0 {
				continue
			}
			if !s.bindStatement(op, local, statement, depth) {
				if discarded, standalone := standaloneTransformationCall(statement); standalone {
					if len(resolveCall(op, discarded, s.units, s.byKey)) != 1 {
						s.incomplete = true
					}
					continue
				}
				return nil, false
			}
		}
		returned := firstTransformationExpression(body[returnIndex+1:])
		if len(returned) == 0 {
			return nil, false
		}
		parts := splitArguments(returned)
		if len(parts) != 1 {
			return nil, false
		}
		return s.expression(op, parts[0], local, depth)
	}
	if !straightLinePrefix(body) || len(body) == 0 {
		return nil, false
	}
	return s.expression(op, trimTransformationDelimiters(body), local, depth)
}

// standaloneTransformationCall identifies a call statement whose result is
// discarded. An unresolved call here is side-effect uncertainty; it does not
// determine the value returned by a later, independently parseable expression.
func standaloneTransformationCall(statement []token) (call, bool) {
	statement = trimSemicolonTokens(statement)
	if len(statement) == 0 {
		return call{}, false
	}
	calls := callsIn(statement)
	if len(calls) != 1 {
		return call{}, false
	}
	selected := calls[0]
	if selected.position < 0 || selected.position+1 >= len(statement) {
		return call{}, false
	}
	close := matching(statement, selected.position+1, "(", ")")
	return selected, close == len(statement)-1
}

func (s *transformationState) expression(op *operation, body []token, bindings map[string]*transformationExpr, depth int) (*transformationExpr, bool) {
	parser := transformationParser{state: s, operation: op, bindings: bindings, tokens: body, depth: depth}
	value, ok := parser.parse(0)
	if !ok || parser.position != len(body) {
		return nil, false
	}
	return value, true
}

func (s *transformationState) helper(op *operation, call call, bindings map[string]*transformationExpr, depth int) (*transformationExpr, bool) {
	if s.calls >= maxCallsPerRoot || depth >= maxCallDepth {
		return nil, false
	}
	s.calls++
	actuals := make([]*transformationExpr, len(call.actuals))
	for index, actual := range call.actuals {
		value, ok := s.expression(op, actual, bindings, depth)
		if !ok {
			return nil, false
		}
		actuals[index] = value
	}
	candidates := resolveCall(op, call, s.units, s.byKey)
	if len(candidates) != 1 {
		return nil, false
	}
	callee := candidates[0]
	childBindings := make(map[string]*transformationExpr, len(callee.paramNames))
	for index, formal := range callee.paramNames {
		if index >= len(actuals) {
			return nil, false
		}
		childBindings[formal] = actuals[index]
	}
	return s.operation(callee, childBindings, depth+1)
}

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
	if item.text == "+" || item.text == "-" || item.text == "!" || item.text == "~" {
		p.position++
		value, ok := p.parse(transformationPrecedence("unary"))
		if !ok {
			return nil, false
		}
		if item.text == "+" {
			return value, true
		}
		return &transformationExpr{form: item.text, args: []*transformationExpr{value}, dependsOnInput: value.dependsOnInput, transformed: value.transformed || value.dependsOnInput && isTransformationOperator(item.text), stringLike: value.stringLike}, true
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

func combineTransformation(operator string, left, right *transformationExpr) *transformationExpr {
	if left == nil || right == nil {
		return nil
	}
	if neutralTransformation(operator, left, right) {
		if operator == "*" && isTransformationConstant(left, "0") {
			return &transformationExpr{form: "0"}
		}
		if operator == "*" && isTransformationConstant(right, "0") {
			return &transformationExpr{form: "0"}
		}
		if operator == "-" && left.key() == right.key() {
			return &transformationExpr{form: "0"}
		}
		if isTransformationConstant(left, "0") || isTransformationConstant(left, "__empty_string__") {
			return right
		}
		return left
	}
	return &transformationExpr{
		form:           operator,
		args:           []*transformationExpr{left, right},
		dependsOnInput: left.dependsOnInput || right.dependsOnInput,
		transformed:    left.transformed || right.transformed || (left.dependsOnInput || right.dependsOnInput) && isTransformationOperator(operator),
		stringLike:     operator == "+" && (left.stringLike || right.stringLike),
	}
}

func neutralTransformation(operator string, left, right *transformationExpr) bool {
	switch operator {
	case "+":
		return (!left.stringLike && !right.stringLike && (isTransformationConstant(left, "0") || isTransformationConstant(right, "0"))) || isTransformationConstant(left, "__empty_string__") || isTransformationConstant(right, "__empty_string__")
	case "-":
		return isTransformationConstant(right, "0")
	case "*":
		return isTransformationConstant(left, "0") || isTransformationConstant(left, "1") || isTransformationConstant(right, "0") || isTransformationConstant(right, "1")
	case "/":
		return isTransformationConstant(right, "1")
	}
	return false
}

func isTransformationConstant(value *transformationExpr, text string) bool {
	return value != nil && !value.dependsOnInput && value.form == text
}

func isTransformationOperator(operator string) bool {
	switch operator {
	case "+", "-", "*", "/", "%", "&", "|", "^", "<<", ">>":
		return true
	default:
		return false
	}
}

func straightLinePrefix(body []token) bool {
	for i, item := range body {
		if (item.text == "panic" || item.text == "raise") && i+1 < len(body) && body[i+1].text == "(" {
			return false
		}
		switch item.text {
		case "if", "for", "while", "switch", "match", "try", "catch", "defer", "throw", "await", "yield", "go", "select":
			return false
		}
	}
	return true
}

// pruneDeadFalseBranches removes only syntactically explicit always-false if
// branches. It does not attempt general constant folding, so an uncertain
// condition still makes the caller incomplete.
func pruneDeadFalseBranches(body []token) []token {
	if len(body) == 0 {
		return body
	}
	result := make([]token, 0, len(body))
	for index := 0; index < len(body); {
		if body[index].text != "if" {
			result = append(result, body[index])
			index++
			continue
		}
		end, ok := deadFalseBranchEnd(body, index)
		if !ok {
			result = append(result, body[index])
			index++
			continue
		}
		index = end
	}
	return result
}

func deadFalseBranchEnd(body []token, start int) (int, bool) {
	conditionStart := start + 1
	conditionEnd := conditionStart
	if conditionStart < len(body) && body[conditionStart].text == "(" {
		close := matching(body, conditionStart, "(", ")")
		if close < 0 {
			return 0, false
		}
		conditionStart++
		conditionEnd = close
	} else {
		for conditionEnd < len(body) && body[conditionEnd].text != "{" && body[conditionEnd].text != ";" {
			conditionEnd++
		}
	}
	condition := body[conditionStart:conditionEnd]
	if !alwaysFalseCondition(condition) {
		return 0, false
	}
	bodyStart := conditionEnd
	if bodyStart < len(body) && body[bodyStart].text == ")" {
		bodyStart++
	}
	if bodyStart < len(body) && body[bodyStart].text == "{" {
		close := matching(body, bodyStart, "{", "}")
		if close < 0 || close+1 < len(body) && body[close+1].text == "else" {
			return 0, false
		}
		return close + 1, true
	}
	for bodyStart < len(body) && body[bodyStart].text != ";" {
		bodyStart++
	}
	if bodyStart < len(body) {
		return bodyStart + 1, true
	}
	return len(body), true
}

func alwaysFalseCondition(condition []token) bool {
	for len(condition) >= 2 && condition[0].text == "(" && condition[len(condition)-1].text == ")" {
		if matching(condition, 0, "(", ")") != len(condition)-1 {
			break
		}
		condition = condition[1 : len(condition)-1]
	}
	return len(condition) == 1 && condition[0].text == "false"
}

func splitTransformationStatements(body []token) [][]token {
	result := make([][]token, 0, 4)
	start, depth := 0, 0
	for index, item := range body {
		switch item.text {
		case "(", "[", "{":
			depth++
		case ")", "]", "}":
			if depth > 0 {
				depth--
			}
		case ";":
			if depth == 0 {
				result = append(result, body[start:index])
				start = index + 1
			}
		}
	}
	if start < len(body) {
		result = append(result, body[start:])
	}
	return result
}

func (s *transformationState) bindStatement(op *operation, bindings map[string]*transformationExpr, statement []token, depth int) bool {
	if len(statement) < 3 {
		return false
	}
	nameIndex, operatorIndex := 0, 1
	if statement[0].text == "let" || statement[0].text == "var" || statement[0].text == "const" {
		nameIndex, operatorIndex = 1, 2
	}
	if nameIndex >= len(statement) || !isIdentifier(statement[nameIndex].text) || operatorIndex >= len(statement) || statement[operatorIndex].text != "=" && statement[operatorIndex].text != ":=" {
		return false
	}
	rhs := statement[operatorIndex+1:]
	parser := transformationParser{state: s, operation: op, tokens: rhs, bindings: bindings, depth: depth}
	value, ok := parser.parse(0)
	if !ok || parser.position != len(rhs) {
		return false
	}
	bindings[statement[nameIndex].text] = value
	return true
}

func trimTransformationDelimiters(body []token) []token {
	start, end := 0, len(body)
	for start < end && body[start].text == ";" {
		start++
	}
	for end > start && (body[end-1].text == ";" || body[end-1].text == "}") {
		end--
	}
	return body[start:end]
}

func firstTransformationExpression(body []token) []token {
	body = trimTransformationDelimiters(body)
	depth := 0
	for index, item := range body {
		switch item.text {
		case "(", "[", "{":
			depth++
		case ")", "]", "}":
			if depth == 0 {
				return body[:index]
			}
			depth--
		case ";":
			if depth == 0 {
				return body[:index]
			}
		}
	}
	return body
}
