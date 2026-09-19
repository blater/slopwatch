package sourceestimate

import "strings"

func gradedRustStandardDrop(tokens []token, impl rustImplRange) bool {
	if impl.trait != "Drop" {
		return false
	}
	for i, t := range tokens {
		if t.text == "trait" && i+1 < len(tokens) && tokens[i+1].text == "Drop" {
			return false
		}
		if t.text == "use" {
			end := i + 1
			for end < len(tokens) && tokens[end].text != ";" {
				end++
			}
			path := joinTokens(tokens[i+1 : end])
			hasDrop := false
			for _, part := range tokens[i+1 : end] {
				hasDrop = hasDrop || part.text == "Drop"
			}
			if hasDrop && !strings.HasPrefix(path, "core::ops::") && !strings.HasPrefix(path, "std::ops::") {
				return false
			}
		}
	}
	start := impl.start + 1
	if start < len(tokens) && tokens[start].text == "<" {
		end := rustGenericEnd(tokens, start)
		if end < 0 {
			return false
		}
		start = end + 1
	}
	end := start
	for end < len(tokens) && tokens[end].text != "for" && tokens[end].text != "{" {
		end++
	}
	path := joinTokens(tokens[start:end])
	return path == "Drop" || path == "core::ops::Drop" || path == "std::ops::Drop"
}
