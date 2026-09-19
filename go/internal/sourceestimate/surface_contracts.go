package sourceestimate

func surfaceContractOperations(u unit) map[*operation]bool {
	result := make(map[*operation]bool)
	lang := normalizeLanguage(u.file.Language, u.file.Path)
	if lang == "rust" {
		functions := rustFunctions(u.tokens)
		for index, candidate := range u.ops {
			if index >= len(functions) {
				continue
			}
			result[candidate] = functions[index].impl != nil && functions[index].impl.trait != ""
		}
		escapes := surfaceFunctionEscapes(u)
		for _, candidate := range u.ops {
			result[candidate] = result[candidate] || escapes[candidate.name]
		}
		return result
	}
	contractOwners := map[string]bool{}
	for i := 0; i+1 < len(u.tokens); i++ {
		if u.tokens[i].text != "class" && u.tokens[i].text != "interface" {
			continue
		}
		if !isIdentifier(u.tokens[i+1].text) {
			continue
		}
		owner := u.tokens[i+1].text
		open := i + 2
		for open < len(u.tokens) && u.tokens[open].text != "{" {
			open++
		}
		if open >= len(u.tokens) {
			continue
		}
		contract := u.tokens[i].text == "interface"
		for _, item := range u.tokens[i+2 : open] {
			contract = contract || item.text == "extends" || item.text == "implements"
		}
		contractOwners[owner] = contract
	}
	overrideNames := map[string]bool{}
	for i := 0; i < len(u.tokens); i++ {
		if u.tokens[i].text != "Override" && u.tokens[i].text != "override" {
			continue
		}
		for j := i + 1; j < len(u.tokens) && j-i <= 16; j++ {
			if u.tokens[j].text == "{" || u.tokens[j].text == ";" {
				break
			}
			if isIdentifier(u.tokens[j].text) && j+1 < len(u.tokens) && u.tokens[j+1].text == "(" {
				overrideNames[u.tokens[j].text] = true
				break
			}
		}
	}
	callbackNames := map[string]bool{}
	callbackEscapes := map[string]bool{}
	if lang == "typescript" {
		for i := 0; i+2 < len(u.tokens); i++ {
			if u.tokens[i].text != "const" || !isIdentifier(u.tokens[i+1].text) {
				continue
			}
			for j := i + 2; j < len(u.tokens) && j-i <= 10; j++ {
				if u.tokens[j].text == ":" {
					callbackNames[u.tokens[i+1].text] = true
				}
				if u.tokens[j].text == "=" || u.tokens[j].text == ";" {
					break
				}
			}
		}
		declared := map[string]bool{}
		for _, op := range u.ops {
			declared[op.name] = true
		}
		for i, item := range u.tokens {
			if !declared[item.text] || i+1 >= len(u.tokens) {
				continue
			}
			if u.tokens[i+1].text == "(" || u.tokens[i+1].text == "=" || (i > 0 && u.tokens[i-1].text == "const") {
				continue
			}
			callbackEscapes[item.text] = true
		}
	}
	for _, op := range u.ops {
		result[op] = contractOwners[op.owner] || overrideNames[op.name] || callbackNames[op.name] || callbackEscapes[op.name]
	}
	return result
}

func surfaceFunctionEscapes(u unit) map[string]bool {
	declared, escaped := map[string]bool{}, map[string]bool{}
	for _, op := range u.ops {
		declared[op.name] = true
	}
	for i, item := range u.tokens {
		if !declared[item.text] {
			continue
		}
		if i > 0 && (u.tokens[i-1].text == "fn" || u.tokens[i-1].text == "function") {
			continue
		}
		if i+1 < len(u.tokens) && u.tokens[i+1].text == "(" {
			continue
		}
		escaped[item.text] = true
	}
	return escaped
}
