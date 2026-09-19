package sourceestimate

func gradedStorageEffectAt(op *operation, u unit, body []token, fields map[string]gradedSurfaceField, i int, operator string) (gradedStorageEffect, bool) {
	if !gradedStorageOperator(operator) {
		return gradedStorageEffect{}, false
	}
	target := i - 1
	if (operator == "++" || operator == "--") && i+1 < len(body) && isIdentifier(body[i+1].text) {
		target = i + 1
		if isMember(body, target) {
			target += 2
		}
	}
	if target < 0 || target >= len(body) || !isIdentifier(body[target].text) {
		return gradedStorageEffect{}, false
	}
	_, declared := fields[body[target].text]
	owned := declared && gradeOwnedFieldReference(op, u, body, target)
	if owned && op.language == "go" && !gradedMutableGoReceiver(op, u) {
		return gradedStorageEffect{}, false
	}
	member := target >= 2 && body[target-1].text == "."
	if !owned && !member {
		return gradedStorageEffect{}, false
	}
	rhs := body[i+1 : statementEnd(body, i+1)]
	state, computed := gradedStorageEffectValue(op, u, body, i, target, operator, owned, member, rhs)
	if owned && state && gradedUnitStep(op, u, body, i, target) {
		computed = false
	}
	name := body[target].text
	if !owned && member {
		name = joinTokens(body[target-2 : target+1])
	}
	return gradedStorageEffect{target: name, computed: computed, state: state}, true
}

func gradedStorageEffectValue(op *operation, u unit, body []token, i, target int, operator string, owned, member bool, rhs []token) (state, computed bool) {
	state = operator == "++" || operator == "--"
	if operator != "=" && !state {
		state = compoundUpdateChanges(rhs, operator)
		computed = owned && state
	} else if operator == "=" {
		computed = owned && hasTransform(rhs)
		if member {
			state = readsAndChangesMember(body[target-2:target+1], rhs)
		}
		if owned && hasTransform(rhs) {
			for j, t := range rhs {
				if t.text == body[target].text && gradeOwnedFieldReference(op, u, body, i+1+j) {
					state = true
				}
			}
		}
	}
	return state, computed
}
