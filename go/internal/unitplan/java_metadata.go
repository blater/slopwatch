package unitplan

import (
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
	return buildMavenDependencyGraph(context, modules)
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
	declareGradleModules(context, modules, moduleDirs)
}

func gradleDependencyGraph(context plannerContext, modules map[string]*javaModule) (map[string]javaDependencies, bool, []Diagnostic) {
	return buildGradleDependencyGraph(context, modules)
}

func parseAllGradleSettings(context plannerContext) []gradleSettings {
	return parseGradleSettings(context)
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
