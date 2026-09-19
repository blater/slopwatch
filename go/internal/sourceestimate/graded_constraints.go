package sourceestimate

import (
	"fmt"
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

// Require the actual upper and lower bound, their polarity, and a rejecting
// branch before the access. Unresolved offset forms remain explicit obligations.

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

// Include only uniquely resolved same-owner helpers reached from the boundary.
// Unused private helpers cannot add a caller obligation.

// Every participating location and the receiver identity must retain its
// version after a guard/maintenance update. Unknown intervening calls cannot
// prove preservation.

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
