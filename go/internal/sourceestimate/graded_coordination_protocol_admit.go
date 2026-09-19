package sourceestimate

func gradedProtocolReadAdmits(op *operation, u unit, index int, reset string) bool {
	body := op.body
	for i := 0; i < index; i++ {
		assertion := op.language == "rust" && body[i].text == "assert" && i+2 < len(body) && body[i+1].text == "!"
		if body[i].text != "if" && !assertion {
			continue
		}
		start := i + 1
		if assertion {
			start = i + 2
		}
		if start >= len(body) {
			continue
		}
		end, after := start, start
		if body[start].text == "(" {
			end = matching(body, start, "(", ")")
			if end < 0 {
				continue
			}
			start++
			after = end + 1
		} else {
			for end < len(body) && body[end].text != "{" {
				end++
			}
			after = end
		}
		if index < start || index >= end {
			continue
		}
		value, known := gradedBooleanCondition(body[start:end], body[index].text, reset == "true")
		if !known {
			return false
		}
		if assertion {
			return !value
		}
		branch, next := gradedProtocolStatement(body, after)
		opposite := body[next:]
		if len(opposite) > 0 && opposite[0].text == "else" {
			opposite, _ = gradedProtocolStatement(body, next+1)
		}
		// The reset state must select the rejecting branch. The other branch
		// must expose an admitted return, rather than unrelated validation.
		rejects, admits := gradedProtocolOutcome(op, u, branch), gradedProtocolOutcome(op, u, opposite)
		if value {
			return rejects == -1 && admits == 1
		}
		return rejects == 1 && admits == -1
	}
	return gradedObservableProtocolRead(op, u, index)
}
