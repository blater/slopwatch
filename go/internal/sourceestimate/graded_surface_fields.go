package sourceestimate

import "unicode"

func gradedSurfaceFields(tokens []token, language string, open, close int) map[string]gradedSurfaceField {
	fields := map[string]gradedSurfaceField{}
	if language == "go" {
		// Go's semicolons are inserted by the compiler and are not retained by
		// name followed by a type token, as the existing Go field inventory does.
		// A declaration may contain several names (`Value,Checksum int`), so
		// parse one semicolon-delimited field declaration at a time.
		for i := open + 1; i < close; {
			end := i
			for end < close && tokens[end].text != ";" && (tokens[end].line == 0 || tokens[end].line == tokens[i].line) {
				end++
			}
			segment := tokens[i:end]
			// Names precede the entire type, including slice/map prefixes.
			names := []string{}
			typeIndex := 0
			for typeIndex < len(segment) && isIdentifier(segment[typeIndex].text) {
				names = append(names, segment[typeIndex].text)
				typeIndex++
				if typeIndex >= len(segment) || segment[typeIndex].text != "," {
					break
				}
				typeIndex++
			}
			if typeIndex < len(segment) {
				for _, name := range names {
					fields[name] = gradedSurfaceField{name: name, typeName: joinTokens(segment[typeIndex:]), public: unicode.IsUpper(rune(name[0])), mutable: true, alias: gradedAliasType(segment[typeIndex:])}
				}
			}

			i = end
			if i < close && tokens[i].text == ";" {
				i++
			}
		}
		return fields
	}
	if language == "rust" {
		for i := open + 1; i < close; {
			end := i
			for end < close && tokens[end].text != "," {
				end++
			}
			segment := tokens[i:end]
			for j := 0; j+1 < len(segment); j++ {
				if !isIdentifier(segment[j].text) || segment[j+1].text != ":" {
					continue
				}
				public := j > 0 && segment[j-1].text == "pub"
				typeName := ""
				if j+2 < len(segment) && isIdentifier(segment[j+2].text) {
					typeName = segment[j+2].text
				}
				fields[segment[j].text] = gradedSurfaceField{name: segment[j].text, typeName: typeName, public: public, mutable: true, alias: gradedAliasType(segment[j+2:])}
				break
			}
			i = end + 1
		}
		return fields
	}
	// Java and TypeScript class members have explicit semicolon or method
	// braces. The parser only admits top-level members within the owner.
	for i := open + 1; i < close; {
		if methodEnd := gradedMemberMethodEnd(tokens, i, close); methodEnd > i {
			if language == "typescript" && tokens[i].text == "constructor" {
				gradedTypeScriptConstructorFields(fields, tokens, i, methodEnd)
			}
			i = methodEnd
			continue
		}
		end := i
		depth := 0
		for end < close {
			switch tokens[end].text {
			case "{":
				depth++
			case "}":
				if depth > 0 {
					depth--
				}
			case ";":
				if depth == 0 {
					end++
					goto memberEnd
				}
			}
			end++
		}
	memberEnd:
		segment := tokens[i:end]
		public, mutable, explicitVisibility := false, true, false
		for _, item := range segment {
			switch item.text {
			case "public", "protected", "export":
				explicitVisibility = true
				public = item.text == "public" || item.text == "export"
			case "private", "readonly", "final", "const", "static":
				if item.text == "private" {
					explicitVisibility = true
				}
				if item.text == "readonly" || item.text == "final" || item.text == "const" {
					mutable = false
				}
			}
		}
		if language == "typescript" && !explicitVisibility {
			public = true
		}
		if language == "java" {
			gradedJavaFields(fields, segment, public, mutable)
			if !explicitVisibility {
				for name, f := range fields {
					if f.name == name && gradedSegmentContains(segment, name) {
						f.packageVisible = true
						fields[name] = f
					}
				}
			}
		}
		if language != "typescript" {
			i = end
			if i == open+1 {
				i++
			}
			continue
		}
		for j := 0; j < len(segment); j++ {
			if !isIdentifier(segment[j].text) {
				continue
			}
			if language == "typescript" && j+1 < len(segment) && (segment[j+1].text == ":" || segment[j+1].text == "=") {
				fields[segment[j].text] = gradedSurfaceField{name: segment[j].text, typeName: gradedFieldTypeAfter(segment, j+1), public: public, mutable: mutable, alias: gradedAliasType(segment[j+2:])}
			}
		}
		i = end
		if i == open+1 {
			i++
		}
	}
	return fields
}
