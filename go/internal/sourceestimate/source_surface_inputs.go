package sourceestimate

import (
	"sort"
	"strings"
)

// annotateSourceSurfaceInputs adds a deliberately narrow finding for a
// required argument which is accepted by a callable source operation, passed
// by a same-workspace caller, and then never read by the operation. It runs on
// the already parsed operation inventory; callers must invoke it after the
// shared operation index is available.
//
// This is source evidence, not a type-system proof. Ambiguous calls, callback
// escapes, contract methods, optional parameters, generated/deprecated source,
// and incomplete declarations are left alone.
func annotateSourceSurfaceInputs(units []unit, byKey map[string][]*operation) {
	if len(units) == 0 || len(byKey) == 0 {
		return
	}
	callers := make(map[*operation][]surfaceCaller)
	contracts := make(map[*operation]bool)
	for i := range units {
		u := &units[i]
		lang := normalizeLanguage(u.file.Language, u.file.Path)
		if lang != "java" && lang != "typescript" && lang != "rust" {
			continue
		}
		for op, contract := range surfaceContractOperations(*u) {
			contracts[op] = contract
		}
	}
	for i := range units {
		u := &units[i]
		lang := normalizeLanguage(u.file.Language, u.file.Path)
		if lang != "java" && lang != "typescript" && lang != "rust" {
			continue
		}
		if u.limited || !u.lexicallyValid || sourceSurfaceExcluded(u.file.Source) {
			continue
		}
		for _, caller := range u.ops {
			if caller == nil || surfaceDynamicArgumentAccess(caller.body) {
				continue
			}
			calls := callsIn(caller.body)
			if len(calls) > maxCallsPerRoot {
				calls = calls[:maxCallsPerRoot]
			}
			for _, c := range calls {
				if unreachableCall(caller.body, c) {
					continue
				}
				candidate := surfaceCallTarget(caller, c, units, byKey)
				if candidate == nil || candidate.language != lang || candidate == caller {
					continue
				}
				if len(c.actuals) != len(candidate.paramNames) {
					continue
				}
				if !surfaceArgumentEvidence(caller, c.actuals) {
					continue
				}
				callers[candidate] = append(callers[candidate], surfaceCaller{file: u.file.Path, caller: caller, call: c})
			}
		}
	}

	for _, u := range units {
		lang := normalizeLanguage(u.file.Language, u.file.Path)
		if lang != "java" && lang != "typescript" && lang != "rust" {
			continue
		}
		if u.limited || !u.lexicallyValid || sourceSurfaceExcluded(u.file.Source) {
			continue
		}
		for _, op := range u.ops {
			if op == nil || !surfaceRootEligible(op, callers[op]) || contracts[op] || surfaceConstructor(op) {
				continue
			}
			if surfaceDynamicArgumentAccess(op.body) || surfaceStub(op.body) {
				continue
			}
			if len(op.requiredParamNames) == 0 {
				continue
			}
			used := make(map[string]bool, len(op.requiredParamNames))
			for _, name := range op.requiredParamNames {
				used[name] = surfaceBodyReads(op.body, name)
			}
			usedCount := 0
			for _, value := range used {
				if value {
					usedCount++
				}
			}
			// A one-input identity/predicate is not a redundant surface. For a
			// finding there must be another required input which the body reads.
			if usedCount == 0 {
				continue
			}
			for _, unusedName := range op.requiredParamNames {
				if used[unusedName] {
					continue
				}
				index := surfaceParameterIndex(op, unusedName)
				if index < 0 {
					continue
				}
				files := make([]string, 0, len(callers[op]))
				seenFiles := map[string]bool{}
				for _, witness := range callers[op] {
					if index >= len(witness.call.actuals) || !surfaceArgumentUsesCallerInput(witness.caller, witness.call.actuals[index]) {
						continue
					}
					if !seenFiles[witness.file] {
						seenFiles[witness.file] = true
						files = append(files, witness.file)
					}
				}
				if len(files) == 0 && !op.exposed {
					continue
				}
				sort.Strings(files)
				op.findings = append(op.findings, Finding{
					Kind:              "unused-input",
					Operation:         op.id,
					Parameter:         unusedName,
					CallerFiles:       files,
					UnnecessaryBurden: 0.25,
				})
			}
		}
	}
}

type surfaceCaller struct {
	file   string
	caller *operation
	call   call
}

