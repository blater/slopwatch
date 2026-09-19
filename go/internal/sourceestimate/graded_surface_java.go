package sourceestimate

func gradedJavaFields(fields map[string]gradedSurfaceField, segment []token, public, mutable bool) {
	modifiers := map[string]bool{"public": true, "private": true, "protected": true, "static": true, "final": true, "volatile": true, "transient": true}
	i := 0
	for i < len(segment) && modifiers[segment[i].text] {
		i++
	}
	if i >= len(segment) || !isIdentifier(segment[i].text) {
		return
	}
	typ := segment[i].text
	i++
	for i+1 < len(segment) && segment[i].text == "." && isIdentifier(segment[i+1].text) {
		typ = segment[i+1].text
		i += 2
	}
	if i < len(segment) && segment[i].text == "<" {
		depth := 1
		i++
		for i < len(segment) && depth > 0 {
			switch segment[i].text {
			case "<":
				depth++
			case ">":
				depth--
			case ">>":
				depth -= 2
			case ">>>":
				depth -= 3
			}
			i++
		}
	}
	alias := false
	for i+1 < len(segment) && segment[i].text == "[" && segment[i+1].text == "]" {
		alias = true
		i += 2
	}
	for i < len(segment) {
		if !isIdentifier(segment[i].text) {
			return
		}
		name := segment[i].text
		i++
		fields[name] = gradedSurfaceField{name: name, typeName: typ, public: public, mutable: mutable, alias: alias || gradedAliasType(segment)}
		depth := 0
		for i < len(segment) {
			t := segment[i].text
			if t == "(" || t == "{" || t == "[" {
				depth++
			}
			if t == ")" || t == "}" || t == "]" {
				depth--
			}
			if t == "," && depth == 0 {
				i++
				break
			}
			i++
		}
	}
}
