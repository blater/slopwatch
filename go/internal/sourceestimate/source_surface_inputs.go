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
			observeSurfaceCalls(caller, lang, u.file.Path, units, byKey, callers)
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
			annotateSurfaceOperation(op, callers, contracts)
		}
	}
}

type surfaceCaller struct {
	file   string
	caller *operation
	call   call
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

func annotateSurfaceOperation(op *operation, callers map[*operation][]surfaceCaller, contracts map[*operation]bool) {
	if op == nil || !surfaceRootEligible(op, callers[op]) || contracts[op] || surfaceConstructor(op) {
		return
	}
	if surfaceDynamicArgumentAccess(op.body) || surfaceStub(op.body) {
		return
	}
	if len(op.requiredParamNames) == 0 {
		return
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
		return
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

func observeSurfaceCalls(caller *operation, lang, path string, units []unit, byKey map[string][]*operation, callers map[*operation][]surfaceCaller) {
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
		callers[candidate] = append(callers[candidate], surfaceCaller{file: path, caller: caller, call: c})
	}
}
