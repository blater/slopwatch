package sourceestimate

import "strings"

func gradedRustVectorType(tokens []token) bool {
	for i, t := range tokens {
		if (t.text == "struct" || t.text == "type" || t.text == "enum") && i+1 < len(tokens) && tokens[i+1].text == "Vec" {
			return false
		}
		if t.text == "Vec" && i > 0 && i+1 < len(tokens) && tokens[i-1].text == "<" && (tokens[i+1].text == ">" || tokens[i+1].text == ":" || tokens[i+1].text == ",") {
			return false
		}
		if t.text == "use" {
			end := i + 1
			for end < len(tokens) && tokens[end].text != ";" {
				end++
			}
			path := joinTokens(tokens[i+1 : end])
			if strings.Contains(path, "Vec") && path != "std::vec::Vec" && path != "alloc::vec::Vec" {
				return false
			}
		}
	}
	return true
}
