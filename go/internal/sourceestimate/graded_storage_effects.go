package sourceestimate

func gradedStorageEffects(op *operation, u unit, body []token) []gradedStorageEffect {
	fields := callerDeclaredFields(u, op.owner)
	result := []gradedStorageEffect{}
	for i, tok := range body {
		if effect, ok := gradedStorageEffectAt(op, u, body, fields, i, tok.text); ok {
			result = append(result, effect)
		}
	}
	return result
}

func gradedStorageOperator(operator string) bool {
	switch operator {
	case "=", "+=", "-=", "*=", "/=", "%=", "++", "--":
		return true
	default:
		return false
	}
}
