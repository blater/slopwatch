package unitplan

import (
	"encoding/xml"
	"fmt"
	"regexp"
	"strings"
)

func javaDependencyGraph(context plannerContext, modules map[string]*javaModule) (map[string]javaDependencies, bool, []Diagnostic) {
	result := make(map[string]javaDependencies, len(modules))
	mavenModules := map[string]*javaModule{}
	gradleModules := map[string]*javaModule{}
	for directory, module := range modules {
		if module.kind == "maven" {
			mavenModules[directory] = module
		} else {
			gradleModules[directory] = module
		}
	}
	var diagnostics []Diagnostic
	if len(mavenModules) > 0 && len(gradleModules) > 0 {
		return result, false, []Diagnostic{{Message: "mixed Maven and Gradle workspace requires conservative Java dependencies"}}
	}
	mavenGraph, mavenOK, mavenDiagnostics := mavenDependencyGraph(context, mavenModules)
	gradleGraph, gradleOK, gradleDiagnostics := gradleDependencyGraph(context, gradleModules)
	diagnostics = append(diagnostics, mavenDiagnostics...)
	diagnostics = append(diagnostics, gradleDiagnostics...)
	if !mavenOK || !gradleOK {
		return result, false, diagnostics
	}
	for directory, dependencies := range mavenGraph {
		result[directory] = dependencies
	}
	for directory, dependencies := range gradleGraph {
		result[directory] = dependencies
	}
	return result, true, diagnostics
}

type mavenXMLProject struct {
	GroupID    string `xml:"groupId"`
	ArtifactID string `xml:"artifactId"`
	Parent     struct {
		GroupID      string `xml:"groupId"`
		ArtifactID   string `xml:"artifactId"`
		RelativePath string `xml:"relativePath"`
	} `xml:"parent"`
	Modules struct {
		Values []string `xml:"module"`
	} `xml:"modules"`
	Dependencies struct {
		Values []struct {
			GroupID    string `xml:"groupId"`
			ArtifactID string `xml:"artifactId"`
			Scope      string `xml:"scope"`
		} `xml:"dependency"`
	} `xml:"dependencies"`
}

type mavenMetadata struct {
	directory    string
	manifest     string
	groupID      string
	artifactID   string
	modules      []string
	dependencies []mavenDependency
}

type mavenDependency struct {
	groupID    string
	artifactID string
	scope      string
}

