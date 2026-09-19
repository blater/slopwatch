package sourceestimate

func goStringFields(units []unit) map[string]bool {
	fields := map[string]bool{}
	for _, unit := range units {
		if normalizeLanguage(unit.file.Language, unit.file.Path) != "go" {
			continue
		}
		collectGoStringFields(fields, unit)
	}
	return fields
}

func collectGoStringFields(fields map[string]bool, unit unit) {
	for i := 0; i+3 < len(unit.tokens); i++ {
		if !goStructStart(unit.tokens, i) {
			continue
		}
		end := matching(unit.tokens, i+3, "{", "}")
		if end < 0 {
			continue
		}
		owner := unit.tokens[i+1].text
		for j := i + 4; j+1 < end; j++ {
			if isIdentifier(unit.tokens[j].text) && unit.tokens[j+1].text == "string" {
				fields[unit.pkg+"#"+owner+"."+unit.tokens[j].text] = true
			}
		}
		i = end
	}
}

func goDeclaredFields(units []unit) map[string]bool {
	fields := map[string]bool{}
	for _, unit := range units {
		if normalizeLanguage(unit.file.Language, unit.file.Path) == "go" {
			collectGoDeclaredFields(fields, unit)
		}
	}
	return fields
}

func collectGoDeclaredFields(fields map[string]bool, unit unit) {
	for i := 0; i+3 < len(unit.tokens); i++ {
		if !goStructStart(unit.tokens, i) {
			continue
		}
		end := matching(unit.tokens, i+3, "{", "}")
		if end < 0 {
			continue
		}
		owner := unit.tokens[i+1].text
		for j := i + 4; j+1 < end; j++ {
			if isIdentifier(unit.tokens[j].text) && goFieldTypeToken(unit.tokens[j+1]) {
				fields[unit.pkg+"#"+owner+"."+unit.tokens[j].text] = true
			}
		}
		i = end
	}
}

func goStructStart(tokens []token, index int) bool {
	return tokens[index].text == "type" && isIdentifier(tokens[index+1].text) && tokens[index+2].text == "struct" && tokens[index+3].text == "{"
}

func goFieldTypeToken(item token) bool {
	return isIdentifier(item.text) || item.text == "*"
}
