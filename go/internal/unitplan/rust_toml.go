package unitplan

import "strings"

func cargoStatements(contents string) []string {
	var result []string
	var current strings.Builder
	depth := 0
	for _, raw := range strings.Split(contents, "\n") {
		line := strings.TrimSpace(stripTOMLComment(raw))
		if line == "" {
			continue
		}
		if current.Len() > 0 {
			current.WriteByte(' ')
		}
		current.WriteString(line)
		depth += cargoBracketDelta(line)
		if depth <= 0 {
			result = append(result, current.String())
			current.Reset()
			depth = 0
		}
	}
	if current.Len() > 0 {
		result = append(result, current.String())
	}
	return result
}

func cargoBracketDelta(value string) int {
	depth := 0
	inString := rune(0)
	escaped := false
	for _, char := range value {
		if inString != 0 {
			if escaped {
				escaped = false
			} else if char == '\\' && inString == '"' {
				escaped = true
			} else if char == inString {
				inString = 0
			}
			continue
		}
		if char == '"' || char == '\'' {
			inString = char
		} else if char == '[' || char == '{' {
			depth++
		} else if char == ']' || char == '}' {
			depth--
		}
	}
	return depth
}

func stripTOMLComment(value string) string {
	inString := rune(0)
	escaped := false
	for index, char := range value {
		if inString != 0 {
			if escaped {
				escaped = false
			} else if char == '\\' && inString == '"' {
				escaped = true
			} else if char == inString {
				inString = 0
			}
			continue
		}
		if char == '"' || char == '\'' {
			inString = char
		} else if char == '#' {
			return value[:index]
		}
	}
	return value
}

func cargoDependencyTable(section string) (string, bool) {
	if section == "dependencies" || strings.HasSuffix(section, ".dependencies") {
		return "main", true
	}
	if section == "dev-dependencies" || strings.HasSuffix(section, ".dev-dependencies") {
		return "dev", true
	}
	if section == "build-dependencies" || strings.HasSuffix(section, ".build-dependencies") {
		return "build", true
	}
	return "", false
}

func dependencySubtableName(section string) string {
	for _, marker := range []string{"dependencies.", "dev-dependencies.", "build-dependencies."} {
		if index := strings.LastIndex(section, marker); index >= 0 {
			return strings.Trim(section[index+len(marker):], `"'`)
		}
	}
	return ""
}
