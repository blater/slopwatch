package sourceestimate

func typeScriptModuleStorage(body []token) map[string]string {
	result := map[string]string{}
	shadow := map[string]bool{}
	depth := 0
	for i, t := range body {
		if t.text == "{" {
			depth++
		}
		if t.text == "}" {
			depth--
		}
		if depth != 0 {
			continue
		}
		if (t.text == "class" || t.text == "interface" || t.text == "type" || t.text == "function") && i+1 < len(body) {
			shadow[body[i+1].text] = true
		}
		if t.text == "import" {
			end := statementEnd(body, i)
			for _, v := range body[i:end] {
				if v.text == "Set" || v.text == "Map" {
					shadow[v.text] = true
				}
			}
		}
		if (t.text == "const" || t.text == "let" || t.text == "var") && i+1 < len(body) {
			shadow[body[i+1].text] = true
		}
	}
	depth = 0
	for i := 0; i < len(body); i++ {
		t := body[i].text
		if t == "{" {
			depth++
		}
		if t == "}" {
			depth--
		}
		if depth != 0 || (t != "let" && t != "var" && t != "const") || i+2 >= len(body) || !isIdentifier(body[i+1].text) {
			continue
		}
		end := statementEnd(body, i)
		assign := i + 2
		for assign < end && body[assign].text != "=" {
			assign++
		}
		if assign+1 >= end {
			continue
		}
		init := body[assign+1 : end]
		if len(init) == 1 && t != "const" && (init[0].kind == "number" || init[0].kind == "string" || init[0].text == "true" || init[0].text == "false") {
			result[body[i+1].text] = "scalar"
		}
		if len(init) >= 4 && init[0].text == "new" && (init[1].text == "Set" || init[1].text == "Map") && !shadow[init[1].text] {
			open := 2
			if init[open].text == "<" {
				close := matching(init, open, "<", ">")
				if close < 0 {
					continue
				}
				open = close + 1
			}
			if open+1 < len(init) && init[open].text == "(" && matching(init, open, "(", ")") == len(init)-1 {
				result[body[i+1].text] = init[1].text
			}
		}
	}
	return result
}
