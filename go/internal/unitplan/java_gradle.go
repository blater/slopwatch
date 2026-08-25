package unitplan

import "strings"

func declareGradleModules(context plannerContext, modules map[string]*javaModule, moduleDirs map[string]bool) {
	for _, settings := range parseGradleSettings(context) {
		if !settings.confident {
			continue
		}
		for _, directory := range settings.projects {
			if modules[directory] != nil {
				continue
			}
			manifest := gradleModuleManifest(context, settings.root, directory)
			modules[directory] = &javaModule{directory: directory, kind: "gradle", manifest: manifest, sets: map[string][]string{}}
			moduleDirs[directory] = true
		}
	}
}

func gradleModuleManifest(context plannerContext, root, directory string) string {
	for _, name := range []string{"build.gradle", "build.gradle.kts"} {
		candidate := joinPath(directory, name)
		if context.fileSet[candidate] {
			return candidate
		}
	}
	return gradleRootManifest(context, root)
}

func buildGradleDependencyGraph(context plannerContext, modules map[string]*javaModule) (map[string]javaDependencies, bool, []Diagnostic) {
	result := make(map[string]javaDependencies, len(modules))
	if len(modules) < 2 {
		return result, true, nil
	}
	settingsValues := parseGradleSettings(context)
	if diagnostic := firstGradleSettingsDiagnostic(settingsValues); diagnostic != nil {
		return result, false, []Diagnostic{*diagnostic}
	}
	for directory, module := range modules {
		settings, ok := nearestGradleSettings(directory, settingsValues)
		if !ok {
			return result, false, []Diagnostic{{Path: module.manifest, Message: "Gradle module had no statically parseable settings owner; Java dependencies were broadened"}}
		}
		entry, diagnostic := gradleModuleDependencies(context, directory, module, settings)
		if diagnostic != nil {
			return result, false, []Diagnostic{*diagnostic}
		}
		result[directory] = entry
	}
	return result, true, nil
}

func firstGradleSettingsDiagnostic(settings []gradleSettings) *Diagnostic {
	for _, item := range settings {
		if !item.confident {
			return item.diagnostic
		}
	}
	return nil
}

func gradleModuleDependencies(context plannerContext, directory string, module *javaModule, settings gradleSettings) (javaDependencies, *Diagnostic) {
	projectByDirectory := invertGradleProjects(settings.projects)
	if projectByDirectory[directory] == "" {
		return javaDependencies{}, &Diagnostic{Path: module.manifest, Message: "Gradle module was absent from settings includes; Java dependencies were broadened"}
	}
	manifest := gradleModuleBuildManifest(context, directory)
	if manifest == "" {
		return javaDependencies{}, nil
	}
	data, err := context.read(manifest)
	if err != nil {
		return javaDependencies{}, &Diagnostic{Path: manifest, Message: javaMetadataDiagnostic(manifest, err)[0].Message}
	}
	return parseGradleDependencies(manifest, string(data), settings.projects)
}

func invertGradleProjects(projects map[string]string) map[string]string {
	result := make(map[string]string, len(projects))
	for project, directory := range projects {
		result[directory] = project
	}
	return result
}

func gradleModuleBuildManifest(context plannerContext, directory string) string {
	for _, name := range []string{"build.gradle", "build.gradle.kts"} {
		manifest := joinPath(directory, name)
		if context.fileSet[manifest] {
			return manifest
		}
	}
	return ""
}

func parseGradleDependencies(manifest, contents string, projects map[string]string) (javaDependencies, *Diagnostic) {
	entry := javaDependencies{}
	for _, line := range strings.Split(stripGradleComments(contents), "\n") {
		if gradleTypeSafeProject.MatchString(line) {
			return javaDependencies{}, &Diagnostic{Path: manifest, Message: "Gradle type-safe project accessor broadened Java dependencies"}
		}
		matches := gradleProjectCall.FindAllStringSubmatch(line, -1)
		if len(matches) != len(gradleAnyProject.FindAllString(line, -1)) {
			return javaDependencies{}, &Diagnostic{Path: manifest, Message: "dynamic Gradle project dependency broadened Java dependencies"}
		}
		for _, match := range matches {
			owner := projects[match[1]]
			if owner == "" {
				return javaDependencies{}, &Diagnostic{Path: manifest, Message: "Gradle project dependency did not resolve in settings; Java dependencies were broadened"}
			}
			addGradleDependency(&entry, owner, line, match[0])
		}
	}
	return entry, nil
}

func addGradleDependency(entry *javaDependencies, owner, line, match string) {
	prefix := line[:strings.Index(line, match)]
	if strings.Contains(strings.ToLower(prefix), "test") {
		entry.test = append(entry.test, owner)
	} else {
		entry.main = append(entry.main, owner)
	}
}

func parseGradleSettings(context plannerContext) []gradleSettings {
	var result []gradleSettings
	for _, file := range context.files {
		if !isGradleSettings(file) {
			continue
		}
		settings := newGradleSettings(file)
		data, err := context.read(file)
		if err != nil {
			markGradleSettingsFailure(&settings, file, "Gradle settings could not be read; Java dependencies were broadened")
			result = append(result, settings)
			continue
		}
		parseGradleSettingsContents(&settings, stripGradleComments(string(data)), file)
		result = append(result, settings)
	}
	return result
}

func isGradleSettings(file string) bool {
	return lastPart(file) == "settings.gradle" || lastPart(file) == "settings.gradle.kts"
}

func newGradleSettings(file string) gradleSettings {
	root := pathDirectory(file)
	return gradleSettings{root: root, projects: map[string]string{":": root}, confident: true}
}

func parseGradleSettingsContents(settings *gradleSettings, contents, file string) {
	for _, line := range strings.Split(contents, "\n") {
		trimmed := strings.TrimSpace(line)
		if isGradleInclude(trimmed) && !parseGradleInclude(settings, trimmed, file) {
			return
		}
		if strings.Contains(trimmed, "includeBuild") {
			markGradleSettingsFailure(settings, file, "Gradle composite build requires conservative Java dependencies")
			return
		}
	}
	if settings.confident {
		parseGradleProjectDirectories(settings, contents, file)
	}
}

func parseGradleInclude(settings *gradleSettings, line, file string) bool {
	values := quotedValue.FindAllStringSubmatch(line, -1)
	if len(values) == 0 || !staticGradleInclude(line) {
		markGradleSettingsFailure(settings, file, "dynamic Gradle include broadened Java dependencies")
		return false
	}
	for _, value := range values {
		project := value[1]
		if !strings.HasPrefix(project, ":") {
			project = ":" + project
		}
		settings.projects[project] = joinPath(settings.root, strings.ReplaceAll(strings.TrimPrefix(project, ":"), ":", "/"))
	}
	return true
}

func parseGradleProjectDirectories(settings *gradleSettings, contents, file string) {
	matches := gradleProjectDir.FindAllStringSubmatch(contents, -1)
	if len(matches) != len(gradleAnyProjectDir.FindAllString(contents, -1)) {
		markGradleSettingsFailure(settings, file, "dynamic Gradle projectDir broadened Java dependencies")
		return
	}
	for _, match := range matches {
		if settings.projects[match[1]] == "" || strings.Contains(match[2], "$") {
			markGradleSettingsFailure(settings, file, "dynamic Gradle projectDir broadened Java dependencies")
			return
		}
		settings.projects[match[1]] = joinPath(settings.root, match[2])
	}
}

func markGradleSettingsFailure(settings *gradleSettings, path, message string) {
	settings.confident = false
	settings.diagnostic = &Diagnostic{Path: path, Message: message}
}
