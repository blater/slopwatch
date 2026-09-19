package sourceestimate

func gradedSurfaceConstructor(op *operation) bool {
	if op == nil {
		return false
	}
	if surfaceConstructor(op) || op.name == "constructor" {
		return true
	}
	if gradedGoConstructorOwner(op) != "" {
		return true
	}
	if op.language == "rust" && op.name == "new" {
		for i := 0; i+1 < len(op.body); i++ {
			if op.body[i].text == "Self" && op.body[i+1].text == "{" {
				return true
			}
			if op.owner != "" && op.body[i].text == op.owner && op.body[i+1].text == "{" {
				return true
			}
		}
	}
	return false
}
func gradedGoConstructorOwner(op *operation) string {
	if op == nil || op.language != "go" || op.owner != "" {
		return ""
	}
	for i := 0; i+3 < len(op.body); i++ {
		if op.body[i].text != "return" {
			continue
		}
		j := i + 1
		if j < len(op.body) && op.body[j].text == "&" {
			j++
		}
		if j+1 < len(op.body) && isIdentifier(op.body[j].text) && op.body[j+1].text == "{" {
			return op.body[j].text
		}
	}
	// A common Go constructor initializes a local and returns that local. It
	// is recognized only when the returned identifier is tied to a typed
	// struct literal, avoiding arbitrary `name {` expressions in the body.
	for i := 0; i+4 < len(op.body); i++ {
		if !isIdentifier(op.body[i].text) || (op.body[i+1].text != ":=" && op.body[i+1].text != "=") {
			continue
		}
		j := i + 2
		if op.body[j].text == "&" {
			j++
		}
		if j+1 >= len(op.body) || !isIdentifier(op.body[j].text) || op.body[j+1].text != "{" {
			continue
		}
		local, owner := op.body[i].text, op.body[j].text
		for k := j + 2; k+1 < len(op.body); k++ {
			if op.body[k].text == "return" && op.body[k+1].text == local {
				return owner
			}
		}
	}
	return ""
}
func gradedInferSurfaceOwner(tokens []token, language, operationName string) string {
	if language != "rust" {
		return ""
	}
	candidate := ""
	for i := 0; i+2 < len(tokens); i++ {
		if tokens[i].text != "impl" {
			continue
		}
		owner := ""
		for j := i + 1; j < len(tokens) && tokens[j].text != "{"; j++ {
			if tokens[j].text == "for" && j+1 < len(tokens) && isIdentifier(tokens[j+1].text) {
				owner = tokens[j+1].text
				break
			}
			if owner == "" && isIdentifier(tokens[j].text) {
				owner = tokens[j].text
			}
		}
		if owner == "" {
			continue
		}
		open := i
		for open < len(tokens) && tokens[open].text != "{" {
			open++
		}
		if open >= len(tokens) {
			continue
		}
		close := matching(tokens, open, "{", "}")
		if close < 0 {
			continue
		}
		for j := open + 1; j+1 < close; j++ {
			if tokens[j].text == "fn" && tokens[j+1].text == operationName {
				if candidate != "" && candidate != owner {
					return ""
				}
				candidate = owner
				break
			}
		}
		i = close
	}
	return candidate
}
