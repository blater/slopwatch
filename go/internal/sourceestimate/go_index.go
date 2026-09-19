package sourceestimate

import "unicode"

type goMethodIndex struct {
	all, exported        map[string][]*operation
	supporting           map[string]string
	supportingOperations map[string]string
}

func indexGoMethods(units []unit) goMethodIndex {
	index := newGoMethodIndex()
	for _, unit := range units {
		if normalizeLanguage(unit.file.Language, unit.file.Path) == "go" {
			indexGoUnit(index, unit)
		}
	}
	classifyGoFieldMethods(index, units)
	indexGoSupportingOperations(index, units)
	return index
}

func newGoMethodIndex() goMethodIndex {
	return goMethodIndex{
		all: map[string][]*operation{}, exported: map[string][]*operation{},
		supporting: map[string]string{}, supportingOperations: map[string]string{},
	}
}

func indexGoUnit(index goMethodIndex, unit unit) {
	for _, op := range unit.ops {
		if op.owner == "" {
			continue
		}
		key := goMethodGroupKey(op)
		index.all[key] = append(index.all[key], op)
		if op.exposed {
			index.exported[key] = append(index.exported[key], op)
		}
	}
}

func classifyGoFieldMethods(index goMethodIndex, units []unit) {
	fields, stringFields := goDeclaredFields(units), goStringFields(units)
	for key, methods := range index.all {
		if !singleExportedMethod(index, key, methods) {
			continue
		}
		op := methods[0]
		if !unexportedGoName(op.owner) || !fields[goTypeFieldKey(op, returnedField(op))] {
			continue
		}
		role := goFieldMethodRole(op, stringFields[goTypeFieldKey(op, returnedField(op))])
		if role != "" {
			index.supporting[key] = role
		}
	}
}

func singleExportedMethod(index goMethodIndex, key string, methods []*operation) bool {
	return len(methods) == 1 && len(index.exported[key]) == 1
}

func goFieldMethodRole(op *operation, stringField bool) string {
	if op.name == "Error" && op.returnType == "string" && op.params == 0 && pureStringFieldReturn(op) && stringField {
		return roleSupportingErrorRepresentation
	}
	if op.name != "Error" && op.params == 0 && pureStringFieldReturn(op) {
		return roleSupportingDataRepresentation
	}
	return ""
}

func indexGoSupportingOperations(index goMethodIndex, units []unit) {
	for _, unit := range units {
		if normalizeLanguage(unit.file.Language, unit.file.Path) != "go" {
			continue
		}
		for _, op := range unit.ops {
			if op.owner == "" {
				indexGoConstructorRole(index, op)
			}
		}
	}
}

func indexGoConstructorRole(index goMethodIndex, op *operation) {
	body := trimSemicolonTokens(op.body)
	if len(body) < 5 || body[0].text != "return" {
		return
	}
	start := constructorStart(body)
	if start < 0 || start+2 >= len(body) || body[start+1].text != "{" || body[len(body)-1].text != "}" {
		return
	}
	role := index.supporting[op.pkg+"#"+body[start].text]
	if role != "" && pureGoConstructorBody(body, start, op.paramNames) {
		index.supportingOperations[op.id] = role
	}
}

func constructorStart(body []token) int {
	if body[1].text == "&" {
		return 2
	}
	return 1
}

func pureGoConstructorBody(body []token, start int, paramNames []string) bool {
	params := map[string]bool{}
	for _, name := range paramNames {
		params[name] = true
	}
	for i := start + 2; i < len(body)-1; i++ {
		item := body[i]
		if item.text == "," || item.text == ":" || goFieldLabel(body, i) {
			continue
		}
		if params[item.text] || item.kind == "string" || item.kind == "number" || item.text == "nil" || item.text == "true" || item.text == "false" {
			continue
		}
		return false
	}
	return true
}

func goFieldLabel(body []token, index int) bool {
	return index+1 < len(body) && body[index+1].text == ":" && isIdentifier(body[index].text)
}

func unexportedGoName(name string) bool {
	return name != "" && unicode.IsLower(rune(name[0]))
}

func goTypeFieldKey(op *operation, field string) string {
	return op.pkg + "#" + op.owner + "." + field
}

func returnedField(op *operation) string {
	body := trimSemicolonTokens(op.body)
	if len(body) == 4 && body[0].text == "return" && body[1].text == op.receiverName && body[2].text == "." && isIdentifier(body[3].text) {
		return body[3].text
	}
	return ""
}

func pureStringFieldReturn(op *operation) bool { return returnedField(op) != "" }

func trimSemicolonTokens(body []token) []token {
	for len(body) > 0 && body[len(body)-1].text == ";" {
		body = body[:len(body)-1]
	}
	return body
}

func goMethodGroupKey(op *operation) string {
	return op.pkg + "#" + op.owner
}

func goMethodGroupKeyFor(unit unit, key string) string {
	if len(key) >= len(unit.pkg)+1 && key[:len(unit.pkg)+1] == unit.pkg+"#" {
		return key
	}
	return unit.pkg + "#" + key
}

func goSupportingRole(index goMethodIndex, op *operation) string {
	if role := index.supportingOperations[op.id]; role != "" {
		return role
	}
	return index.supporting[goMethodGroupKey(op)]
}
