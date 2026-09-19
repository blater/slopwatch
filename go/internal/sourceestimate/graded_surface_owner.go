package sourceestimate

func gradedFindSurfaceOwner(tokens []token, language, owner string) *gradedSurfaceOwner {
	for i := 0; i+2 < len(tokens); i++ {
		nameIndex := -1
		switch language {
		case "go":
			if tokens[i].text == "type" && i+3 < len(tokens) && tokens[i+2].text == "struct" {
				nameIndex = i + 1
			}
		case "rust":
			if tokens[i].text == "struct" && isIdentifier(tokens[i+1].text) {
				nameIndex = i + 1
			}
		default:
			if tokens[i].text == "class" && isIdentifier(tokens[i+1].text) {
				nameIndex = i + 1
			}
		}
		if nameIndex < 0 || tokens[nameIndex].text != owner {
			continue
		}
		open := nameIndex + 1
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
		return &gradedSurfaceOwner{open: open, close: close, fields: gradedSurfaceFields(tokens, language, open, close)}
	}
	return nil
}
