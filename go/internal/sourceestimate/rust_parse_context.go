package sourceestimate

// Source-order interval events preserve the bounded parser's exact rules:
// unmatched module bodies impose no visibility restriction; test attributes
// cover positions after the attribute, including the following signature.
type rustParseContext struct {
	moduleVisible, testOnly, barePub, restricted                                          []bool
	braceEnds, parenEnds, angleEnds, nextBoundary, nextFunctionBoundary, nextMatchedBrace []int
}

func newRustParseContext(tokens []token) rustParseContext {
	n := len(tokens)
	result := rustParseContext{moduleVisible: make([]bool, n), testOnly: make([]bool, n), barePub: make([]bool, n), restricted: make([]bool, n)}
	braceEnds, bracketEnds := make([]int, n), make([]int, n)
	for i := 0; i < n; i++ {
		braceEnds[i] = -1
		bracketEnds[i] = -1
	}
	result.parenEnds = make([]int, n)
	result.angleEnds = make([]int, n)
	for i := range tokens {
		result.parenEnds[i] = -1
		result.angleEnds[i] = -1
	}
	braces, brackets, parens, angles := []int{}, []int{}, []int{}, []int{}
	for i, t := range tokens {
		switch t.text {
		case "(":
			parens = append(parens, i)
		case ")":
			if len(parens) > 0 {
				open := parens[len(parens)-1]
				parens = parens[:len(parens)-1]
				result.parenEnds[open] = i
			}
		case "<":
			angles = append(angles, i)
		case ">":
			if len(angles) > 0 {
				open := angles[len(angles)-1]
				angles = angles[:len(angles)-1]
				result.angleEnds[open] = i
			}
		case "{":
			braces = append(braces, i)
		case "}":
			if len(braces) > 0 {
				open := braces[len(braces)-1]
				braces = braces[:len(braces)-1]
				braceEnds[open] = i
			}
		case "[":
			brackets = append(brackets, i)
		case "]":
			if len(brackets) > 0 {
				open := brackets[len(brackets)-1]
				brackets = brackets[:len(brackets)-1]
				bracketEnds[open] = i
			}
		}
	}
	nextBoundary := make([]int, n+1)
	nextBoundary[n] = n
	for i := n - 1; i >= 0; i-- {
		nextBoundary[i] = nextBoundary[i+1]
		if tokens[i].text == "{" || tokens[i].text == ";" {
			nextBoundary[i] = i
		}
	}
	result.braceEnds = braceEnds
	result.nextBoundary = nextBoundary
	result.nextFunctionBoundary = make([]int, n+1)
	result.nextFunctionBoundary[n] = n
	result.nextMatchedBrace = make([]int, n+1)
	result.nextMatchedBrace[n] = n
	for i := n - 1; i >= 0; i-- {
		result.nextFunctionBoundary[i] = result.nextFunctionBoundary[i+1]
		result.nextMatchedBrace[i] = result.nextMatchedBrace[i+1]
		if tokens[i].text == "{" || tokens[i].text == ";" || tokens[i].text == "=>" {
			result.nextFunctionBoundary[i] = i
		}
		if tokens[i].text == "{" && braceEnds[i] >= 0 {
			result.nextMatchedBrace[i] = i
		}
	}
	bare, restricted := false, false
	for i, t := range tokens {
		result.barePub[i] = bare || i > 0 && tokens[i-1].text == "pub"
		result.restricted[i] = restricted
		if t.text == "{" || t.text == "}" || t.text == ";" {
			bare = false
			restricted = false
		}
		if t.text == "pub" && (i+1 == n || tokens[i+1].text != "(") {
			bare = true
		}
		if t.text == "(" && i > 0 && tokens[i-1].text == "pub" {
			restricted = true
		}
	}
	hidden, test := make([]int, n+1), make([]int, n+1)
	for i, t := range tokens {
		if t.text == "mod" && i+1 < n && isIdentifier(tokens[i+1].text) {
			body := nextBoundary[i+2]
			if body < n && tokens[body].text == "{" && braceEnds[body] >= 0 && !result.barePub[i] {
				hidden[body+1]++
				hidden[braceEnds[body]+1]--
			}
		}
		if t.text == "#" && i+1 < n && tokens[i+1].text == "[" {
			end := bracketEnds[i+1]
			if end < 0 {
				continue
			}
			if !rustTokenConcatEquals(tokens[i+2:end], "test") && !rustTokenConcatEquals(tokens[i+2:end], "cfg(test)") {
				continue
			}
			body := nextBoundary[end+1]
			if body < n && tokens[body].text == "{" && braceEnds[body] > end+1 {
				test[end+1]++
				test[braceEnds[body]]--
			}
		}
	}
	hiddenCount, testCount := 0, 0
	for i := range tokens {
		hiddenCount += hidden[i]
		testCount += test[i]
		result.moduleVisible[i] = hiddenCount == 0
		result.testOnly[i] = testCount > 0
	}
	return result
}

// Bound work by the short target while accepting arbitrary token fragments.
func rustTokenConcatEquals(tokens []token, target string) bool {
	offset := 0
	for _, t := range tokens {
		if len(t.text) > len(target)-offset || target[offset:offset+len(t.text)] != t.text {
			return false
		}
		offset += len(t.text)
	}
	return offset == len(target)
}
