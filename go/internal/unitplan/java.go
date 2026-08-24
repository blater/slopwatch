package unitplan

import "strings"

type javaModule struct {
	directory string
	kind      string
	manifest  string
	sets      map[string][]string
}

func planJava(context plannerContext, _ Options) ([]Unit, []Diagnostic) {
	modules := map[string]*javaModule{}
	moduleDirs := map[string]bool{}
	for _, file := range context.files {
		base := lastPart(file)
		kind := ""
		switch base {
		case "pom.xml":
			kind = "maven"
		case "build.gradle", "build.gradle.kts":
			kind = "gradle"
		}
		if kind != "" {
			directory := pathDirectory(file)
			// Maven wins when both ecosystems leave files in one directory.
			if existing := modules[directory]; existing == nil || kind == "maven" {
				modules[directory] = &javaModule{directory: directory, kind: kind, manifest: file, sets: map[string][]string{}}
			}
			moduleDirs[directory] = true
		}
	}
	addGradleDeclaredModules(context, modules, moduleDirs)
	configInputs := javaConfigInputsByModule(context, modules)

	var fallback []string
	for _, file := range context.files {
		if !strings.HasSuffix(file, ".java") {
			continue
		}
		directory, ok := nearestAncestor(pathDirectory(file), moduleDirs)
		if !ok {
			fallback = append(fallback, file)
			continue
		}
		module := modules[directory]
		module.sets[javaSourceSet(directory, file)] = append(module.sets[javaSourceSet(directory, file)], file)
	}

	var units []Unit
	mainIDs := map[string]string{}
	for _, module := range modules {
		if len(module.sets["main"]) > 0 {
			mainIDs[module.directory] = javaUnitID(module, "main")
		}
	}
	localDependencies, narrow, diagnostics := javaDependencyGraph(context, modules)
	if len(fallback) > 0 {
		narrow = false
		diagnostics = append(diagnostics, Diagnostic{Path: fallback[0], Message: "Java source outside a recognized build broadened the workspace dependency graph"})
	}
	for _, module := range modules {
		for sourceSet, sources := range module.sets {
			dependencies := []string{}
			if sourceSet != "main" && mainIDs[module.directory] != "" {
				dependencies = append(dependencies, mainIDs[module.directory])
			}
			if narrow {
				for _, directory := range localDependencies[module.directory].forSourceSet(sourceSet) {
					if id := mainIDs[directory]; id != "" {
						dependencies = append(dependencies, id)
					}
				}
			}
			capabilities := []Capability{CapabilitySyntax, CapabilityTypes, CapabilityDependencies}
			if strings.Contains(strings.ToLower(sourceSet), "test") {
				capabilities = append(capabilities, CapabilityTests)
			}
			units = append(units, Unit{
				ID: javaUnitID(module, sourceSet), Language: LanguageJava, Mode: ModeProject,
				Capabilities: capabilities, Sources: sources,
				ConfigInputs: configInputs[module.directory], DirectDependencies: dependencies,
				Conservative: !narrow,
			})
		}
	}
	if len(fallback) > 0 {
		units = append(units, Unit{
			ID: "java:workspace-fallback", Language: LanguageJava, Mode: ModeProject,
			Capabilities: []Capability{CapabilitySyntax, CapabilityTypes, CapabilityDependencies},
			Sources:      fallback, ConfigInputs: javaWorkspaceConfigs(context),
			Conservative: true,
		})
	}
	return units, diagnostics
}

type javaDependencies struct {
	main []string
	test []string
}

func (dependencies javaDependencies) forSourceSet(sourceSet string) []string {
	if strings.Contains(strings.ToLower(sourceSet), "test") {
		return append(append([]string(nil), dependencies.main...), dependencies.test...)
	}
	return dependencies.main
}

func javaUnitID(module *javaModule, sourceSet string) string {
	return "java:" + module.kind + ":" + relativeIDPath(module.directory) + ":" + sourceSet
}

func javaSourceSet(moduleDirectory, file string) string {
	relative := strings.TrimPrefix(strings.TrimPrefix(file, moduleDirectory), "/")
	parts := strings.Split(relative, "/")
	if len(parts) >= 3 && parts[0] == "src" && parts[2] == "java" {
		return parts[1]
	}
	if strings.Contains(strings.ToLower(lastPart(file)), "test") || hasPathSegment(strings.ToLower(file), "test") {
		return "test"
	}
	return "main"
}

func javaConfigInputsByModule(context plannerContext, modules map[string]*javaModule) map[string][]string {
	byOwner := map[string][]string{}
	for _, file := range context.files {
		if !javaConfigName(lastPart(file)) {
			continue
		}
		owner := pathDirectory(file)
		for _, buildDirectory := range []string{".mvn", "gradle"} {
			marker := "/" + buildDirectory + "/"
			if index := strings.Index(file, marker); index >= 0 {
				owner = file[:index]
			} else if strings.HasPrefix(file, buildDirectory+"/") {
				owner = "."
			}
		}
		byOwner[owner] = append(byOwner[owner], file)
	}
	result := make(map[string][]string, len(modules))
	for directory, module := range modules {
		for current := directory; ; current = pathDirectory(current) {
			result[directory] = append(result[directory], byOwner[current]...)
			if current == "." {
				break
			}
		}
		result[directory] = append(result[directory], module.manifest)
	}
	return result
}

func javaWorkspaceConfigs(context plannerContext) []string {
	var result []string
	for _, file := range context.files {
		if javaConfigName(lastPart(file)) {
			result = append(result, file)
		}
	}
	return result
}

func javaConfigName(name string) bool {
	switch name {
	case "pom.xml", "build.gradle", "build.gradle.kts", "settings.gradle", "settings.gradle.kts",
		"gradle.properties", "gradle.lockfile", "maven.config", "jvm.config":
		return true
	default:
		return false
	}
}