func requiredParameterNames(parameters []token, language string) []string {
	if len(parameters) == 0 {
		return nil
	}
	result := make([]string, 0, parameterCount(parameters))
	for _, part := range splitParameterDeclarations(parameters) {
		if len(part) == 0 || surfaceOptionalParameter(part, language) {
			continue
		}
		names := parameterNames(part, language)
		if len(names) == 0 {
			continue
		}
		result = append(result, names[0])
	}
	return result
}

func surfaceOptionalParameter(part []token, language string) bool {
	for _, item := range part {
		switch item.text {
		case "...", "?", "=", "..":
			return true
		}
	}
	// TypeScript parameter properties and destructured/default bindings are
	// intentionally outside the bounded name model.
	if language == "typescript" {
		for _, item := range part {
			if item.text == "{" || item.text == "[" {
				return true
			}
		}
	}
	return false
}

func sourceSurfaceExcluded(source []byte) bool {
	text := strings.ToLower(string(source))
	if strings.Contains(text, "code generated") && strings.Contains(text, "do not edit") {
		return true
	}
	// Comments are intentionally removed by the bounded lexer, so inspect raw
	// source here. Suppressing a whole affected unit avoids claiming a precise
	// finding for a declaration whose lifecycle contract is unclear.
	return strings.Contains(text, "@deprecated")
}

func surfaceRootEligible(op *operation, observed []surfaceCaller) bool {
	if op.exposed || (op.language == "java" && op.packageVisible) {
		return true
	}
	// TypeScript and Rust attribution promote cross-file internal operations
	// only when the shared source set demonstrates an external inbound edge.
	for _, caller := range observed {
		if caller.caller != nil && caller.caller.file != op.file {
			return true
		}
	}
	return false
}

func surfaceConstructor(op *operation) bool {
	return op.owner != "" && op.name == op.owner
}

func surfaceBodyReads(body []token, name string) bool {
	for _, item := range body {
		if item.text == name {
			return true
		}
	}
	return false
}

func surfaceDynamicArgumentAccess(body []token) bool {
	for _, item := range body {
		if item.text == "arguments" || item.text == "eval" {
			return true
		}
		if item.text == "..." || item.text == ".." {
			return true
		}
	}
	return false
}

func surfaceStub(body []token) bool {
	body = trimSemicolonTokens(body)
	if len(body) == 0 {
		return true
	}
	for _, item := range body {
		if item.text == "UnsupportedOperationException" || item.text == "NotImplementedException" || item.text == "unimplemented" || item.text == "todo" {
			return true
		}
	}
	return len(body) == 2 && body[0].text == "return" && (body[1].kind == "number" || body[1].kind == "string" || body[1].text == "null" || body[1].text == "None")
}

func surfaceParameterIndex(op *operation, name string) int {
	for index, candidate := range op.paramNames {
		if candidate == name {
			return index
		}
	}
	return -1
}

func surfaceArgumentEvidence(caller *operation, args [][]token) bool {
	for _, arg := range args {
		if surfaceArgumentUsesCallerInput(caller, arg) {
			return true
		}
	}
	return false
}

func surfaceArgumentUsesCallerInput(caller *operation, arg []token) bool {
	if len(arg) == 0 || caller == nil {
		return false
	}
	allowed := map[string]bool{}
	for _, name := range caller.paramNames {
		allowed[name] = true
	}
	usesInput := false
	for i, item := range arg {
		if item.kind == "identifier" {
			if item.text == "new" || item.text == "true" || item.text == "false" || item.text == "null" {
				continue
			}
			if !allowed[item.text] {
				return false
			}
			usesInput = true
		}
		if item.text == "(" && i > 0 {
			return false
		}
	}
	return usesInput
}

func surfaceCallTarget(caller *operation, c call, units []unit, byKey map[string][]*operation) *operation {
	candidates := resolveCall(caller, c, units, byKey)
	if len(candidates) == 1 {
		return candidates[0]
	}
	return nil
}

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

// Signature commas inside generic type arguments do not separate parameters.
func splitParameterDeclarations(tokens []token) [][]token {
	result := [][]token{}
	start, nested, generic := 0, 0, 0
	for i, item := range tokens {
		switch item.text {
		case "(", "[", "{":
			nested++
		case ")", "]", "}":
			if nested > 0 {
				nested--
			}
		case "<":
			generic++
		case ">":
			if generic > 0 {
				generic--
			}
		case ">>":
			if generic >= 2 {
				generic -= 2
			} else {
				generic = 0
			}
		case ",":
			if nested == 0 && generic == 0 {
				result = append(result, tokens[start:i])
				start = i + 1
			}
		}
	}
	if start < len(tokens) {
		result = append(result, tokens[start:])
	}
	return result
}
