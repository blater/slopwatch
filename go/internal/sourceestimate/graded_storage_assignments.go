package sourceestimate

func gradedAliasDeclaration(body []token, index int) bool {
	if index+1 < len(body) && body[index+1].text == ":=" {
		return true
	}
	if index == 0 {
		return false
	}
	previous := body[index-1].text
	if previous == "let" || previous == "const" || previous == "var" {
		return true
	}
	return isIdentifier(previous) && previous != "return" && previous != "throw" && previous != "yield" && previous != "else"
}
func gradedAssignmentTargets(body []token, operator int) []int {
	targets := []int{}
	for end := operator - 1; end >= 0; {
		target, start := end, end
		if body[end].text == "]" {
			depth := 1
			open := end - 1
			for open >= 0 {
				if body[open].text == "]" {
					depth++
				}
				if body[open].text == "[" {
					depth--
					if depth == 0 {
						break
					}
				}
				open--
			}
			if open < 1 {
				break
			}
			target = open - 1
			start = gradedStorageReferenceStart(body, target)
		} else if body[end].text == ")" {
			depth := 1
			open := end - 1
			for open >= 0 {
				if body[open].text == ")" {
					depth++
				}
				if body[open].text == "(" {
					depth--
					if depth == 0 {
						break
					}
				}
				open--
			}
			if open < 0 {
				break
			}
			target = open + 1
			if target < end && body[target].text == "*" {
				target++
			}
			if target+1 != end {
				break
			}
			start = open
		} else {
			if !isIdentifier(body[target].text) {
				break
			}
			start = gradedStorageReferenceStart(body, target)
		}
		if start < 0 || !isIdentifier(body[target].text) {
			break
		}
		if start > 0 && body[start-1].text == "*" {
			start--
		}
		targets = append(targets, target)
		if start == 0 || body[start-1].text != "," {
			break
		}
		end = start - 2
	}
	return targets
}
