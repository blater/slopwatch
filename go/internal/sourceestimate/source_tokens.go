package sourceestimate

import (
	"strings"
	"unicode"
)

func locateTokens(tokens []token, source []byte) {
	at, line := 0, 1
	for i := range tokens {
		for at < tokens[i].offset && at < len(source) {
			if source[at] == '\n' {
				line++
			}
			at++
		}
		tokens[i].line = line
	}
}

func matching(tokens []token, start int, open, close string) int {
	depth := 0
	for i := start; i < len(tokens); i++ {
		if tokens[i].text == open {
			depth++
		}
		if tokens[i].text == close {
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

func isIdentifier(s string) bool {
	if s == "" || !(unicode.IsLetter(rune(s[0])) || s[0] == '_' || s[0] == '$') {
		return false
	}
	for _, r := range s[1:] {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' && r != '$' {
			return false
		}
	}
	return true
}

func isControl(s string) bool {
	switch s {
	case "if", "for", "while", "switch", "catch", "return", "sizeof", "match":
		return true
	default:
		return false
	}
}

func isOperator(s string) bool { return strings.Contains("+-*/%&|^=!<>", s) }

func joinTokens(tokens []token) string {
	var b strings.Builder
	for _, t := range tokens {
		b.WriteString(t.text)
	}
	return b.String()
}

func unique(values []string) []string {
	out := values[:0]
	seen := map[string]bool{}
	for _, value := range values {
		if !seen[value] {
			seen[value] = true
			out = append(out, value)
		}
	}
	return out
}

func maxFloat(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func itoa(value int) string {
	const digits = "0123456789"
	if value == 0 {
		return "0"
	}
	var out [20]byte
	i := len(out)
	for value > 0 {
		i--
		out[i] = digits[value%10]
		value /= 10
	}
	return string(out[i:])
}
