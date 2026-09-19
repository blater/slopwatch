package sourceestimate

import "strings"

func gradedRustImportedType(source []token, name string, packages []string) bool {
	found := false
	for i, t := range source {
		if (t.text == "struct" || t.text == "enum" || t.text == "type") && i+1 < len(source) && source[i+1].text == name {
			return false
		}
		if t.text != "use" {
			continue
		}
		end := i + 1
		for end < len(source) && source[end].text != ";" {
			end++
		}
		contains := false
		for _, part := range source[i+1 : end] {
			contains = contains || part.text == name
		}
		if !contains {
			continue
		}
		path := joinTokens(source[i+1 : end])
		valid := false
		for _, pkg := range packages {
			valid = valid || strings.HasPrefix(path, pkg+"::")
		}
		if !valid {
			return false
		}
		found = true
	}
	return found
}
