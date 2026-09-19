package sourceestimate

import "strings"

func gradedConstraintExpression(op *operation, u unit, expression []token) string {
	expression = gradedResultUngroup(expression)
	fields := callerDeclaredFields(u, op.owner)
	result := ""
	for i := 0; i < len(expression); i++ {
		if expression[i].text == "len" && op.language == "go" && gradedBuiltinName(u, op, "len") && i+1 < len(expression) && expression[i+1].text == "(" {
			end := matching(expression, i+1, "(", ")")
			if end > i {
				inner := gradedConstraintExpression(op, u, expression[i+2:end])
				if field, ok := fields[inner]; ok && strings.HasPrefix(field.typeName, "[") {
					result += "size(" + inner + ")"
					i = end
					continue
				}
			}
		}
		// A field's reference start includes its owned receiver qualification.
		fieldIndex := i
		if _, direct := fields[expression[i].text]; !direct && i+2 < len(expression) && expression[i+1].text == "." {
			fieldIndex = i + 2
		}
		field, ok := fields[expression[fieldIndex].text]
		if ok && gradeOwnedFieldReference(op, u, expression, fieldIndex) {
			name := expression[fieldIndex].text
			i = fieldIndex
			if i+2 < len(expression) && expression[i+1].text == "." && expression[i+2].text == "length" && (op.language == "java" || op.language == "typescript") && field.alias {
				result += "size(" + name + ")"
				i += 2
				continue
			}
			if i+4 < len(expression) && expression[i+1].text == "." && expression[i+2].text == "len" && expression[i+3].text == "(" && expression[i+4].text == ")" && op.language == "rust" && gradedBuiltinName(u, op, "Vec") && (field.typeName == "Vec" || strings.HasPrefix(field.typeName, "[")) {
				result += "size(" + name + ")"
				i += 4
				continue
			}
			result += name
			continue
		}
		result += expression[i].text
	}
	return result
}
