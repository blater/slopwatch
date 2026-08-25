package unitplan

import (
	"strings"

	"github.com/blater/slopwatch/internal/sourcepath"
)

type javaModule struct {
	directory string
	kind      string
	manifest  string
	sets      map[string][]string
}

func javaModules(context plannerContext) (map[string]*javaModule, map[string]bool) {
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
	return modules, moduleDirs
}

func javaSources(
	context plannerContext,
	modules map[string]*javaModule,
	moduleDirs map[string]bool,
) []string {
	var fallback []string
	for _, file := range context.files {
		if !strings.HasSuffix(file, ".java") || sourcepath.IsJavaResource(file) {
			continue
		}
		directory, ok := nearestAncestor(pathDirectory(file), moduleDirs)
		if !ok {
			fallback = append(fallback, file)
			continue
		}
		module := modules[directory]
		sourceSet := javaSourceSet(directory, file)
		module.sets[sourceSet] = append(module.sets[sourceSet], file)
	}
	return fallback
}

func javaMainIDs(modules map[string]*javaModule) map[string]string {
	mainIDs := map[string]string{}
	for _, module := range modules {
		if len(module.sets["main"]) > 0 {
			mainIDs[module.directory] = javaUnitID(module, "main")
		}
	}
	return mainIDs
}

func javaSourceDependencies(
	module *javaModule,
	sourceSet string,
	mainIDs map[string]string,
	localDependencies map[string]javaDependencies,
	narrow bool,
) []string {
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
	return dependencies
}

func javaCapabilities(sourceSet string) []Capability {
	capabilities := []Capability{CapabilitySyntax, CapabilityTypes, CapabilityDependencies}
	if strings.Contains(strings.ToLower(sourceSet), "test") {
		capabilities = append(capabilities, CapabilityTests)
	}
	return capabilities
}

func javaModuleUnits(
	module *javaModule,
	mainIDs map[string]string,
	localDependencies map[string]javaDependencies,
	narrow bool,
	configInputs map[string][]string,
) []Unit {
	var units []Unit
	for sourceSet, sources := range module.sets {
		units = append(units, Unit{
			ID: javaUnitID(module, sourceSet), Language: LanguageJava, Mode: ModeProject,
			Capabilities: javaCapabilities(sourceSet), Sources: sources,
			ConfigInputs:       configInputs[module.directory],
			DirectDependencies: javaSourceDependencies(module, sourceSet, mainIDs, localDependencies, narrow),
			Conservative:       !narrow,
		})
	}
	return units
}

func javaUnits(
	modules map[string]*javaModule,
	mainIDs map[string]string,
	localDependencies map[string]javaDependencies,
	narrow bool,
	configInputs map[string][]string,
	fallback []string,
	fallbackConfigs []string,
) []Unit {
	var units []Unit
	for _, module := range modules {
		units = append(units, javaModuleUnits(module, mainIDs, localDependencies, narrow, configInputs)...)
	}
	if len(fallback) > 0 {
		units = append(units, Unit{
			ID: "java:workspace-fallback", Language: LanguageJava, Mode: ModeProject,
			Capabilities: []Capability{CapabilitySyntax, CapabilityTypes, CapabilityDependencies},
			Sources:      fallback, ConfigInputs: fallbackConfigs, Conservative: true,
		})
	}
	return units
}

func planJava(context plannerContext, _ Options) ([]Unit, []Diagnostic) {
	modules, moduleDirs := javaModules(context)
	addGradleDeclaredModules(context, modules, moduleDirs)
	configInputs := javaConfigInputsByModule(context, modules)
	fallback := javaSources(context, modules, moduleDirs)
	mainIDs := javaMainIDs(modules)
	localDependencies, narrow, diagnostics := javaDependencyGraph(context, modules)
	if len(fallback) > 0 {
		narrow = false
		diagnostics = append(diagnostics, Diagnostic{Path: fallback[0], Message: "Java source outside a recognized build broadened the workspace dependency graph"})
	}
	units := javaUnits(modules, mainIDs, localDependencies, narrow, configInputs, fallback, javaWorkspaceConfigs(context))
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
