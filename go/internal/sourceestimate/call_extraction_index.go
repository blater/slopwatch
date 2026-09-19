package sourceestimate

// Parentheses match independently of other delimiters. Argument depth instead
// treats every opener/closer alike, including crossed or unmatched delimiters.
// Keep separate indexes to preserve both legacy behaviors on malformed input.
type callDelimiterIndex struct {
	parentheses []int
	nextComma   []int
}

func indexCallDelimiters(body []token) callDelimiterIndex {
	n := len(body)
	index := callDelimiterIndex{make([]int, n), make([]int, n+1)}
	untyped := make([]int, n)
	parenStack, depthStack := make([]int, 0), make([]int, 0)
	for i, item := range body {
		index.parentheses[i], untyped[i] = -1, n
		switch item.text {
		case "(":
			parenStack = append(parenStack, i)
		case ")":
			if len(parenStack) > 0 {
				last := len(parenStack) - 1
				index.parentheses[parenStack[last]] = i
				parenStack = parenStack[:last]
			}
		}
		switch item.text {
		case "(", "[", "{":
			depthStack = append(depthStack, i)
		case ")", "]", "}":
			if len(depthStack) > 0 {
				last := len(depthStack) - 1
				untyped[depthStack[last]] = i
				depthStack = depthStack[:last]
			}
		}
	}
	index.nextComma[n] = n
	for i := n - 1; i >= 0; i-- {
		switch body[i].text {
		case ",":
			index.nextComma[i] = i
		case "(", "[", "{":
			index.nextComma[i] = n
			if untyped[i] < n {
				index.nextComma[i] = index.nextComma[untyped[i]+1]
			}
		default:
			index.nextComma[i] = index.nextComma[i+1]
		}
	}
	return index
}

func (index callDelimiterIndex) arguments(body []token, start, end int) [][]token {
	if start == end {
		return nil
	}
	result := make([][]token, 0, 2)
	for comma := index.nextComma[start]; comma < end; comma = index.nextComma[start] {
		result = append(result, body[start:comma])
		start = comma + 1
	}
	return append(result, body[start:end])
}
