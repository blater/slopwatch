package sourceestimate

import "strconv"

// CallerSurface is a bounded estimate of the ordinary work a caller still
// carries at an abstraction boundary. It is intentionally separate from the
// responsibility/depth estimate: a zero surface is not proof of a passive
// role, and these units do not alter scoring by themselves.
type CallerSurface struct {
	OperationUnits      float64
	InputUnits          float64
	RepresentationUnits float64
	Evidence            []string
	Constraints         []ConstraintEvidence
}

const gradedSurfaceTokenLimit = 20000

type gradedSurfaceField struct {
	name           string
	typeName       string
	public         bool
	packageVisible bool
	mutable        bool
	alias          bool
}

type gradedSurfaceOwner struct {
	open, close int
	fields      map[string]gradedSurfaceField
}

// callerDeclaredFields exposes the bounded owner inventory to the graded
// evidence pass. The returned map is scoped to this unit and owner; it never
// performs a workspace-wide declaration scan.
func callerDeclaredFields(u unit, owner string) map[string]gradedSurfaceField {
	limit := len(u.tokens)
	if limit > gradedSurfaceTokenLimit {
		limit = gradedSurfaceTokenLimit
	}
	if limit == 0 {
		return nil
	}
	declaration := gradedFindSurfaceOwner(u.tokens[:limit], normalizeLanguage(u.file.Language, u.file.Path), owner)
	if declaration == nil {
		return nil
	}
	return declaration.fields
}

func formatSurfaceUnits(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}

func gradedDirectRead(body []token) bool {
	if len(body) < 2 || body[0].text != "return" || gradedBodyHasCall(body) || gradedBodyHasControl(body) || gradedHasAssignment(body) {
		return false
	}
	for _, item := range body[1:] {
		switch item.text {
		case "+", "-", "*", "/", "%", "&", "|", "^", "<<", ">>", "?", ":":
			return false
		}
	}
	return true
}

func gradedHasAssignment(body []token) bool {
	for _, item := range body {
		if item.text == "=" || item.text == ":=" || item.text == "+=" || item.text == "-=" || item.text == "*=" || item.text == "/=" {
			return true
		}
	}
	return false
}

func gradedBodyHasCall(body []token) bool {
	for i := 1; i < len(body); i++ {
		if body[i].text == "(" && isIdentifier(body[i-1].text) && body[i-1].text != "if" && body[i-1].text != "return" {
			return true
		}
	}
	return false
}

func gradedBodyHasControl(body []token) bool {
	for _, item := range body {
		switch item.text {
		case "if", "else", "for", "while", "switch", "match", "try", "catch", "throw":
			return true
		}
	}
	return false
}

func gradedFieldTypeAfter(segment []token, separator int) string {
	if separator < 0 || separator+1 >= len(segment) || segment[separator].text == "=" {
		return ""
	}
	if isIdentifier(segment[separator+1].text) {
		return segment[separator+1].text
	}
	return ""
}

func gradedSegmentHasCall(segment []token) bool {
	for i := 1; i < len(segment); i++ {
		if segment[i].text == "(" && isIdentifier(segment[i-1].text) {
			return true
		}
	}
	return false
}

func gradedAliasType(tokens []token) bool {
	for _, item := range tokens {
		switch item.text {
		case "[", "]", "*", "&", "Vec", "map", "Map", "List", "Set", "HashMap", "slice":
			return true
		}
	}
	return false
}

func gradedSegmentContains(segment []token, name string) bool {
	for _, t := range segment {
		if t.text == name {
			return true
		}
	}
	return false
}
