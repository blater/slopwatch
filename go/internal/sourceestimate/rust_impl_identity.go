package sourceestimate

func rustImplIdentity(header []token) (traitName, selfType string) {
	if len(header) > 0 && header[0].text == "<" {
		end := rustGenericEnd(header, 0)
		if end < 0 {
			return "", ""
		}
		header = header[end+1:]
	}
	depth := 0
	split := -1
	for i, t := range header {
		switch t.text {
		case "<":
			depth++
		case ">":
			depth--
		case ">>":
			depth -= 2
		}
		if depth == 0 && t.text == "where" {
			header = header[:i]
			break
		}
		if depth == 0 && t.text == "for" {
			split = i
		}
	}
	if split >= 0 && split < len(header) {
		return rustImplPathName(header[:split]), rustImplPathName(header[split+1:])
	}
	return "", rustImplPathName(header)
}
func rustGenericEnd(tokens []token, start int) int {
	depth := 0
	for i := start; i < len(tokens); i++ {
		switch tokens[i].text {
		case "<":
			depth++
		case ">":
			depth--
		case ">>":
			depth -= 2
		}
		if depth <= 0 {
			return i
		}
	}
	return -1
}

// Preserve the terminal nominal type/trait identity through qualified paths and
// generic arguments. Lifetimes and reference qualifiers never become owners.
func rustImplPathName(tokens []token) string {
	i := 0
	for i < len(tokens) {
		switch tokens[i].text {
		case "&", "*", "mut", "const", "!":
			i++
		case "'":
			i += 2
		default:
			goto path
		}
	}
path:
	if i < len(tokens) && tokens[i].text == "::" {
		i++
	}
	if i >= len(tokens) || !isIdentifier(tokens[i].text) || tokens[i].text == "fn" {
		return ""
	}
	name := tokens[i].text
	i++
	for i < len(tokens) {
		if tokens[i].text == "<" {
			end := rustGenericEnd(tokens, i)
			if end < 0 {
				return ""
			}
			i = end + 1
			continue
		}
		if tokens[i].text == "::" && i+1 < len(tokens) && isIdentifier(tokens[i+1].text) {
			name = tokens[i+1].text
			i += 2
			continue
		}
		return ""
	}
	if name == "self" || name == "crate" || name == "super" {
		return ""
	}
	return name
}

func rustTraitPublic(tokens []token, name string) bool {
	if name == "" {
		return false
	}
	for _, declaration := range rustNamedRanges(tokens, "trait") {
		if declaration.name == name && declaration.public {
			return true
		}
	}
	return false
}

func rustBarePubBefore(tokens []token, index int) bool {
	start := index - 1
	for start >= 0 && tokens[start].text != "{" && tokens[start].text != "}" && tokens[start].text != ";" {
		start--
	}
	for i := start + 1; i < index; i++ {
		if tokens[i].text != "pub" {
			continue
		}
		if i+1 < index && tokens[i+1].text == "(" {
			continue
		}
		return true
	}
	return false
}

func rustModuleVisible(tokens []token, index int) bool {
	visible := true
	for i := 0; i+1 < index; i++ {
		if tokens[i].text != "mod" || !isIdentifier(tokens[i+1].text) {
			continue
		}
		body := i + 2
		for body < index && tokens[body].text != "{" && tokens[body].text != ";" {
			body++
		}
		if body >= index || tokens[body].text != "{" {
			continue
		}
		end := matching(tokens, body, "{", "}")
		if end < index {
			continue
		}
		if !rustBarePubBefore(tokens, i) {
			visible = false
		}
	}
	return visible
}
