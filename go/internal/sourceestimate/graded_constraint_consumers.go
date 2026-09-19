package sourceestimate

import "strings"

// Consumer preconditions follow actual parameter bindings through resolved
// calls. A selection/projection or two calls sharing arguments is not a
// constraint. Only rejecting relational clauses and phase admission qualify.
type gradedConsumerConstraint struct {
	fields    map[string]bool
	op        *operation
	lifecycle bool
}

func gradedConsumerConstraints(op *operation, u unit, units []unit, index map[string][]*operation, bindings map[string]map[string]bool, depth int) []gradedConsumerConstraint {
	budget := gradedSurfaceTokenLimit
	return gradedConsumerConstraintsBounded(op, u, units, index, bindings, depth, &budget)
}
func gradedConsumerConstraintsBounded(op *operation, u unit, units []unit, index map[string][]*operation, bindings map[string]map[string]bool, depth int, budget *int) []gradedConsumerConstraint {
	if len(op.body) > *budget {
		return nil
	}
	*budget -= len(op.body)

	if depth > maxCallDepth || bindings["#visited:"+op.id] != nil {
		return nil
	}
	bindings["#visited:"+op.id] = map[string]bool{}
	copy := *op
	copy.fieldTypes = cloneStringMap(op.fieldTypes)
	if copy.fieldTypes == nil {
		copy.fieldTypes = map[string]string{}
	}
	for name, field := range callerDeclaredFields(u, op.owner) {
		copy.fieldTypes[name] = field.typeName
	}
	for name, typ := range op.parameterTypes {
		copy.fieldTypes[name] = typ
	}
	op = &copy
	body := gradedEagerBody(pruneDeadFalseBranches(op.body), op.language)
	deps := func(expression []token) map[string]bool {
		result := map[string]bool{}
		for value := range gradedCallerDependencies(expression) {
			if replacement, ok := bindings[value]; ok {
				for dep := range replacement {
					result[dep] = true
				}
			} else {
				result[value] = true
			}
		}
		return result
	}
	calls := callsIn(body)
	callsAt := map[int][]call{}
	for _, c := range calls {
		callsAt[c.position] = append(callsAt[c.position], c)
	}
	result := []gradedConsumerConstraint{}
	for i, t := range body {
		if (t.text == "=" || t.text == ":=") && i > 0 && isIdentifier(body[i-1].text) && (i < 2 || body[i-2].text != ".") {
			if gradedUnconditional(body, i) {
				bindings[body[i-1].text] = deps(body[i+1 : statementEnd(body, i+1)])
			} else {
				bindings[body[i-1].text] = map[string]bool{}
			}
		}
		if t.text == "if" && i+1 < len(body) && gradedUnconditional(body, i) {
			conditionStart := i + 1
			close := conditionStart
			if body[conditionStart].text == "(" {
				close = matching(body, conditionStart, "(", ")")
				conditionStart++
			} else {
				for close < len(body) && body[close].text != "{" {
					close++
				}
			}
			if close < 0 || close >= len(body) {
				continue
			}

			start := close + 1
			if start < len(body) && body[start].text == "{" {
				start++
			}
			if start >= len(body) || body[start].text != "return" && body[start].text != "throw" {
				continue
			}
			end := statementEnd(body, start)
			useful := false
			for _, c := range calls {
				useful = useful || c.position > end
			}
			if !useful {
				continue
			}
			condition := body[conditionStart:close]
			// Boolean admission controls execution of subsequent operations. Producer
			// confirmation later requires both phases on the exact mutable location.
			if field := gradedBooleanConstraintField(condition); len(field) > 0 {
				result = append(result, gradedConsumerConstraint{fields: deps(field), op: op, lifecycle: true})
			}
			clause := 0
			for j := 0; j <= len(condition); j++ {
				if j < len(condition) && condition[j].text != "||" && condition[j].text != "&&" {
					continue
				}
				part := condition[clause:j]
				comparison := false
				for _, tok := range part {
					comparison = comparison || tok.text == "<" || tok.text == ">" || tok.text == "<=" || tok.text == ">=" || tok.text == "==" || tok.text == "!="
				}
				fields := deps(part)
				if comparison && len(fields) >= 2 {
					result = append(result, gradedConsumerConstraint{fields: fields, op: op})
				}
				clause = j + 1
			}
		}
		for _, c := range callsAt[i] {
			matches := resolveCall(op, c, units, index)
			if len(matches) != 1 {
				continue
			}
			callee := matches[0]
			if len(c.actuals) != len(callee.paramNames) {
				continue
			}
			inherited := map[string]map[string]bool{}
			for key, value := range bindings {
				if strings.HasPrefix(key, "#visited:") {
					inherited[key] = value
				}
			}
			for j, name := range callee.paramNames {
				inherited[name] = deps(c.actuals[j])
			}
			result = append(result, gradedConsumerConstraintsBounded(callee, units[callee.file], units, index, inherited, depth+1, budget)...)
			if len(result) > 128 {
				return result[:128]
			}
		}
	}
	return result
}

// Boolean polarity changes which phase admits work, not whether the caller
// must coordinate that phase. Both transitions are still required separately.
func gradedBooleanConstraintField(condition []token) []token {
	condition = gradedResultUngroup(condition)
	for len(condition) > 0 && condition[0].text == "!" {
		condition = gradedResultUngroup(condition[1:])
	}
	if len(condition) == 3 && condition[1].text == "." {
		return condition
	}
	for i, t := range condition {
		if t.text != "==" && t.text != "!=" {
			continue
		}
		left, right := gradedResultUngroup(condition[:i]), gradedResultUngroup(condition[i+1:])
		if len(right) > 0 && right[0].text == "=" {
			right = right[1:]
		}
		if len(right) == 1 && (right[0].text == "true" || right[0].text == "false") {
			return gradedBooleanConstraintField(left)
		}
		if len(left) == 1 && (left[0].text == "true" || left[0].text == "false") {
			return gradedBooleanConstraintField(right)
		}
	}
	return nil
}
