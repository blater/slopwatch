package unitplan

func buildTypeScriptPlan(context plannerContext, options Options) ([]Unit, []Diagnostic) {
	sources, declarations, configs, diagnostics := collectTSWorkspace(context)
	if len(sources) == 0 {
		return nil, diagnostics
	}
	if options.TypeScriptMode == TypeScriptSyntax {
		return typeScriptSyntaxUnits(context, sources, declarations), diagnostics
	}
	units, typedDiagnostics, configDirs := typedTypeScriptUnits(context, sources, declarations, configs)
	diagnostics = append(diagnostics, typedDiagnostics...)
	looseSources := looseTypeScriptSources(sources, configDirs)
	return append(units, typeScriptSyntaxUnits(context, looseSources, looseTypeScriptSources(declarations, configDirs))...), diagnostics
}

func collectTSWorkspace(context plannerContext) ([]string, []string, map[string]*tsConfig, []Diagnostic) {
	var sources []string
	var declarations []string
	configs := map[string]*tsConfig{}
	var diagnostics []Diagnostic
	for _, file := range context.files {
		if isTypeScriptSource(file) {
			if isTypeScriptDeclaration(file) {
				declarations = append(declarations, file)
			} else {
				sources = append(sources, file)
			}
		}
		if !isTSConfig(file) {
			continue
		}
		config, err := readTSConfig(context, file)
		if err != nil {
			diagnostics = append(diagnostics, Diagnostic{file, "configuration could not be parsed; project ownership was broadened"})
			config = &tsConfig{path: file, directory: pathDirectory(file), project: true, uncertain: true}
		}
		configs[file] = config
	}
	normalizeTSProjects(context, configs)
	return sources, declarations, configs, diagnostics
}

func normalizeTSProjects(context plannerContext, configs map[string]*tsConfig) {
	for _, config := range configs {
		if config.extends == "" {
			continue
		}
		resolved := resolveTSConfigPath(context, config.directory, config.extends)
		base := configs[resolved]
		if base != nil && lastPart(base.path) != "tsconfig.json" && !base.hasRoots {
			base.project = false
		}
	}
}

func typedTypeScriptUnits(context plannerContext, sources, declarations []string, configs map[string]*tsConfig) ([]Unit, []Diagnostic, map[string]bool) {
	unitIDs, projectsByDirectory := typeScriptProjects(configs)
	ownedByProject := assignTypeScriptSources(sources, projectsByDirectory)
	declarationsByProject := assignTypeScriptSources(declarations, projectsByDirectory)
	packageInputs := typeScriptPackageInputsByProject(context, configs)
	units, diagnostics := buildTypeScriptUnits(context, configs, unitIDs, ownedByProject, declarationsByProject, packageInputs)
	markOverlappingTypeScriptUnits(units)
	return units, diagnostics, typeScriptProjectDirectories(configs)
}

func typeScriptProjects(configs map[string]*tsConfig) (map[string]string, map[string][]string) {
	unitIDs := map[string]string{}
	projectsByDirectory := map[string][]string{}
	for path, config := range configs {
		if config.project {
			unitIDs[path] = "typescript:typed:" + path
			projectsByDirectory[config.directory] = append(projectsByDirectory[config.directory], path)
		}
	}
	return unitIDs, projectsByDirectory
}

func assignTypeScriptSources(sources []string, projectsByDirectory map[string][]string) map[string][]string {
	owned := map[string][]string{}
	for _, source := range sources {
		for directory := pathDirectory(source); ; directory = pathDirectory(directory) {
			for _, project := range projectsByDirectory[directory] {
				owned[project] = append(owned[project], source)
			}
			if directory == "." {
				break
			}
		}
	}
	return owned
}

func buildTypeScriptUnits(context plannerContext, configs map[string]*tsConfig, unitIDs map[string]string, ownedByProject, declarationsByProject map[string][]string, packageInputs map[string][]string) ([]Unit, []Diagnostic) {
	var units []Unit
	var diagnostics []Diagnostic
	for path, config := range configs {
		if !config.project || (len(ownedByProject[path]) == 0 && len(declarationsByProject[path]) == 0) {
			continue
		}
		configInputs, extendDiagnostic := tsConfigInputs(context, config, configs, packageInputs[path])
		if extendDiagnostic != nil {
			diagnostics = append(diagnostics, *extendDiagnostic)
		}
		dependencies := typeScriptDependencies(context, config, unitIDs)
		units = append(units, typeScriptUnit(unitIDs[path], ownedByProject[path], declarationsByProject[path], configInputs, dependencies, config.uncertain || extendDiagnostic != nil))
	}
	return units, diagnostics
}

func typeScriptUnit(id string, sources, contextSources, configInputs, dependencies []string, conservative bool) Unit {
	return Unit{
		ID: id, Language: LanguageTypeScript, Mode: ModeTyped,
		Capabilities: []Capability{CapabilitySyntax, CapabilityTypes, CapabilityDependencies},
		Sources:      sources, ContextSources: contextSources, ConfigInputs: configInputs, DirectDependencies: dependencies,
		Conservative: conservative,
	}
}

func typeScriptDependencies(context plannerContext, config *tsConfig, unitIDs map[string]string) []string {
	var dependencies []string
	for _, reference := range config.references {
		resolved := resolveTSConfigPath(context, config.directory, reference)
		if unitIDs[resolved] != "" {
			dependencies = append(dependencies, unitIDs[resolved])
		}
	}
	return dependencies
}

func markOverlappingTypeScriptUnits(units []Unit) {
	owners := map[string][]int{}
	for index, unit := range units {
		for _, source := range unit.Sources {
			owners[source] = append(owners[source], index)
		}
	}
	for _, candidates := range owners {
		if len(candidates) < 2 {
			continue
		}
		for _, index := range candidates {
			units[index].Conservative = true
		}
	}
}

func typeScriptProjectDirectories(configs map[string]*tsConfig) map[string]bool {
	result := map[string]bool{}
	for _, config := range configs {
		if config.project {
			result[config.directory] = true
		}
	}
	return result
}

func looseTypeScriptSources(sources []string, configDirs map[string]bool) []string {
	var loose []string
	for _, source := range sources {
		if _, ok := nearestAncestor(pathDirectory(source), configDirs); !ok {
			loose = append(loose, source)
		}
	}
	return loose
}
