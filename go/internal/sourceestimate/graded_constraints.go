package sourceestimate

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// ConstraintEvidence exposes stable storage identities and the bounded proof
// independently of the score and of human-readable evidence strings.
type ConstraintEvidence struct {
	ID                  string   `json:"id"`
	Kind                string   `json:"kind"`
	IndexExpression     string   `json:"index_expression,omitempty"`
	Storage             []string `json:"storage"`
	CallerControlled    []string `json:"caller_controlled"`
	Operation           string   `json:"operation"`
	Path                string   `json:"path"`
	Offset              int      `json:"offset"`
	Line                int      `json:"line"`
	Column              int      `json:"column"`
	InternallyProtected bool     `json:"internally_protected"`
	Limitations         []string `json:"limitations"`
}

func (c gradedConstraint) finding(u unit, controlled []string) ConstraintEvidence {
	location := u.file
	if c.source != nil {
		location = *c.source
	}
	line, column := 1, 1
	for i, b := range location.Source {
		if i >= c.offset {
			break
		}
		if b == '\n' {
			line++
			column = 1
		} else {
			column++
		}
	}
	storage := make([]string, 0, len(c.fields))
	for _, field := range c.fields {
		storage = append(storage, u.file.Path+"#"+strings.Split(c.id, "#")[0]+"."+field)
	}
	return ConstraintEvidence{ID: u.file.Path + "#" + c.id, Kind: c.kind, IndexExpression: c.indexExpression, Storage: storage, CallerControlled: controlled, Operation: c.operation, Path: location.Path, Offset: c.offset, Line: line, Column: column, InternallyProtected: c.protected, Limitations: []string{"unknown external callers", "bounded local control and size-contract proof"}}
}

// Constraints witness preconditions on exact storage, never field co-occurrence.
type gradedConstraint struct {
	id, kind                         string
	derivedTarget, derivedExpression string
	indexExpression                  string
	fields                           []string
	operation                        string
	offset                           int
	protected                        bool
	source                           *File
}

func (c gradedConstraint) evidence(u unit) string {
	if c.source != nil {
		u.file = *c.source
	}
	line, column := 1, 1
	for i, b := range u.file.Source {
		if i >= c.offset {
			break
		}
		if b == '\n' {
			line++
			column = 1
		} else {
			column++
		}
	}
	return fmt.Sprintf("constraint:%s:storage=%s:operation=%s:location=%s:%d:%d:internal-protection=%t:caller-control=declared-or-resolved-write:limitations=unknown-external-callers", c.kind, strings.Join(c.fields, ","), c.operation, u.file.Path, line, column, c.protected)
}
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
		body := gradedEagerBody(pruneDeadFalseBranches(op.body), op.language)
		// Relational admission predicates constrain their operand storage. Split
		// Boolean clauses so unrelated mode/metadata fields never join a pair.
		start := 0
		for end := 0; end <= len(body); end++ {
			if end < len(body) && body[end].text != "&&" && body[end].text != "||" && body[end].text != ";" && body[end].text != "{" && body[end].text != "}" {
				continue
			}
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
				if _, ok := fields[body[j].text]; ok && gradeOwnedFieldReference(op, u, body, j) {
					found := false
					for _, name := range names {
						found = found || name == body[j].text
					}
					if !found {
						names = append(names, body[j].text)
					}
				}
			}
			// A continuing Boolean clause inherits its admission predicate.
			if start > 0 && (body[start-1].text == "&&" || body[start-1].text == "||") {
				for j := start - 2; j >= 0 && body[j].text != "{" && body[j].text != ";"; j-- {
					admission = admission || body[j].text == "if" || body[j].text == "assert"
				}
			}
			if (!length || equality) && admission && gradedConstraintRejects(op, u, body, end) && (comparison && len(names) == 2 || len(names) == 1 && gradedConstraintRejects(op, u, body, end)) {
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
				records[id] = record
			}
			start = end + 1
		}

		for i := 1; i < len(body); i++ {
			if body[i].text != "[" {
				continue
			}
			data := i - 1
			if field, ok := fields[body[data].text]; !ok || !field.alias || (op.language == "go" && strings.HasPrefix(field.typeName, "map[")) || !gradeOwnedFieldReference(op, u, body, data) {
				continue
			}
			end := matching(body, i, "[", "]")
			if end < 0 {
				continue
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
				if previous, ok := records[id]; ok {
					if !previous.protected {
						record = previous
					}
				}
				records[id] = record
			}
		}
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

