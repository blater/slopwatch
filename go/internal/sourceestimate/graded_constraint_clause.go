package sourceestimate

import (
	"sort"
	"strings"
)

func gradedOwnerClause(u unit, owner string, fields map[string]gradedSurfaceField, op *operation, body []token, start, end int) (gradedConstraint, bool) {
	comparison, admission, length, equality := false, false, false, false
	names := []string{}
	for j := start; j < end; j++ {
		if body[j].text == "length" && j >= 2 && body[j-1].text == "." {
			field, ok := fields[body[j-2].text]
			length = length || ok && field.alias && gradeOwnedFieldReference(op, u, body, j-2)
		}
		if body[j].text == "len" && j+1 < end && body[j+1].text == "(" && gradedBuiltinName(u, op, "len") {
			length = true
		}
		equality = equality || body[j].text == "==" || body[j].text == "!="
		switch body[j].text {
		case "==", "!=", "<", ">", "<=", ">=":
			comparison = true
		}
		if body[j].text == "if" || body[j].text == "assert" {
			admission = true
		}
		if _, ok := fields[body[j].text]; ok && gradeOwnedFieldReference(op, u, body, j) && !containsString(names, body[j].text) {
			names = append(names, body[j].text)
		}
	}
	if start > 0 && (body[start-1].text == "&&" || body[start-1].text == "||") {
		for j := start - 2; j >= 0 && body[j].text != "{" && body[j].text != ";"; j-- {
			admission = admission || body[j].text == "if" || body[j].text == "assert"
		}
	}
	valid := (!length || equality) && admission && gradedConstraintRejects(op, u, body, end) && (comparison && len(names) == 2 || len(names) == 1 && gradedConstraintRejects(op, u, body, end))
	if !valid {
		return gradedConstraint{}, false
	}
	sort.Strings(names)
	id := owner + "#admission#" + strings.Join(names, ",")
	record := gradedConstraint{id: id, kind: "relational-admission", fields: names, operation: op.id, offset: body[start].offset}
	for j := start + 1; j < end; j++ {
		if body[j].text != "!=" && !(body[j].text == "==" && op.language == "rust") {
			continue
		}
		if _, ok := fields[body[j-1].text]; !ok || !gradeOwnedFieldReference(op, u, body, j-1) {
			continue
		}
		last := j + 1
		for last < end && body[last].text != ")" && body[last].text != "throw" && body[last].text != ";" {
			last++
		}
		right := body[j+1 : last]
		if len(right) > 0 && right[0].text == "=" {
			right = right[1:]
		}
		if hasTransform(right) {
			record.derivedTarget = body[j-1].text
			record.derivedExpression = gradedConstraintExpression(op, u, right)
		}
	}
	return record, true
}
func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