func mavenDependencyGraph(context plannerContext, modules map[string]*javaModule) (map[string]javaDependencies, bool, []Diagnostic) {
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
		contents := string(data)
		if strings.Contains(contents, "<profiles") || strings.Contains(contents, "<plugin") || strings.Contains(contents, "<extensions") {
			return result, false, []Diagnostic{{Path: module.manifest, Message: "Maven profiles or build extensions require conservative Java dependencies"}}
		}
		groupID := strings.TrimSpace(project.GroupID)
		if groupID == "" {
			groupID = strings.TrimSpace(project.Parent.GroupID)
		}
		item := mavenMetadata{directory: directory, manifest: module.manifest, groupID: groupID, artifactID: strings.TrimSpace(project.ArtifactID)}
		for _, child := range project.Modules.Values {
			item.modules = append(item.modules, strings.TrimSpace(child))
		}
		for _, dependency := range project.Dependencies.Values {
			item.dependencies = append(item.dependencies, mavenDependency{
				groupID: strings.TrimSpace(dependency.GroupID), artifactID: strings.TrimSpace(dependency.ArtifactID), scope: strings.TrimSpace(dependency.Scope),
			})
		}
		metadata[directory] = item
	}

	coordinates := map[string]string{}
	for directory, item := range metadata {
		if item.artifactID == "" || item.groupID == "" || strings.Contains(item.groupID+item.artifactID, "${") {
			return result, false, []Diagnostic{{Path: item.manifest, Message: "Maven coordinates were incomplete or interpolated; Java dependencies were broadened"}}
		}
		coordinate := item.groupID + ":" + item.artifactID
		if previous := coordinates[coordinate]; previous != "" && previous != directory {
			return result, false, []Diagnostic{{Path: item.manifest, Message: "duplicate Maven coordinates made local dependency ownership ambiguous"}}
		}
		coordinates[coordinate] = directory
	}
	for _, item := range metadata {
		for _, child := range item.modules {
			if child == "" || strings.Contains(child, "${") {
				return result, false, []Diagnostic{{Path: item.manifest, Message: "Maven module declaration was dynamic; Java dependencies were broadened"}}
			}
			manifest := joinPath(item.directory, child, "pom.xml")
			if !context.fileSet[manifest] {
				return result, false, []Diagnostic{{Path: item.manifest, Message: "declared Maven module was not present in the workspace; Java dependencies were broadened"}}
			}
		}
		for _, dependency := range item.dependencies {
			if dependency.groupID == "" || dependency.artifactID == "" || strings.Contains(dependency.groupID+dependency.artifactID, "${") {
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

func javaMetadataDiagnostic(path string, err error) []Diagnostic {
	return []Diagnostic{{Path: path, Message: fmt.Sprintf("Java build metadata could not be parsed (%v); dependencies were broadened", err)}}
}

type gradleSettings struct {
	root       string
	projects   map[string]string
	confident  bool
	diagnostic *Diagnostic
}

var (
	quotedValue           = regexp.MustCompile(`["']([^"']+)["']`)
	gradleProjectCall     = regexp.MustCompile(`project\s*\(\s*(?:path\s*[:=]\s*)?["'](:[^"']*)["']\s*\)`)
	gradleAnyProject      = regexp.MustCompile(`project\s*\(`)
	gradleProjectDir      = regexp.MustCompile(`project\s*\(\s*["'](:[^"']+)["']\s*\)\.projectDir\s*=\s*(?:file|File)\s*\([^"']*["']([^"']+)["']\s*\)`)
	gradleAnyProjectDir   = regexp.MustCompile(`\.projectDir\s*=`)
	gradleTypeSafeProject = regexp.MustCompile(`\bprojects\s*\.`)
)

func addGradleDeclaredModules(context plannerContext, modules map[string]*javaModule, moduleDirs map[string]bool) {
	for _, settings := range parseAllGradleSettings(context) {
		if !settings.confident {
			continue
		}
		for _, directory := range settings.projects {
			if modules[directory] != nil {
				continue
			}
			manifest := joinPath(directory, "build.gradle")
			if !context.fileSet[manifest] {
				manifest = joinPath(directory, "build.gradle.kts")
			}
			if !context.fileSet[manifest] {
				manifest = gradleRootManifest(context, settings.root)
			}
			modules[directory] = &javaModule{directory: directory, kind: "gradle", manifest: manifest, sets: map[string][]string{}}
			moduleDirs[directory] = true
		}
	}
}

func gradleDependencyGraph(context plannerContext, modules map[string]*javaModule) (map[string]javaDependencies, bool, []Diagnostic) {
	result := make(map[string]javaDependencies, len(modules))
	if len(modules) < 2 {
		return result, true, nil
	}
	settingsValues := parseAllGradleSettings(context)
	for _, settings := range settingsValues {
		if !settings.confident {
			return result, false, []Diagnostic{*settings.diagnostic}
		}
	}
	for directory, module := range modules {
		settings, ok := nearestGradleSettings(directory, settingsValues)
		if !ok {
			return result, false, []Diagnostic{{Path: module.manifest, Message: "Gradle module had no statically parseable settings owner; Java dependencies were broadened"}}
		}
		projectByDirectory := map[string]string{}
		for project, projectDirectory := range settings.projects {
			projectByDirectory[projectDirectory] = project
		}
		if projectByDirectory[directory] == "" {
			return result, false, []Diagnostic{{Path: module.manifest, Message: "Gradle module was absent from settings includes; Java dependencies were broadened"}}
		}
		manifest := joinPath(directory, "build.gradle")
		if !context.fileSet[manifest] {
			manifest = joinPath(directory, "build.gradle.kts")
		}
		if !context.fileSet[manifest] {
			continue
		}
		data, err := context.read(manifest)
		if err != nil {
			return result, false, javaMetadataDiagnostic(manifest, err)
		}
		entry := javaDependencies{}
		for _, line := range strings.Split(stripGradleComments(string(data)), "\n") {
			if gradleTypeSafeProject.MatchString(line) {
				return result, false, []Diagnostic{{Path: manifest, Message: "Gradle type-safe project accessor broadened Java dependencies"}}
			}
			matches := gradleProjectCall.FindAllStringSubmatch(line, -1)
			if len(matches) != len(gradleAnyProject.FindAllString(line, -1)) {
				return result, false, []Diagnostic{{Path: manifest, Message: "dynamic Gradle project dependency broadened Java dependencies"}}
			}
			for _, match := range matches {
				owner := settings.projects[match[1]]
				if owner == "" {
					return result, false, []Diagnostic{{Path: manifest, Message: "Gradle project dependency did not resolve in settings; Java dependencies were broadened"}}
				}
				if strings.Contains(strings.ToLower(line[:strings.Index(line, match[0])]), "test") {
					entry.test = append(entry.test, owner)
				} else {
					entry.main = append(entry.main, owner)
				}
			}
		}
		result[directory] = entry
	}
	return result, true, nil
}

func parseAllGradleSettings(context plannerContext) []gradleSettings {
	var result []gradleSettings
	for _, file := range context.files {
		if lastPart(file) != "settings.gradle" && lastPart(file) != "settings.gradle.kts" {
			continue
		}
		root := pathDirectory(file)
		settings := gradleSettings{root: root, projects: map[string]string{":": root}, confident: true}
		data, err := context.read(file)
		if err != nil {
			settings.confident = false
			settings.diagnostic = &Diagnostic{Path: file, Message: "Gradle settings could not be read; Java dependencies were broadened"}
			result = append(result, settings)
			continue
		}
		contents := stripGradleComments(string(data))
		for _, line := range strings.Split(contents, "\n") {
			trimmed := strings.TrimSpace(line)
			if isGradleInclude(trimmed) {
				values := quotedValue.FindAllStringSubmatch(trimmed, -1)
				if len(values) == 0 || !staticGradleInclude(trimmed) {
					settings.confident = false
					settings.diagnostic = &Diagnostic{Path: file, Message: "dynamic Gradle include broadened Java dependencies"}
					break
				}
				for _, value := range values {
					project := value[1]
					if !strings.HasPrefix(project, ":") {
						project = ":" + project
					}
					settings.projects[project] = joinPath(root, strings.ReplaceAll(strings.TrimPrefix(project, ":"), ":", "/"))
				}
			}
			if strings.Contains(trimmed, "includeBuild") {
				settings.confident = false
				settings.diagnostic = &Diagnostic{Path: file, Message: "Gradle composite build requires conservative Java dependencies"}
				break
			}
		}
		if settings.confident {
			matches := gradleProjectDir.FindAllStringSubmatch(contents, -1)
			if len(matches) != len(gradleAnyProjectDir.FindAllString(contents, -1)) {
				settings.confident = false
				settings.diagnostic = &Diagnostic{Path: file, Message: "dynamic Gradle projectDir broadened Java dependencies"}
			}
			for _, match := range matches {
				if settings.projects[match[1]] == "" || strings.Contains(match[2], "$") {
					settings.confident = false
					settings.diagnostic = &Diagnostic{Path: file, Message: "dynamic Gradle projectDir broadened Java dependencies"}
					break
				}
				settings.projects[match[1]] = joinPath(root, match[2])
			}
		}
		result = append(result, settings)
	}
	return result
}

func isGradleInclude(line string) bool {
	if !strings.HasPrefix(line, "include") || strings.HasPrefix(line, "includeBuild") || strings.HasPrefix(line, "includeFlat") {
		return false
	}
	return len(line) == len("include") || line[len("include")] == '(' || line[len("include")] == ' ' || line[len("include")] == '\t'
}

func staticGradleInclude(line string) bool {
	withoutValues := quotedValue.ReplaceAllString(line, "")
	withoutValues = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(withoutValues), "include"))
	for _, char := range withoutValues {
		if char != '(' && char != ')' && char != ',' && char != ' ' && char != '\t' {
			return false
		}
	}
	return true
}

func nearestGradleSettings(directory string, settings []gradleSettings) (gradleSettings, bool) {
	best := gradleSettings{}
	found := false
	for _, candidate := range settings {
		if pathWithin(directory, candidate.root) && (!found || len(candidate.root) > len(best.root)) {
			best, found = candidate, true
		}
	}
	return best, found
}

func gradleRootManifest(context plannerContext, root string) string {
	for _, name := range []string{"build.gradle", "build.gradle.kts", "settings.gradle", "settings.gradle.kts"} {
		candidate := joinPath(root, name)
		if context.fileSet[candidate] {
			return candidate
		}
	}
	return joinPath(root, "settings.gradle")
}

func stripGradleComments(contents string) string {
	var result []string
	for _, line := range strings.Split(contents, "\n") {
		result = append(result, strings.SplitN(line, "//", 2)[0])
	}
	return strings.Join(result, "\n")
}
