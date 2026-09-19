package sourceestimate

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// Only published reasons can affect filtering. Index their exact arguments by
// name and byte length; map lookup retains exact equality even on hash collision.
// Normal call measurement still emits exact bounded signatures: deeply nested
// signatures can intrinsically contain quadratic bytes. Filtering must not
// manufacture every hypothetical signature merely to discard it.
type callLimitationIndex map[string]map[int]map[string]string

func neededCallLimitations(limitations []string) callLimitationIndex {
	result := callLimitationIndex{}
	const prefix = "unresolved_call_range_0_2:"
	for _, reason := range limitations {
		if !strings.HasPrefix(reason, prefix) {
			continue
		}
		name, args, ok := strings.Cut(reason[len(prefix):], "/")
		if !ok {
			continue
		}
		if result[name] == nil {
			result[name] = map[int]map[string]string{}
		}
		if result[name][len(args)] == nil {
			result[name][len(args)] = map[string]string{}
		}
		result[name][len(args)][args] = reason
	}
	return result
}

func matchCallLimitations(body []token, needed callLimitationIndex, excluded map[string]bool) {
	calls := callsIn(body)
	hasNeededName := false
	for _, c := range calls {
		if len(needed[c.name]) > 0 {
			hasNeededName = true
			break
		}
	}
	if !hasNeededName {
		return
	}
	offsets := make([]int, len(body)+1)
	var builder strings.Builder
	for i, item := range body {
		builder.WriteString(item.text)
		offsets[i+1] = builder.Len()
	}
	flat := builder.String()
	// Precompute exact TrimSpace boundaries at every byte position, including
	// invalid UTF-8 and token boundaries inside a rune. This avoids repeatedly
	// scanning long whitespace spans in overlapping malformed argument ranges.
	left, right := make([]int, len(flat)+1), make([]int, len(flat)+1)
	left[len(flat)] = len(flat)
	for i := len(flat) - 1; i >= 0; i-- {
		left[i] = i
		r, size := utf8.DecodeRuneInString(flat[i:])
		if unicode.IsSpace(r) {
			left[i] = left[i+size]
		}
	}
	for i := 1; i <= len(flat); i++ {
		right[i] = i
		r, size := utf8.DecodeLastRuneInString(flat[:i])
		if unicode.IsSpace(r) {
			right[i] = right[i-size]
		}
	}
	for _, c := range calls {
		lengths := needed[c.name]
		if len(lengths) == 0 {
			continue
		}
		start, end := offsets[c.position+2], offsets[c.position+2+len(c.argumentTokens)]
		trimmedStart, trimmedEnd := left[start], right[end]
		// Bound whitespace runs to this substring. If a synthetic token splits
		// a whitespace rune, its partial bytes are invalid UTF-8 and retained
		// by TrimSpace. Inspect only the at-most-three crossing bytes.
		if trimmedStart > end {
			trimmedStart = end
			for i := max(start, end-utf8.UTFMax+1); i < end; i++ {
				r, size := utf8.DecodeRuneInString(flat[i:])
				if i+size > end && unicode.IsSpace(r) {
					trimmedStart = i
					break
				}
			}
		}
		if trimmedEnd < start {
			trimmedEnd = start
			for i := max(0, start-utf8.UTFMax+1); i < start; i++ {
				r, size := utf8.DecodeRuneInString(flat[i:])
				if i+size > start && unicode.IsSpace(r) {
					trimmedEnd = min(end, i+size)
					break
				}
			}
		}
		if trimmedStart > trimmedEnd {
			trimmedEnd = trimmedStart
		}
		args := flat[trimmedStart:trimmedEnd]
		if candidates := lengths[len(args)]; len(candidates) > 0 {
			if reason, ok := candidates[args]; ok {
				excluded[reason] = true
			}
		}
	}
}
