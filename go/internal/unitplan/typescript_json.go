package unitplan

import "regexp"

var trailingJSONComma = regexp.MustCompile(`,\s*([}\]])`)

func normalizeTSConfigJSON(data []byte) []byte {
	// Strip comments with a small string-aware scanner. tsconfig JSON permits
	// comments and trailing commas even though encoding/json does not.
	result := make([]byte, 0, len(data))
	inString := false
	escaped := false
	for index := 0; index < len(data); index++ {
		char := data[index]
		if inString {
			result = append(result, char)
			if escaped {
				escaped = false
			} else if char == '\\' {
				escaped = true
			} else if char == '"' {
				inString = false
			}
			continue
		}
		if char == '"' {
			inString = true
			result = append(result, char)
			continue
		}
		if char == '/' && index+1 < len(data) && data[index+1] == '/' {
			index = skipLineComment(data, index)
			result = append(result, '\n')
			continue
		}
		if char == '/' && index+1 < len(data) && data[index+1] == '*' {
			index = skipBlockComment(data, index+2)
			continue
		}
		result = append(result, char)
	}
	return trailingJSONComma.ReplaceAll(result, []byte("$1"))
}

func skipLineComment(data []byte, index int) int {
	for index < len(data) && data[index] != '\n' {
		index++
	}
	return index
}

func skipBlockComment(data []byte, index int) int {
	for index+1 < len(data) && !(data[index] == '*' && data[index+1] == '/') {
		index++
	}
	return index + 1
}
