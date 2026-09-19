package sourceestimate

// Record headers are declarations, not methods. Compact constructor bodies
// must be evaluated as construction behavior, even without resolved Java types.
func javaCompactConstructors(file File, index int, tokens []token, pkg string) (map[int]bool, []*operation) {
	headers := map[int]bool{}
	var constructors []*operation
	if normalizeLanguage(file.Language, file.Path) != "java" {
		return headers, constructors
	}
	for i := 0; i+2 < len(tokens); i++ {
		if tokens[i].text != "record" || !isIdentifier(tokens[i+1].text) {
			continue
		}
		name := tokens[i+1].text
		headers[i+1] = true
		open := i + 2
		if tokens[open].text == "<" {
			end := matching(tokens, open, "<", ">")
			if end < 0 {
				continue
			}
			open = end + 1
		}
		if open >= len(tokens) || tokens[open].text != "(" {
			continue
		}
		close := matching(tokens, open, "(", ")")
		if close < 0 {
			continue
		}
		params := tokens[open+1 : close]
		body := close + 1
		for body < len(tokens) && tokens[body].text != "{" {
			body++
		}
		if body >= len(tokens) {
			continue
		}
		end := matching(tokens, body, "{", "}")
		if end < 0 {
			continue
		}
		for j := body + 1; j < end; j++ {
			if tokens[j].text == name && j+1 < end && tokens[j+1].text == "{" {
				finish := matching(tokens, j+1, "{", "}")
				if finish < 0 {
					break
				}
				constructors = append(constructors, &operation{id: file.Path + "#compact:" + name + "/" + itoa(j), name: name, owner: name, language: "java", pkg: pkg, file: index,
					params: parameterCount(params), paramNames: parameterNames(params, "java"), stringParams: stringParameterNames(params, "java"), exposed: true, packageVisible: true, compactConstructor: true, body: tokens[j+2 : finish]})
				j = finish
			} else if tokens[j].text == "{" {
				finish := matching(tokens, j, "{", "}")
				if finish < 0 {
					break
				}
				j = finish
			}
		}
	}
	return headers, constructors
}
