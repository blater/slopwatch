package unitplan

import (
	"fmt"
	"strings"
)

type cargoManifestParser struct {
	manifest          cargoManifest
	section           string
	sectionDependency string
}

func parseCargoManifest(path, contents string) (cargoManifest, error) {
	parser := cargoManifestParser{manifest: cargoManifest{path: path, directory: pathDirectory(path), workspaceDeps: map[string]cargoDependency{}}}
	for _, statement := range cargoStatements(contents) {
		if parser.setSection(statement) {
			continue
		}
		if err := parser.parseStatement(statement); err != nil {
			return parser.manifest, err
		}
	}
	return parser.manifest, nil
}

func (parser *cargoManifestParser) setSection(statement string) bool {
	if !strings.HasPrefix(statement, "[") || !strings.HasSuffix(statement, "]") {
		return false
	}
	parser.section = strings.Trim(strings.TrimSpace(statement), "[]")
	parser.sectionDependency = dependencySubtableName(parser.section)
	if parser.section == "workspace" {
		parser.manifest.workspace = true
	}
	return true
}

func (parser *cargoManifestParser) parseStatement(statement string) error {
	parts := strings.SplitN(statement, "=", 2)
	if len(parts) != 2 {
		if strings.TrimSpace(statement) != "" {
			return fmt.Errorf("unsupported statement %q", statement)
		}
		return nil
	}
	key := strings.Trim(strings.TrimSpace(parts[0]), `"'`)
	value := strings.TrimSpace(parts[1])
	switch parser.section {
	case "package":
		return parser.parsePackageField(key, value)
	case "workspace":
		return parser.parseWorkspaceField(key, value)
	default:
		return parser.parseDependencyField(key, value)
	}
}

func (parser *cargoManifestParser) parsePackageField(key, value string) error {
	if key != "name" {
		return nil
	}
	name, ok := cargoString(value)
	if !ok {
		return fmt.Errorf("package name is not a literal string")
	}
	parser.manifest.packageName = name
	return nil
}

func (parser *cargoManifestParser) parseWorkspaceField(key, value string) error {
	if key != "members" && key != "exclude" {
		return nil
	}
	values, ok := cargoStringArray(value)
	if !ok {
		return fmt.Errorf("workspace %s is not a literal string array", key)
	}
	if key == "members" {
		parser.manifest.members = values
	} else {
		parser.manifest.exclude = values
	}
	return nil
}

func (parser *cargoManifestParser) parseDependencyField(key, value string) error {
	kind, dependencyTable := cargoDependencyTable(parser.section)
	if dependencyTable && parser.sectionDependency == "" {
		return parser.parseDependencyTable(key, value, kind)
	}
	if parser.sectionDependency != "" {
		return parser.parseDependencySubtable(key, value, kind)
	}
	if strings.HasPrefix(parser.section, "patch.") || parser.section == "replace" || strings.Contains(key, "dependencies.") || strings.Contains(value, "path") {
		return fmt.Errorf("unsupported dependency indirection in section %q", parser.section)
	}
	return nil
}

func (parser *cargoManifestParser) parseDependencyTable(key, value, kind string) error {
	dependency, err := parseCargoDependency(key, value, kind)
	if err != nil {
		return err
	}
	if parser.section == "workspace.dependencies" {
		parser.manifest.workspaceDeps[key] = dependency
	} else {
		parser.manifest.dependencies = append(parser.manifest.dependencies, dependency)
	}
	return nil
}

func (parser *cargoManifestParser) parseDependencySubtable(key, value, kind string) error {
	baseSection := strings.TrimSuffix(parser.section, "."+parser.sectionDependency)
	kind, _ = cargoDependencyTable(baseSection)
	dependency := cargoDependency{alias: parser.sectionDependency, packageID: parser.sectionDependency, kind: kind}
	workspaceDependency := strings.HasPrefix(parser.section, "workspace.dependencies.")
	if workspaceDependency {
		if existing, exists := parser.manifest.workspaceDeps[parser.sectionDependency]; exists {
			dependency = existing
		}
	} else if existing := findCargoDependency(parser.manifest.dependencies, parser.sectionDependency, kind); existing != nil {
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
		parser.manifest.workspaceDeps[parser.sectionDependency] = dependency
	} else {
		replaceCargoDependency(&parser.manifest, dependency)
	}
	return nil
}
