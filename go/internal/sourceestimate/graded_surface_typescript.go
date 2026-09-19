package sourceestimate

func gradedTypeScriptConstructorFields(fields map[string]gradedSurfaceField, tokens []token, start, methodEnd int) {
	paren := -1
	for i := start; i < methodEnd; i++ {
		if tokens[i].text == "(" {
			paren = i
			break
		}
	}
	if paren < 0 {
		return
	}
	end := matching(tokens, paren, "(", ")")
	if end < 0 || end > methodEnd {
		return
	}
	for i := paren + 1; i < end; {
		segmentEnd := i
		depth := 0
		for segmentEnd < end {
			switch tokens[segmentEnd].text {
			case "(", "[", "{":
				depth++
			case ")", "]", "}":
				if depth > 0 {
					depth--
				}
			case ",":
				if depth == 0 {
					goto parameterEnd
				}
			}
			segmentEnd++
		}
	parameterEnd:
		segment := tokens[i:segmentEnd]
		public, mutable, property := false, true, false
		for _, item := range segment {
			switch item.text {
			case "public", "export":
				public, property = true, true
			case "private", "protected":
				property = true
			case "readonly":
				mutable, property = false, true
			}
		}
		if property {
			for j, item := range segment {
				if !isIdentifier(item.text) || item.text == "public" || item.text == "private" || item.text == "protected" || item.text == "readonly" {
					continue
				}
				if j+1 < len(segment) && (segment[j+1].text == ":" || segment[j+1].text == "=") {
					fields[item.text] = gradedSurfaceField{name: item.text, typeName: gradedFieldTypeAfter(segment, j+1), public: public, mutable: mutable, alias: gradedAliasType(segment[j+1:])}
					break
				}
			}
		}
		i = segmentEnd + 1
	}
}
