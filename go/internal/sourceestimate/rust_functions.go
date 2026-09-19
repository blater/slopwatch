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
	context := newRustParseContext(tokens)
	impls := rustImplRangesWithContext(tokens, context)
	types := rustNamedRangesWithContext(tokens, "struct", context)
	firstType := map[string]bool{}
	for _, declaration := range types {
		if _, exists := firstType[declaration.name]; !exists {
			firstType[declaration.name] = declaration.public
		}
	}
	traits := map[string]bool{}
	for _, declaration := range rustNamedRangesWithContext(tokens, "trait", context) {
		traits[declaration.name] = traits[declaration.name] || declaration.public
	}
	implIndex := 0
	for i := 0; i < len(tokens); i++ {
		if tokens[i].text != "fn" || i+1 >= len(tokens) || !isIdentifier(tokens[i+1].text) {
			continue
		}
		j := i + 2
		if j < len(tokens) && tokens[j].text == "<" {
			end := context.angleEnds[j]
			if end < 0 {
				continue
			}
			j = end + 1
		}
		if j >= len(tokens) || tokens[j].text != "(" {
			continue
		}
		close := context.parenEnds[j]
		if close < 0 {
			continue
		}
		bodyStart := context.nextFunctionBoundary[close+1]
		if bodyStart >= len(tokens) || tokens[bodyStart].text != "{" {
			continue
		}
		bodyEnd := context.braceEnds[bodyStart]
		if bodyEnd < 0 {
			continue
		}
		info := rustFunction{start: i, paramOpen: j, paramClose: close, bodyStart: bodyStart, bodyEnd: bodyEnd, name: tokens[i+1].text,
			pubFn: context.barePub[i], restricted: context.restricted[i], testOnly: context.testOnly[i], modulePub: context.moduleVisible[i]}
		for implIndex < len(impls) && impls[implIndex].end <= i {
			implIndex++
		}
		if implIndex < len(impls) && i > impls[implIndex].start && i < impls[implIndex].end {
			info.impl = &impls[implIndex]
			info.owner = impls[implIndex].selfType
		}
		info.selfPub = firstType[info.owner]
		if info.impl != nil && info.impl.trait != "" {
			info.traitPub = traits[info.impl.trait]
		}
		result = append(result, info)
		i = bodyEnd
	}
	return result
}

func rustImplRanges(tokens []token) []rustImplRange {
	return rustImplRangesWithContext(tokens, newRustParseContext(tokens))
}
func rustImplRangesWithContext(tokens []token, context rustParseContext) []rustImplRange {
	result := make([]rustImplRange, 0, 4)
	for i := 0; i < len(tokens); i++ {
		if tokens[i].text != "impl" {
			continue
		}
		if context.nextMatchedBrace[i+1] == len(tokens) {
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
		bodyEnd := context.braceEnds[bodyStart]
		if bodyEnd < 0 {
			continue
		}
		header := tokens[i+1 : bodyStart]
		traitName, selfType := rustImplIdentity(header)
		result = append(result, rustImplRange{start: i, end: bodyEnd, trait: traitName, selfType: selfType, modulePub: context.moduleVisible[i]})
		i = bodyEnd
	}
	return result
}

func rustNamedRanges(tokens []token, keyword string) []rustRange {
	return rustNamedRangesWithContext(tokens, keyword, newRustParseContext(tokens))
}
func rustNamedRangesWithContext(tokens []token, keyword string, context rustParseContext) []rustRange {
	result := make([]rustRange, 0, 4)
	for i := 0; i+1 < len(tokens); i++ {
		if tokens[i].text != keyword || !isIdentifier(tokens[i+1].text) {
			continue
		}
		name := tokens[i+1].text
		public := context.barePub[i] && context.moduleVisible[i]
		end := context.nextBoundary[i+2]
		if end < len(tokens) && tokens[end].text == "{" {
			if close := context.braceEnds[end]; close >= 0 {
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
