package sourceestimate

import "strings"

func gradedRustRAIICleanup(op *operation, u unit, body []token, units []unit, byKey *operationLookup, acquisition bool) bool {
	if op.language != "rust" {
		return false
	}
	fields := callerDeclaredFields(u, op.owner)
	for i := 0; i+5 < len(body); i++ {
		guard, ok := gradedRustGuardAt(body, i, fields)
		if ok && gradedRustGuardDrop(op, u, body, units, byKey, acquisition, guard) {
			return true
		}
	}
	return false
}

type gradedRustGuard struct {
	index, close     int
	name, typ, field string
}

func gradedRustGuardAt(body []token, index int, fields map[string]gradedSurfaceField) (gradedRustGuard, bool) {
	if body[index].text != "let" {
		return gradedRustGuard{}, false
	}
	n := index + 1
	if body[n].text == "mut" {
		n++
	}
	if n+3 >= len(body) || body[n+1].text != "=" || body[n+3].text != "(" {
		return gradedRustGuard{}, false
	}
	close := matching(body, n+3, "(", ")")
	if close < 0 {
		return gradedRustGuard{}, false
	}
	arg := body[n+4 : close]
	if len(arg) > 0 && arg[0].text == "&" {
		arg = arg[1:]
	}
	if len(arg) > 0 && arg[0].text == "mut" {
		arg = arg[1:]
	}
	if len(arg) != 3 || arg[0].text != "self" || arg[1].text != "." {
		return gradedRustGuard{}, false
	}
	field, ok := fields[arg[2].text]
	if !ok || field.typeName == "" || gradedRustGuardEscapes(body, close, body[n].text) {
		return gradedRustGuard{}, false
	}
	return gradedRustGuard{index: index, close: close, name: body[n].text, typ: body[n+2].text, field: arg[2].text}, true
}

func gradedRustGuardEscapes(body []token, close int, guard string) bool {
	for j := close + 1; j < len(body); j++ {
		if body[j].text != guard {
			continue
		}
		projected := j+1 < len(body) && body[j+1].text == "."
		dropped := j >= 2 && j+1 < len(body) && body[j-2].text == "drop" && body[j-1].text == "(" && body[j+1].text == ")"
		if !projected && !dropped {
			return true
		}
	}
	for _, c := range callsIn(body[close+1:]) {
		for _, actual := range c.actuals {
			if len(actual) == 1 && actual[0].text == guard && c.name != "drop" {
				return true
			}
		}
	}
	return false
}

func gradedRustGuardDrop(op *operation, u unit, body []token, units []unit, byKey *operationLookup, acquisition bool, guard gradedRustGuard) bool {
	fields := callerDeclaredFields(u, op.owner)
	fieldType := fields[guard.field].typeName
	for _, function := range rustUnitMembers(u, guard.typ, "drop") {
		if function.name != "drop" || function.owner != guard.typ || function.impl == nil || function.impl.trait != "Drop" {
			continue
		}
		dropBody := u.tokens[function.bodyStart+1 : function.bodyEnd]
		if gradedRustDropBody(op, u, body, units, byKey, acquisition, guard, fieldType, dropBody) {
			return true
		}
	}
	return false
}

func gradedRustDropBody(op *operation, u unit, body []token, units []unit, byKey *operationLookup, acquisition bool, guard gradedRustGuard, fieldType string, dropBody []token) bool {
	var flow unconditionalQueries
	for _, c := range callsIn(dropBody) {
		if !strings.HasPrefix(c.name, "self.0.") || !flow.unconditional(dropBody, c.position) {
			continue
		}
		method := strings.TrimPrefix(c.name, "self.0.")
		if gradedRustRelease(op, u, body, units, byKey, acquisition, guard, fieldType, method) {
			return true
		}
	}
	return false
}

func gradedRustRelease(op *operation, u unit, body []token, units []unit, byKey *operationLookup, acquisition bool, guard gradedRustGuard, fieldType, method string) bool {
	for _, candidate := range rustReleaseCandidates(units, fieldType, method) {
		resourceUnit := units[candidate.unit]
		reset := gradedRustCleanupReset(candidate.function, resourceUnit, 0)
		if len(reset) > 0 && gradedRustReleaseState(op, u, body, units, byKey, acquisition, guard, reset) {
			return true
		}
	}
	return false
}

func gradedRustReleaseState(op *operation, u unit, body []token, units []unit, byKey *operationLookup, acquisition bool, guard gradedRustGuard, reset map[string]string) bool {
	receiver := "self." + guard.field
	acquired, used, invalidated := map[string]bool{}, map[string]bool{}, map[string]bool{}
	guardCalls := false
	effects := map[callSelection]map[string]bool{}
	for _, actual := range callsIn(body) {
		guardCalls = gradedRustReleaseActual(op, u, body, units, byKey, acquisition, guard, receiver, reset, actual, acquired, used, invalidated, effects) || guardCalls
	}
	for field := range reset {
		if acquired[field] && used[field] || !acquisition && (!guardCalls || used[field]) {
			return true
		}
	}
	return false
}

func gradedRustReleaseActual(op *operation, u unit, body []token, units []unit, byKey *operationLookup, acquisition bool, guard gradedRustGuard, receiver string, reset map[string]string, actual call, acquired, used, invalidated map[string]bool, effects map[callSelection]map[string]bool) bool {
	dot := strings.LastIndexByte(actual.name, '.')
	if dot < 0 {
		return false
	}
	guardCalls := false
	if actual.name[:dot] == receiver && actual.position < guard.index {
		selection := gradedCleanupCandidates(op, u, units, actual, byKey)
		for effect, value := range gradedRustAcquisitionEffects(selection, byKey, units, reset, effects) {
			acquired[effect] = value && operationBodyUnconditional(op, body, actual.position)
			invalidated[effect] = !acquired[effect]
		}
	}
	if strings.HasPrefix(actual.name, guard.name+".0.") && actual.position > guard.close {
		guardCalls = true
		connected := actual
		connected.name = receiver + "." + actual.name[strings.LastIndexByte(actual.name, '.')+1:]
		matches := gradedCleanupCandidates(op, u, units, connected, byKey)
		for field := range reset {
			if matches.count() != 1 || gradedOperationWritesField(matches.unique(), units[matches.unique().file], field) {
				acquired[field], invalidated[field] = false, true
			}
		}
		for field, state := range reset {
			used[field] = used[field] || !invalidated[field] && (!acquisition || acquired[field]) && gradedCallObservesProtocol(op, u, units, connected, byKey, map[string]string{field: state})
		}
	}
	for _, argument := range actual.actuals {
		if joinTokens(argument) == guard.name+".0" {
			guardCalls = true
		}
	}
	return guardCalls
}

// Candidate order matters: the last candidate that writes a field supplies
// its state. Cache that position-independent summary once per selected bucket;
// each actual call still applies its own unconditional-execution check.
func gradedRustAcquisitionEffects(selection callSelection, index *operationLookup, units []unit, reset map[string]string, cache map[callSelection]map[string]bool) map[string]bool {
	if effects, ok := cache[selection]; ok {
		return effects
	}
	effects := map[string]bool{}
	selection.each(index, func(resolved *operation) {
		for effect, state := range reset {
			if gradedOperationWritesField(resolved, units[resolved.file], effect) {
				effects[effect] = gradedHasBooleanWrite(resolved, units, effect, gradedOppositeBoolean(state))
			}
		}
	})
	cache[selection] = effects
	return effects
}