func gradedConstraintRejects(op *operation, u unit, body []token, end int) bool {
	for i := end; i < len(body); i++ {
		if body[i].text == "throw" || gradedConstraintBuiltinReject(op, u, body, i) {
			return true
		}
		if body[i].text == "}" || body[i].text == ";" {
			break
		}
	}
	for i := end - 1; i >= 0 && body[i].text != ";" && body[i].text != "{"; i-- {
		if body[i].text == "throw" || gradedConstraintBuiltinReject(op, u, body, i) {
			return true
		}
	}
	return false
}

// Require the actual upper and lower bound, their polarity, and a rejecting
// branch before the access. Unresolved offset forms remain explicit obligations.
func gradedBoundsProtection(op *operation, u unit, body []token, access int, data, index string, expression []token) bool {
	expr := gradedConstraintExpression(op, u, expression)
	offset := 0
	if expr == index+"-1" {
		offset = 1
	} else if expr != index {
		return false
	}
	lower := map[string]bool{index + "<0": true}
	upper := index + ">=size(" + data + ")"
	if offset == 1 {
		lower = map[string]bool{index + "<=0": true, index + "<1": true}
		upper = index + ">size(" + data + ")"
	}
	for i := 0; i < access; i++ {
		if body[i].text != "if" || !gradedUnconditional(body, i) {
			continue
		}
		open := i + 1
		for open < access && body[open].text != "{" {
			open++
		}
		if open >= access {
			continue
		}
		end := matching(body, open, "{", "}")
		if end < 0 || end >= access || open+1 >= end {
			continue
		}
		// Reject must itself be unconditional, not hidden in a nested branch.
		first := body[open+1].text
		if first != "return" && first != "throw" && !(first == "panic" && op.language == "go" && gradedBuiltinName(u, op, "panic")) {
			continue
		}
		cond := gradedResultUngroup(body[i+1 : open])
		split := -1
		nesting := 0
		for j, t := range cond {
			switch t.text {
			case "(", "[":
				nesting++
			case ")", "]":
				nesting--
			}
			if nesting == 0 && t.text == "||" {
				if split >= 0 {
					split = -1
					break
				}
				split = j
			}
		}
		if split < 0 {
			number, err := strconv.Atoi(expr)
			condition := gradedConstraintExpression(op, u, cond)
			constantGuard := err == nil && number >= 0 && (condition == "size("+data+")<="+expr || condition == "size("+data+")<"+strconv.Itoa(number+1) || number == 0 && condition == "size("+data+")==0")
			if constantGuard && !gradedConstraintChanged(op, u, body, end+1, access, []string{data}) {
				return true
			}
			continue
		}
		left, right := gradedConstraintExpression(op, u, cond[:split]), gradedConstraintExpression(op, u, cond[split+1:])
		if !(lower[left] && right == upper || lower[right] && left == upper) {
			continue
		}
		if !gradedConstraintChanged(op, u, body, end+1, access, []string{data, index}) {
			return true
		}

	}
	return false
}

func gradedBuiltinName(u unit, op *operation, name string) bool {
	if u.shadowedBuiltins[name] {
		return false
	}
	if gradedPackageShadow(u, name) {
		return false
	}

	for _, candidate := range u.ops {
		if candidate.name == name {
			return false
		}
	}
	for _, param := range op.paramNames {
		if param == name {
			return false
		}
	}
	for i, t := range op.body {
		if t.text == name && i+1 < len(op.body) && (op.body[i+1].text == "=" || op.body[i+1].text == ":=") {
			return false
		}
	}
	return true
}

