package sourceestimate

import "strconv"

func gradedBoundsProtection(op *operation, u unit, body []token, access int, data, index string, expression []token) bool {
	expr := gradedConstraintExpression(op, u, expression)
	offset := 0
	if expr == index+"-1" {
		offset = 1
	} else if expr != index {
		return false
	}
	lower := map[string]bool{index + "<0": true}
	upper := index + ">=size(" + data + ")"
	if offset == 1 {
		lower = map[string]bool{index + "<=0": true, index + "<1": true}
		upper = index + ">size(" + data + ")"
	}
	for i := 0; i < access; i++ {
		if body[i].text != "if" || !gradedUnconditional(body, i) {
			continue
		}
		open := i + 1
		for open < access && body[open].text != "{" {
			open++
		}
		if open >= access {
			continue
		}
		end := matching(body, open, "{", "}")
		if end < 0 || end >= access || open+1 >= end {
			continue
		}
		// Reject must itself be unconditional, not hidden in a nested branch.
		first := body[open+1].text
		if first != "return" && first != "throw" && !(first == "panic" && op.language == "go" && gradedBuiltinName(u, op, "panic")) {
			continue
		}
		cond := gradedResultUngroup(body[i+1 : open])
		split := -1
		nesting := 0
		for j, t := range cond {
			switch t.text {
			case "(", "[":
				nesting++
			case ")", "]":
				nesting--
			}
			if nesting == 0 && t.text == "||" {
				if split >= 0 {
					split = -1
					break
				}
				split = j
			}
		}
		if split < 0 {
			number, err := strconv.Atoi(expr)
			condition := gradedConstraintExpression(op, u, cond)
			constantGuard := err == nil && number >= 0 && (condition == "size("+data+")<="+expr || condition == "size("+data+")<"+strconv.Itoa(number+1) || number == 0 && condition == "size("+data+")==0")
			if constantGuard && !gradedConstraintChanged(op, u, body, end+1, access, []string{data}) {
				return true
			}
			continue
		}
		left, right := gradedConstraintExpression(op, u, cond[:split]), gradedConstraintExpression(op, u, cond[split+1:])
		if !(lower[left] && right == upper || lower[right] && left == upper) {
			continue
		}
		if !gradedConstraintChanged(op, u, body, end+1, access, []string{data, index}) {
			return true
		}

	}
	return false
}
