package sourceestimate

func gradedCallObservesProtocol(op *operation, u unit, units []unit, c call, byKey *operationLookup, fields map[string]string) bool {
	candidates := gradedCleanupCandidates(op, u, units, c, byKey)
	if candidates.count() != 1 {
		return false
	}
	candidate := candidates.unique()
	for i, tok := range candidate.body {
		if fields[tok.text] != "" && gradeOwnedFieldReference(candidate, units[candidate.file], candidate.body, i) && !gradedStorageWriteAt(candidate.body, i) && gradedProtocolReadAdmits(candidate, units[candidate.file], i, fields[tok.text]) {
			return true
		}
	}
	return false
}
func gradedOppositeBoolean(value string) string {
	if value == "true" {
		return "false"
	}
	return "true"
}
func gradedHasBooleanWrite(op *operation, units []unit, field, value string) bool {
	for _, found := range gradedBooleanWrites(op, units, value) {
		if found == field {
			return true
		}
	}
	return false
}
func gradedProtocolStatement(body []token, start int) ([]token, int) {
	if start >= len(body) {
		return nil, len(body)
	}
	if body[start].text == "{" {
		end := matching(body, start, "{", "}")
		if end < 0 {
			return nil, len(body)
		}
		return body[start+1 : end], end + 1
	}
	end := start
	for end < len(body) && body[end].text != ";" {
		end++
	}
	next := end
	if next < len(body) {
		next++
	}
	return body[start:end], next
}
