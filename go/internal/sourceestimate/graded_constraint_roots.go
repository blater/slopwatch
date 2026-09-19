package sourceestimate

import "strings"

func gradedConstraintRoots(u unit, owner string, roots []*operation) []*operation {
	result := append([]*operation(nil), roots...)
	seen := map[string]bool{}
	for _, op := range roots {
		seen[op.id] = true
	}
	for at := 0; at < len(result) && at < maxCallDepth*16; at++ {
		op := result[at]
		for _, c := range callsIn(normalizedEagerBody(op)) {
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
