package unitplan

import (
	"fmt"
	pathpkg "path"
	"regexp"
	"strings"
)

type cargoManifest struct {
	path          string
	directory     string
	packageName   string
	workspace     bool
	members       []string
	exclude       []string
	dependencies  []cargoDependency
	workspaceDeps map[string]cargoDependency
}

type cargoDependency struct {
	alias     string
	packageID string
	path      string
	kind      string
	workspace bool
}

func cargoDependencyGraph(context plannerContext, packages map[string]*rustPackage) (map[string]cargoDependencies, bool, []Diagnostic) {
	result := make(map[string]cargoDependencies, len(packages))
	manifests := map[string]cargoManifest{}
	for _, file := range context.files {
		if lastPart(file) != "Cargo.toml" {
			continue
		}
		data, err := context.read(file)
		if err != nil {
			return result, false, cargoDiagnostic(file, err)
		}
		manifest, err := parseCargoManifest(file, string(data))
		if err != nil {
			return result, false, cargoDiagnostic(file, err)
		}
		manifests[manifest.directory] = manifest
	}
	if diagnostic := validateCargoWorkspaces(manifests, packages); diagnostic != nil {
		return result, false, []Diagnostic{*diagnostic}
	}

	for directory := range packages {
		manifest, ok := manifests[directory]
		if !ok || manifest.packageName == "" {
			return result, false, []Diagnostic{{Path: joinPath(directory, "Cargo.toml"), Message: "Cargo package metadata was incomplete; Rust dependencies were broadened"}}
		}
		for _, dependency := range manifest.dependencies {
			resolved := dependency
			if dependency.workspace {
				workspace, found := owningCargoWorkspace(directory, manifests)
				if !found {
					return result, false, []Diagnostic{{Path: manifest.path, Message: "inherited Cargo dependency had no workspace owner; Rust dependencies were broadened"}}
				}
				resolved, found = workspace.workspaceDeps[dependency.alias]
				if !found {
					return result, false, []Diagnostic{{Path: manifest.path, Message: "inherited Cargo dependency was absent from workspace.dependencies; Rust dependencies were broadened"}}
				}
			}
			if resolved.path == "" {
				continue
			}
			base := directory
			if dependency.workspace {
				workspace, _ := owningCargoWorkspace(directory, manifests)
				base = workspace.directory
			}
			owner := joinPath(base, resolved.path)
			pkg := packages[owner]
			if pkg == nil {
				return result, false, []Diagnostic{{Path: manifest.path, Message: "Cargo path dependency did not resolve to a workspace package; Rust dependencies were broadened"}}
			}
			packageID := resolved.packageID
			if packageID == "" {
				packageID = resolved.alias
			}
			if packageID != "" && packageID != pkg.name {
				return result, false, []Diagnostic{{Path: manifest.path, Message: "Cargo path dependency package name did not match its manifest; Rust dependencies were broadened"}}
			}
			entry := result[directory]
			if dependency.kind == "dev" {
				entry.dev = append(entry.dev, owner)
			} else {
				entry.main = append(entry.main, owner)
			}
			result[directory] = entry
		}
	}
	return result, true, nil
}

func cargoDiagnostic(path string, err error) []Diagnostic {
	return []Diagnostic{{Path: path, Message: fmt.Sprintf("Cargo metadata could not be parsed (%v); Rust dependencies were broadened", err)}}
}

func validateCargoWorkspaces(manifests map[string]cargoManifest, packages map[string]*rustPackage) *Diagnostic {
	for _, manifest := range manifests {
		if !manifest.workspace {
			continue
		}
		for _, member := range manifest.members {
			matched := false
			for directory := range packages {
				relative := strings.TrimPrefix(strings.TrimPrefix(directory, manifest.directory), "/")
				if cargoMemberMatch(member, relative) {
					matched = true
					break
				}
			}
			if !matched {
				return &Diagnostic{Path: manifest.path, Message: "Cargo workspace member did not resolve; Rust dependencies were broadened"}
			}
		}
	}
	return nil
}

