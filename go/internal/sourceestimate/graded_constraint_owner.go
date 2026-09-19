package sourceestimate

import "sort"

func gradedOwnerConstraints(u unit, owner string, roots []*operation) []gradedConstraint {
	fields := callerDeclaredFields(u, owner)
	records := map[string]gradedConstraint{}
	for _, op := range roots {
		if op.owner == "" {
			copy := *op
			copy.owner = gradedInferSurfaceOwner(u.tokens, op.language, op.name)
			op = &copy
		}
		if op.owner != owner {
			continue
		}
		body := normalizedEagerBody(op)
		gradedOwnerRelational(records, u, owner, fields, op, body)
		gradedOwnerIndexed(records, u, owner, fields, op, body)
	}
	ids := make([]string, 0, len(records))
	for id := range records {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	result := make([]gradedConstraint, 0, len(ids))
	for _, id := range ids {
		result = append(result, records[id])
	}
	return result
}

func gradedOwnerRelational(records map[string]gradedConstraint, u unit, owner string, fields map[string]gradedSurfaceField, op *operation, body []token) {
	start := 0
	for end := 0; end <= len(body); end++ {
		if end < len(body) && body[end].text != "&&" && body[end].text != "||" && body[end].text != ";" && body[end].text != "{" && body[end].text != "}" {
			continue
		}
		if record, ok := gradedOwnerClause(u, owner, fields, op, body, start, end); ok {
			records[record.id] = record
		}
		start = end + 1
	}
}

func gradedOwnerIndexed(records map[string]gradedConstraint, u unit, owner string, fields map[string]gradedSurfaceField, op *operation, body []token) {
	for i := 1; i < len(body); i++ {
		if body[i].text != "[" {
			continue
		}
		gradedOwnerIndexAt(records, u, owner, fields, op, body, i)
	}
}
