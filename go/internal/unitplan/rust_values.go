package unitplan

import (
	"fmt"
	"regexp"
	"strings"
)

var cargoQuotedString = regexp.MustCompile(`^["']([^"']*)["']$`)

func parseCargoDependency(alias, value, kind string) (cargoDependency, error) {
	dependency := cargoDependency{alias: alias, packageID: alias, kind: kind}
	if _, ok := cargoString(value); ok {
		return dependency, nil
	}
	fields, ok := cargoInlineTable(value)
	if !ok {
		return dependency, fmt.Errorf("dependency %s uses unsupported metadata", alias)
	}
	if path, present := fields["path"]; present {
		dependency.path, ok = cargoString(path)
		if !ok {
			return dependency, fmt.Errorf("dependency %s path is not literal", alias)
		}
	}
	if packageID, present := fields["package"]; present {
		dependency.packageID, ok = cargoString(packageID)
		if !ok {
			return dependency, fmt.Errorf("dependency %s package is not literal", alias)
		}
	}
	if workspace, present := fields["workspace"]; present {
		if workspace != "true" && workspace != "false" {
			return dependency, fmt.Errorf("dependency %s workspace flag is not literal", alias)
		}
		dependency.workspace = workspace == "true"
	}
	return dependency, nil
}

func cargoString(value string) (string, bool) {
	match := cargoQuotedString.FindStringSubmatch(strings.TrimSpace(value))
	if match == nil {
		return "", false
	}
	return match[1], true
}

func cargoStringArray(value string) ([]string, bool) {
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(value, "[") || !strings.HasSuffix(value, "]") {
		return nil, false
	}
	inner := strings.TrimSpace(value[1 : len(value)-1])
	if inner == "" {
		return nil, true
	}
	parts := splitCargoFields(inner)
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		item, ok := cargoString(part)
		if !ok {
			return nil, false
		}
		result = append(result, item)
	}
	return result, true
}

func cargoInlineTable(value string) (map[string]string, bool) {
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(value, "{") || !strings.HasSuffix(value, "}") {
		return nil, false
	}
	result := map[string]string{}
	for _, field := range splitCargoFields(strings.TrimSpace(value[1 : len(value)-1])) {
		parts := strings.SplitN(field, "=", 2)
		if len(parts) != 2 {
			return nil, false
		}
		result[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
	}
	return result, true
}

func splitCargoFields(value string) []string {
	var result []string
	start, depth := 0, 0
	inString := byte(0)
	escaped := false
	for index := 0; index < len(value); index++ {
		char := value[index]
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
		} else if char == ',' && depth == 0 {
			result = append(result, strings.TrimSpace(value[start:index]))
			start = index + 1
		}
	}
	if tail := strings.TrimSpace(value[start:]); tail != "" {
		result = append(result, tail)
	}
	return result
}

func findCargoDependency(dependencies []cargoDependency, alias, kind string) *cargoDependency {
	for index := range dependencies {
		if dependencies[index].alias == alias && dependencies[index].kind == kind {
			return &dependencies[index]
		}
	}
	return nil
}

func replaceCargoDependency(manifest *cargoManifest, dependency cargoDependency) {
	for index := range manifest.dependencies {
		if manifest.dependencies[index].alias == dependency.alias && manifest.dependencies[index].kind == dependency.kind {
			manifest.dependencies[index] = dependency
			return
		}
	}
	manifest.dependencies = append(manifest.dependencies, dependency)
}
