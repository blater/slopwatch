package sourceestimate

import "strings"

func gradedIndependentJDBCCleanup(op *operation, u unit, roots []*operation) bool {
	if op.language != "java" {
		return false
	}
	fields := callerDeclaredFields(u, op.owner)
	owned := map[string]bool{}
	for name, f := range fields {
		if f.typeName != "Statement" && f.typeName != "ResultSet" {
			continue
		}
		if !gradedJavaLibraryType(u, f.typeName, "java.sql") {
			continue
		}
		for _, root := range u.ops {
			if root.owner != op.owner || !gradedSurfaceConstructor(root) {
				continue
			}
			for i := 2; i < len(root.body)-1; i++ {
				if root.body[i].text == name && root.body[i-1].text == "." && root.body[i-2].text == "this" && root.body[i+1].text == "=" && i+2 < len(root.body) && root.parameterTypes[root.body[i+2].text] == f.typeName && statementEnd(root.body, i+2) == i+3 {
					owned[name] = true
				}
			}
		}
	}
	attempts := map[string]bool{}
	body := op.body
	for i, t := range body {
		if t.text != "try" || i+1 >= len(body) || body[i+1].text != "{" || !gradedUnconditional(body, i) {
			continue
		}
		close := matching(body, i+1, "{", "}")
		if close < 0 || close+2 >= len(body) || body[close+1].text != "catch" {
			continue
		}
		catchEnd := matching(body, close+2, "(", ")")
		if catchEnd < 0 || catchEnd+1 >= len(body) || body[catchEnd+1].text != "{" {
			continue
		}
		caught := gradedJDBCCatchType(u, body[close+3:catchEnd])
		if !caught {
			continue
		}
		end := matching(body, catchEnd+1, "{", "}")
		if end < 0 {
			continue
		}
		unsafe := false
		for _, tok := range body[catchEnd+2 : end] {
			unsafe = unsafe || tok.text == "return" || tok.text == "throw"
		}
		if unsafe {
			continue
		}
		for _, c := range callsIn(body[i+2 : close]) {
			if !strings.HasSuffix(c.name, ".close") || len(c.actuals) > 0 {
				continue
			}
			receiver := strings.TrimPrefix(strings.TrimSuffix(c.name, ".close"), "this.")
			if owned[receiver] && op.parameterTypes[receiver] == "" && gradedUnconditional(body[i+2:close], c.position) {
				attempts[receiver] = true
			}
		}
	}
	return len(attempts) >= 2
}
