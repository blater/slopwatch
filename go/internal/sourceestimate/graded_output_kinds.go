package sourceestimate

import "strings"

func gradedOutputBufferKinds(op *operation, u unit) (map[string]string, map[string]string) {
	kinds, aliases := map[string]string{}, map[string]string{}
	for name, typ := range op.parameterTypes {
		vector := op.language == "rust" && strings.HasPrefix(typ, "&mutVec<")
		array := op.language == "typescript" && (strings.HasSuffix(typ, "[]") || strings.HasPrefix(typ, "Array<"))
		slice := op.language == "go" && strings.HasPrefix(typ, "[]")
		if vector && !gradedRustVectorType(u.tokens) {
			vector = false
		}
		if vector {
			kinds[name] = "vector"
		}
		if array {
			kinds[name] = "array"
		}
		if slice {
			kinds[name] = "slice"
		}
		if kinds[name] != "" {
			aliases[name] = name
		}
	}
	return kinds, aliases
}
