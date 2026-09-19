package sourceestimate

import "sort"

// AnalyzeTypeScriptFiles returns bounded per-file projections and passive
// structural roles using the shared parser/index supplied by the caller.
func AnalyzeTypeScriptFiles(files []File) map[string]Result {
	units := make([]unit, 0, len(files))
	for index, file := range files {
		if normalizeLanguage(file.Language, file.Path) != "typescript" {
			continue
		}
		tokens, limited, valid := lex(file.Source)
		pkg := packageName(file.Language, tokens)
		item := unit{index: index, file: file, tokens: tokens, pkg: pkg, limited: limited, lexicallyValid: valid}
		item.ops = findOperations(file, index, tokens, pkg)
		units = append(units, item)
	}
	all := make([]*operation, 0)
	for _, unit := range units {
		all = append(all, unit.ops...)
	}
	annotateTypeScriptImports(units)
	byKey := make(map[string][]*operation, len(all))
	for _, op := range all {
		indexOperation(byKey, op)
	}
	annotateSourceSurfaceInputs(units, byKey)
	return analyzeTypeScriptUnits(units, byKey)
}

func analyzeTypeScriptUnits(units []unit, byKey map[string][]*operation) map[string]Result {
	roles := supportingTypeScriptOwners(units)
	rolesByFile := map[int]map[string]string{}
	for key, role := range roles {
		if rolesByFile[key.file] == nil {
			rolesByFile[key.file] = map[string]string{}
		}
		rolesByFile[key.file][key.owner] = role
	}
	inbound := buildGoInboundIndex(units, boundedCallGraph(units, byKey, "typescript"))
	results := make(map[string]Result)
	for _, u := range units {
		if normalizeLanguage(u.file.Language, u.file.Path) != "typescript" {
			continue
		}
		groups := map[string][]*operation{}
		audiences := map[string]string{}
		supporting := rolesByFile[u.index]
		if supporting == nil {
			supporting = map[string]string{}
		}
		moduleGroups := typeScriptModuleGroups(u, units, byKey)
		behavior := false
		for _, op := range u.ops {
			if role := roles[attributionOwner(op)]; role != "" {
				supporting[op.owner] = role
				continue
			}
			behavior = true
			if !op.exposed && inbound.sameFileInbound[op.id] > 0 && inbound.externalInbound[op.id] == 0 {
				continue
			}
			name := op.name
			if op.owner != "" {
				name = op.owner
			} else if group := moduleGroups[op.id]; group != "" {
				name = group
			}
			audience := "local-unresolved"
			if op.exposed {
				audience = "external"
			} else if inbound.externalInbound[op.id] > 0 {
				audience = "module"
			}
			key := audience + ":" + name
			groups[key] = append(groups[key], op)
			audiences[key] = audience
		}
		keys := make([]string, 0, len(groups))
		for key := range groups {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		var best Result
		have := false
		details := []Abstraction{}
		for _, key := range keys {
			projection := estimateRoots(u.index, u, units, byKey, groups[key])
			details = append(details, Abstraction{Grade: projection.Grade, Name: key, Audience: audiences[key], Burden: projection.Burden, Hidden: projection.Hidden, Findings: projection.Findings, RecognizedHidden: projection.RecognizedHidden(), UncertainBurden: projection.Assessment().UncertainBurden, Limitations: append([]string(nil), projection.Limitations...)})
			if !have || largerProjection(projection, best) {
				best, have = projection, true
			}
		}
		if !have {
			best = estimateRoots(u.index, u, units, byKey, nil)
		}
		best.Supporting = supporting
		best.Abstractions = details
		for _, role := range supporting {
			best.Roles = appendUniqueString(best.Roles, role)
		}
		sort.Strings(best.Roles)
		if len(supporting) > 0 && !behavior && typeScriptRoleOnlyFile(u, supporting) {
			best.RoleOnly = true
			best.Applicable = false
			best.Burden, best.Hidden = 0, 0
		}
		results[u.file.Path] = best
	}
	return results
}

// supportingTypeScriptOwners proves data-only classes from bounded source,
// including local classes. Every member must be admitted; an empty constructor
// cannot hide executable parameter defaults, initializers or static blocks.
func supportingTypeScriptOwners(units []unit) map[attributionOwnerKey]string {
	result := map[attributionOwnerKey]string{}
	for _, u := range units {
		if normalizeLanguage(u.file.Language, u.file.Path) != "typescript" || u.limited || !u.lexicallyValid {
			continue
		}
		for i := 0; i+2 < len(u.tokens); i++ {
			if u.tokens[i].text != "class" || !isIdentifier(u.tokens[i+1].text) {
				continue
			}
			start := i + 2
			if u.tokens[start].text == "<" {
				end := matching(u.tokens, start, "<", ">")
				if end < 0 {
					continue
				}
				start = end + 1
			}
			if start >= len(u.tokens) || u.tokens[start].text != "{" {
				continue
			}
			end := matching(u.tokens, start, "{", "}")
			if end < 0 {
				continue
			}
			if passiveTypeScriptMembers(u.tokens[start+1 : end]) {
				// TypeScript classes are file-local in the bounded resolver. Scope
				// the role key by language and path so same-named classes cannot
				// borrow a passive proof from an unrelated source file.
				result[typescriptOwnerKey(u, u.tokens[i+1].text)] = "passive-value-object-v1"
			}
			i = end
		}
	}
	return result
}

func typescriptOwnerKey(u unit, owner string) attributionOwnerKey {
	return attributionOwnerKey{file: u.index, owner: owner}
}

func passiveTypeScriptMembers(body []token) bool {
	fields := map[string]bool{}
	methods := [][]token{}
	for i := 0; i < len(body); {
		if body[i].text == ";" {
			i++
			continue
		}
		start := i
		for i < len(body) && (body[i].text == "public" || body[i].text == "private" || body[i].text == "protected" || body[i].text == "readonly") {
			i++
		}
		if i >= len(body) || !isIdentifier(body[i].text) || body[i].text == "static" {
			return false
		}
		name := body[i].text
		i++
		if name == "get" && i < len(body) && isIdentifier(body[i].text) {
			i++
		}
		if i < len(body) && body[i].text == "(" {
			paramsEnd := matching(body, i, "(", ")")
			if paramsEnd < 0 {
				return false
			}
			for _, param := range body[i+1 : paramsEnd] {
				if param.text == "=" || param.text == "..." {
					return false
				}
			}
			i = paramsEnd + 1
			for i < len(body) && body[i].text != "{" {
				if body[i].text == ";" || body[i].text == "=" {
					return false
				}
				i++
			}
			if i >= len(body) {
				return false
			}
			end := matching(body, i, "{", "}")
			if end < 0 {
				return false
			}
			methods = append(methods, body[start:end+1])
			i = end + 1
			continue
		}
		fields[name] = true
		for i < len(body) && body[i].text != ";" {
			if body[i].text == "=" || body[i].text == "(" || body[i].text == "{" || body[i].text == "}" {
				return false
			}
			i++
		}
		if i < len(body) {
			i++
		}
	}
	if len(fields) == 0 {
		return false
	}
	for _, method := range methods {
		open := -1
		for i, t := range method {
			if t.text == "(" {
				open = i
				break
			}
		}
		if open < 1 {
			return false
		}
		close := matching(method, open, "(", ")")
		start := close + 1
		for start < len(method) && method[start].text != "{" {
			start++
		}
		if start >= len(method) {
			return false
		}
		content := trimSemicolonTokens(method[start+1 : len(method)-1])
		if method[open-1].text == "constructor" {
			params := map[string]bool{}
			for _, name := range parameterNames(method[open+1:close], "typescript") {
				params[name] = true
			}
			for len(content) > 0 {
				if len(content) < 5 || content[0].text != "this" || content[1].text != "." || !fields[content[2].text] || content[3].text != "=" || !params[content[4].text] {
					return false
				}
				content = content[5:]
				if len(content) > 0 {
					if content[0].text != ";" {
						return false
					}
					content = content[1:]
				}
			}
		} else {
			if close != open+1 || len(content) != 4 || content[0].text != "return" || content[1].text != "this" || content[2].text != "." || !fields[content[3].text] {
				return false
			}
		}
	}
	return true
}