// Normalize only resolved owned storage and built-in collection size syntax.
// No substring match or identifier spelling can establish the guard's logic.
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

// Include only uniquely resolved same-owner helpers reached from the boundary.
// Unused private helpers cannot add a caller obligation.
func gradedConstraintRoots(u unit, owner string, roots []*operation) []*operation {
	result := append([]*operation(nil), roots...)
	seen := map[string]bool{}
	for _, op := range roots {
		seen[op.id] = true
	}
	for at := 0; at < len(result) && at < maxCallDepth*16; at++ {
		op := result[at]
		for _, c := range callsIn(gradedEagerBody(pruneDeadFalseBranches(op.body), op.language)) {
			parts := strings.Split(c.name, ".")
			if len(parts) > 1 && parts[0] != "this" && parts[0] != "self" && parts[0] != op.receiverName {
				continue
			}
			name := parts[len(parts)-1]
			var match *operation
			ambiguous := false
			for _, candidate := range u.ops {
				if candidate.owner == "" {
					copy := *candidate
					copy.owner = gradedInferSurfaceOwner(u.tokens, candidate.language, candidate.name)
					candidate = &copy
				}
				if candidate.owner == owner && candidate.name == name {
					if match != nil {
						ambiguous = true
					}
					match = candidate
				}
			}
			if match != nil && !ambiguous && !seen[match.id] {
				seen[match.id] = true
				result = append(result, match)
			}
		}
	}
	return result
}

func gradedConstraintBuiltinReject(op *operation, u unit, body []token, i int) bool {
	name := body[i].text
	if op.language == "go" && name == "panic" && i+1 < len(body) && body[i+1].text == "(" {
		return gradedBuiltinName(u, op, name)
	}
	if op.language == "rust" && name == "assert" && i+1 < len(body) && body[i+1].text == "!" {
		for j := 0; j+2 < len(u.tokens); j++ {
			if u.tokens[j].text == "macro_rules" && u.tokens[j+2].text == name {
				return false
			}
		}
		return true
	}
	return false
}

// Every participating location and the receiver identity must retain its
// version after a guard/maintenance update. Unknown intervening calls cannot
// prove preservation.
func gradedConstraintChanged(op *operation, u unit, body []token, start, end int, fields []string) bool {
	if start > end {
		return true
	}
	for _, call := range callsIn(body[start:end]) {
		if !(call.name == "len" && op.language == "go" && gradedBuiltinName(u, op, "len")) {
			return true
		}
	}
	receiver := op.receiverName
	if op.constraintReceiver != "" {
		receiver = op.constraintReceiver
	}
	if op.language == "go" {
		for operator := start; operator < end; operator++ {
			if body[operator].text != "=" && body[operator].text != ":=" {
				continue
			}
			for _, target := range gradedAssignmentTargets(body, operator) {
				if target < start {
					continue
				}
				if receiver != "" && body[target].text == receiver && (target == 0 || body[target-1].text != ".") {
					return true
				}
				for _, name := range fields {
					if body[target].text == name && gradeOwnedFieldReference(op, u, body, target) {
						return true
					}
				}
			}
		}
	}
	for j := start; j < end; j++ {
		if receiver != "" && body[j].text == receiver && j+1 < end && (body[j+1].text == "=" || body[j+1].text == ":=") {
			return true
		}
		for _, name := range fields {
			if body[j].text == name && gradeOwnedFieldReference(op, u, body, j) && gradedStorageWriteAt(body, j) {
				return true
			}
		}
	}
	return false
}

func gradedPackageShadow(u unit, name string) bool {
	depth := 0
	for i, t := range u.tokens {
		if t.text == "{" {
			depth++
		}
		if t.text == "}" {
			depth--
		}
		if depth != 0 || i == 0 || t.text != name {
			continue
		}
		switch u.tokens[i-1].text {
		case "var", "const", "type", "struct", "class", "fn", "func":
			return true
		}
	}
	return false
}
