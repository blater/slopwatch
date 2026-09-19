package sourceestimate

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

func gradedRejectingConsumerCondition(body []token, i int, calls []call) ([]token, bool) {
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
		return nil, false
	}

	start := close + 1
	if start < len(body) && body[start].text == "{" {
		start++
	}
	if start >= len(body) || body[start].text != "return" && body[start].text != "throw" {
		return nil, false
	}
	end := statementEnd(body, start)
	useful := false
	for _, c := range calls {
		useful = useful || c.position > end
	}
	if !useful {
		return nil, false
	}
	return body[conditionStart:close], true
}

func gradedConsumerClauses(op *operation, condition []token, deps func([]token) map[string]bool) []gradedConsumerConstraint {
	result := []gradedConsumerConstraint{}
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
	return result
}