func cargoMemberMatch(pattern, relative string) bool {
	if strings.ContainsAny(pattern, "*?[") {
		matched, err := pathpkg.Match(pattern, relative)
		return err == nil && matched
	}
	return cleanPath(pattern) == cleanPath(relative)
}

func owningCargoWorkspace(directory string, manifests map[string]cargoManifest) (cargoManifest, bool) {
	best := cargoManifest{}
	found := false
	for _, manifest := range manifests {
		if manifest.workspace && pathWithin(directory, manifest.directory) && (!found || len(manifest.directory) > len(best.directory)) {
			best, found = manifest, true
		}
	}
	return best, found
}

var cargoQuotedString = regexp.MustCompile(`^["']([^"']*)["']$`)

func parseCargoManifest(path, contents string) (cargoManifest, error) {
	manifest := cargoManifest{path: path, directory: pathDirectory(path), workspaceDeps: map[string]cargoDependency{}}
	section := ""
	sectionDependency := ""
	for _, statement := range cargoStatements(contents) {
		if strings.HasPrefix(statement, "[") && strings.HasSuffix(statement, "]") {
			section = strings.Trim(strings.TrimSpace(statement), "[]")
			sectionDependency = dependencySubtableName(section)
			if section == "workspace" {
				manifest.workspace = true
			}
			continue
		}
		parts := strings.SplitN(statement, "=", 2)
		if len(parts) != 2 {
			if strings.TrimSpace(statement) != "" {
				return manifest, fmt.Errorf("unsupported statement %q", statement)
			}
			continue
		}
		key := strings.Trim(strings.TrimSpace(parts[0]), `"'`)
		value := strings.TrimSpace(parts[1])
		switch section {
		case "package":
			if key == "name" {
				name, ok := cargoString(value)
				if !ok {
					return manifest, fmt.Errorf("package name is not a literal string")
				}
				manifest.packageName = name
			}
		case "workspace":
			if key == "members" || key == "exclude" {
				values, ok := cargoStringArray(value)
				if !ok {
					return manifest, fmt.Errorf("workspace %s is not a literal string array", key)
				}
				if key == "members" {
					manifest.members = values
				} else {
					manifest.exclude = values
				}
			}
		default:
			kind, dependencyTable := cargoDependencyTable(section)
			if dependencyTable && sectionDependency == "" {
				dependency, err := parseCargoDependency(key, value, kind)
				if err != nil {
					return manifest, err
				}
				if section == "workspace.dependencies" {
					manifest.workspaceDeps[key] = dependency
				} else {
					manifest.dependencies = append(manifest.dependencies, dependency)
				}
			} else if sectionDependency != "" {
				kind, _ := cargoDependencyTable(strings.TrimSuffix(section, "."+sectionDependency))
				dependency := cargoDependency{alias: sectionDependency, packageID: sectionDependency, kind: kind}
				workspaceDependency := strings.HasPrefix(section, "workspace.dependencies.")
				if workspaceDependency {
					if existing, exists := manifest.workspaceDeps[sectionDependency]; exists {
						dependency = existing
					}
				} else if existing := findCargoDependency(manifest.dependencies, sectionDependency, kind); existing != nil {
					dependency = *existing
				}
				switch key {
				case "path":
					dependency.path, _ = cargoString(value)
				case "package":
					dependency.packageID, _ = cargoString(value)
				case "workspace":
					dependency.workspace = value == "true"
				}
				if workspaceDependency {
					manifest.workspaceDeps[sectionDependency] = dependency
				} else {
					replaceCargoDependency(&manifest, dependency)
				}
			} else if strings.HasPrefix(section, "patch.") || section == "replace" || strings.Contains(key, "dependencies.") || strings.Contains(value, "path") {
				return manifest, fmt.Errorf("unsupported dependency indirection in section %q", section)
			}
		}
	}
	return manifest, nil
}

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
