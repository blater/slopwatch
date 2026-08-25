package unitplan

import (
	"encoding/xml"
	"strings"
)

func buildMavenDependencyGraph(context plannerContext, modules map[string]*javaModule) (map[string]javaDependencies, bool, []Diagnostic) {
	result := make(map[string]javaDependencies, len(modules))
	if len(modules) < 2 {
		return result, true, nil
	}
	metadata := map[string]mavenMetadata{}
	for directory, module := range modules {
		data, err := context.read(module.manifest)
		if err != nil {
			return result, false, javaMetadataDiagnostic(module.manifest, err)
		}
		var project mavenXMLProject
		if err := xml.Unmarshal(data, &project); err != nil {
			return result, false, javaMetadataDiagnostic(module.manifest, err)
		}
		if hasUnsupportedMavenFeatures(string(data)) {
			return result, false, []Diagnostic{{Path: module.manifest, Message: "Maven profiles or build extensions require conservative Java dependencies"}}
		}
		metadata[directory] = mavenMetadataFromProject(directory, module.manifest, project)
	}

	coordinates, diagnostic := mavenCoordinates(metadata)
	if diagnostic != nil {
		return result, false, []Diagnostic{*diagnostic}
	}
	for _, item := range metadata {
		if diagnostic := validateMavenModules(context, item); diagnostic != nil {
			return result, false, []Diagnostic{*diagnostic}
		}
		for _, dependency := range item.dependencies {
			if !validMavenDependency(dependency) {
				return result, false, []Diagnostic{{Path: item.manifest, Message: "Maven dependency coordinates were incomplete or interpolated; Java dependencies were broadened"}}
			}
			owner := coordinates[dependency.groupID+":"+dependency.artifactID]
			if owner == "" || owner == item.directory {
				continue
			}
			entry := result[item.directory]
			if strings.EqualFold(dependency.scope, "test") {
				entry.test = append(entry.test, owner)
			} else {
				entry.main = append(entry.main, owner)
			}
			result[item.directory] = entry
		}
	}
	return result, true, nil
}

func hasUnsupportedMavenFeatures(contents string) bool {
	return strings.Contains(contents, "<profiles") ||
		strings.Contains(contents, "<plugin") ||
		strings.Contains(contents, "<extensions")
}

func mavenMetadataFromProject(directory, manifest string, project mavenXMLProject) mavenMetadata {
	groupID := strings.TrimSpace(project.GroupID)
	if groupID == "" {
		groupID = strings.TrimSpace(project.Parent.GroupID)
	}
	item := mavenMetadata{
		directory: directory, manifest: manifest, groupID: groupID,
		artifactID: strings.TrimSpace(project.ArtifactID),
	}
	for _, child := range project.Modules.Values {
		item.modules = append(item.modules, strings.TrimSpace(child))
	}
	for _, dependency := range project.Dependencies.Values {
		item.dependencies = append(item.dependencies, mavenDependency{
			groupID:    strings.TrimSpace(dependency.GroupID),
			artifactID: strings.TrimSpace(dependency.ArtifactID),
			scope:      strings.TrimSpace(dependency.Scope),
		})
	}
	return item
}

func mavenCoordinates(metadata map[string]mavenMetadata) (map[string]string, *Diagnostic) {
	coordinates := map[string]string{}
	for directory, item := range metadata {
		if !validMavenCoordinate(item) {
			return nil, &Diagnostic{Path: item.manifest, Message: "Maven coordinates were incomplete or interpolated; Java dependencies were broadened"}
		}
		coordinate := item.groupID + ":" + item.artifactID
		if previous := coordinates[coordinate]; previous != "" && previous != directory {
			return nil, &Diagnostic{Path: item.manifest, Message: "duplicate Maven coordinates made local dependency ownership ambiguous"}
		}
		coordinates[coordinate] = directory
	}
	return coordinates, nil
}

func validMavenCoordinate(item mavenMetadata) bool {
	return item.artifactID != "" && item.groupID != "" && !strings.Contains(item.groupID+item.artifactID, "${")
}

func validateMavenModules(context plannerContext, item mavenMetadata) *Diagnostic {
	for _, child := range item.modules {
		if child == "" || strings.Contains(child, "${") {
			return &Diagnostic{Path: item.manifest, Message: "Maven module declaration was dynamic; Java dependencies were broadened"}
		}
		manifest := joinPath(item.directory, child, "pom.xml")
		if !context.fileSet[manifest] {
			return &Diagnostic{Path: item.manifest, Message: "declared Maven module was not present in the workspace; Java dependencies were broadened"}
		}
	}
	return nil
}

func validMavenDependency(dependency mavenDependency) bool {
	coordinates := dependency.groupID + dependency.artifactID
	return dependency.groupID != "" && dependency.artifactID != "" && !strings.Contains(coordinates, "${")
}
