package sourceestimate

func rustOperations(file File, index int, tokens []token, pkg string, functions []rustFunction) []*operation {
	fields := sourceFieldTypes(tokens)
	result := make([]*operation, 0, len(functions))
	for number, function := range functions {
		if function.paramOpen < 0 || function.paramClose < function.paramOpen || function.bodyEnd <= function.bodyStart {
			continue
		}
		parameters := tokens[function.paramOpen+1 : function.paramClose]
		returnType := rustReturnType(tokens[function.paramClose+1 : function.bodyStart])
		result = append(result, &operation{
			id: file.Path + "#" + function.name + "/" + itoa(number), name: function.name,
			owner: function.owner, language: "rust", pkg: pkg, file: index,
			params: rustParameterCount(parameters), paramNames: parameterNames(parameters, "rust"),
			requiredParamNames: requiredParameterNames(parameters, "rust"),
			stringParams:       stringParameterNames(parameters, "rust"), fieldTypes: fields,
			exposed: function.pubFn, packageVisible: function.pubFn, testOnly: function.testOnly, parameterTypes: gradedParameterTypes(parameters, "rust"),
			returnType: returnType,
			body:       tokens[function.bodyStart+1 : function.bodyEnd],
		})
	}
	return result
}

// rustReturnType records the declared result type for command/query
// sequencing. The bounded parser keeps only the signature span, stopping at
// a where-clause so generic constraints do not become part of the type name.
func rustReturnType(signature []token) string {
	for i, item := range signature {
		if item.text != "->" || i+1 >= len(signature) {
			continue
		}
		end := len(signature)
		for j := i + 1; j < len(signature); j++ {
			if signature[j].text == "where" {
				end = j
				break
			}
		}
		if end <= i+1 {
			return ""
		}
		return joinTokens(signature[i+1 : end])
	}
	return ""
}

// rustParameterCount measures caller supplied inputs. A method receiver is
// an implementation context, not an additional input at the public boundary.
func rustParameterCount(tokens []token) int {
	count := 0
	for _, part := range splitParameterDeclarations(tokens) {
		receiver := false
		for _, item := range part {
			if item.text == "self" {
				receiver = true
				break
			}
		}
		if !receiver {
			count++
		}
	}
	return count
}

func rustFunctions(tokens []token) []rustFunction {
	result := make([]rustFunction, 0, 8)
	impls := rustImplRanges(tokens)
	types := rustNamedRanges(tokens, "struct")
	for i := 0; i < len(tokens); i++ {
		if tokens[i].text != "fn" || i+1 >= len(tokens) || !isIdentifier(tokens[i+1].text) {
			continue
		}
		j := i + 2
		if j < len(tokens) && tokens[j].text == "<" {
			end := matching(tokens, j, "<", ">")
			if end < 0 {
				continue
			}
			j = end + 1
		}
		if j >= len(tokens) || tokens[j].text != "(" {
			continue
		}
		close := matching(tokens, j, "(", ")")
		if close < 0 {
			continue
		}
		bodyStart := close + 1
		for bodyStart < len(tokens) && tokens[bodyStart].text != "{" && tokens[bodyStart].text != "=>" && tokens[bodyStart].text != ";" {
			bodyStart++
		}
		if bodyStart >= len(tokens) || tokens[bodyStart].text != "{" {
			continue
		}
		bodyEnd := matching(tokens, bodyStart, "{", "}")
		if bodyEnd < 0 {
			continue
		}
		info := rustFunction{start: i, paramOpen: j, paramClose: close, bodyStart: bodyStart, bodyEnd: bodyEnd, name: tokens[i+1].text,
			pubFn: rustBarePubBefore(tokens, i), restricted: rustRestrictedBefore(tokens, i), testOnly: rustTestOnly(tokens, i), modulePub: rustModuleVisible(tokens, i)}
		for index := range impls {
			if i > impls[index].start && i < impls[index].end {
				info.impl = &impls[index]
				info.owner = impls[index].selfType
				break
			}
		}
		for _, declaration := range types {
			if info.owner == declaration.name {
				info.selfPub = declaration.public
				break
			}
		}
		if info.impl != nil {
			info.traitPub = rustTraitPublic(tokens, info.impl.trait)
		}
		result = append(result, info)
		i = bodyEnd
	}
	return result
}

func rustImplRanges(tokens []token) []rustImplRange {
	result := make([]rustImplRange, 0, 4)
	for i := 0; i < len(tokens); i++ {
		if tokens[i].text != "impl" {
			continue
		}
		bodyStart := i + 1
		angle, paren, bracket := 0, 0, 0
		for bodyStart < len(tokens) {
			text := tokens[bodyStart].text
			if text == "{" && angle == 0 && paren == 0 && bracket == 0 {
				break
			}
			switch text {
			case "<":
				angle++
			case ">":
				if angle > 0 {
					angle--
				}
			case ">>":
				angle = maxInt(0, angle-2)
			case "(":
				paren++
			case ")":
				paren--
			case "[":
				bracket++
			case "]":
				bracket--
			}
			bodyStart++
		}
		if bodyStart >= len(tokens) {
			continue
		}
		bodyEnd := matching(tokens, bodyStart, "{", "}")
		if bodyEnd < 0 {
			continue
		}
		header := tokens[i+1 : bodyStart]
		traitName, selfType := rustImplIdentity(header)
		result = append(result, rustImplRange{start: i, end: bodyEnd, trait: traitName, selfType: selfType, modulePub: rustModuleVisible(tokens, i)})
		i = bodyEnd
	}
	return result
}

func rustNamedRanges(tokens []token, keyword string) []rustRange {
	result := make([]rustRange, 0, 4)
	for i := 0; i+1 < len(tokens); i++ {
		if tokens[i].text != keyword || !isIdentifier(tokens[i+1].text) {
			continue
		}
		name := tokens[i+1].text
		public := rustBarePubBefore(tokens, i) && rustModuleVisible(tokens, i)
		end := i + 2
		for end < len(tokens) && tokens[end].text != "{" && tokens[end].text != ";" {
			end++
		}
		if end < len(tokens) && tokens[end].text == "{" {
			if close := matching(tokens, end, "{", "}"); close >= 0 {
				result = append(result, rustRange{start: i, end: close, name: name, public: public})
				i = close
			}
		} else {
			result = append(result, rustRange{start: i, end: end, name: name, public: public})
		}
	}
	return result
}

// Impl generic binders introduce names; they are not the implemented type.
// Split only top-level `for` before `where`, ignoring nested bounds/HRTBs.
