package sourceestimate

import (
	"sort"
	"strings"
)

func gradedOwnerIndexAt(records map[string]gradedConstraint, u unit, owner string, fields map[string]gradedSurfaceField, op *operation, body []token, i int) {
	data := i - 1
	field, ok := fields[body[data].text]
	if !ok || !field.alias || op.language == "go" && strings.HasPrefix(field.typeName, "map[") || !gradeOwnedFieldReference(op, u, body, data) {
		return
	}
	end := matching(body, i, "[", "]")
	if end < 0 {
		return
	}
	indexFields := false
	for j := i + 1; j < end; j++ {
		if _, ok := fields[body[j].text]; ok && gradeOwnedFieldReference(op, u, body, j) {
			indexFields = true
		}
	}
	if !indexFields {
		id := owner + "#indexed-alias#" + body[data].text
		record := gradedConstraint{id: id, kind: "indexed-alias", fields: []string{body[data].text}, operation: op.id, offset: body[i].offset, indexExpression: gradedConstraintExpression(op, u, body[i+1:end])}
		record.protected = gradedBoundsProtection(op, u, body, i, body[data].text, record.indexExpression, body[i+1:end])
		if previous, ok := records[id]; ok && !previous.protected {
			record = previous
		}
		records[id] = record
	}
	for j := i + 1; j < end; j++ {
		if _, ok := fields[body[j].text]; !ok || body[j].text == body[data].text || !gradeOwnedFieldReference(op, u, body, j) {
			continue
		}
		names := []string{body[data].text, body[j].text}
		sort.Strings(names)
		id := owner + "#bounds#" + strings.Join(names, ",")
		record := gradedConstraint{id: id, kind: "index-bounds", fields: names, operation: op.id, offset: body[i].offset, indexExpression: gradedConstraintExpression(op, u, body[i+1:end])}
		record.protected = gradedBoundsProtection(op, u, body, i, body[data].text, body[j].text, body[i+1:end])
		if previous, ok := records[id]; ok && !previous.protected {
			record = previous
		}
		records[id] = record
	}
}
