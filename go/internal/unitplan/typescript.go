package unitplan

import (
	"encoding/json"
	"regexp"
	"strings"
)

type tsConfig struct {
	path       string
	directory  string
	extends    string
	references []string
	project    bool
	hasRoots   bool
	uncertain  bool
}

func planTypeScript(context plannerContext, options Options) ([]Unit, []Diagnostic) {
	var sources []string
	configs := map[string]*tsConfig{}
	configDirs := map[string]bool{}
	var diagnostics []Diagnostic
	for _, file := range context.files {
		if isTypeScriptSource(file) {
			sources = append(sources, file)
		}
		if isTSConfig(file) {
			config, err := readTSConfig(context, file)
			if err != nil {
				diagnostics = append(diagnostics, Diagnostic{file, "configuration could not be parsed; project ownership was broadened"})
				config = &tsConfig{path: file, directory: pathDirectory(file), project: true, uncertain: true}
			}
			configs[file] = config
			if config.project {
				configDirs[config.directory] = true
			}
		}
	}
	for _, config := range configs {
		if config.extends == "" {
			continue
		}
		resolved := resolveTSConfigPath(context, config.directory, config.extends)
		if base := configs[resolved]; base != nil && lastPart(base.path) != "tsconfig.json" && !base.hasRoots {
			base.project = false
		}
	}
	configDirs = map[string]bool{}
	for _, config := range configs {
		if config.project {
			configDirs[config.directory] = true
		}
	}
	if len(sources) == 0 {
		return nil, diagnostics
	}
	if options.TypeScriptMode == TypeScriptSyntax {
		return typeScriptSyntaxUnits(context, sources), diagnostics
	}

	var units []Unit
	unitIDs := map[string]string{}
	projectsByDirectory := map[string][]string{}
	for path, config := range configs {
		if config.project {
			unitIDs[path] = "typescript:typed:" + path
			projectsByDirectory[config.directory] = append(projectsByDirectory[config.directory], path)
		}
	}
	ownedByProject := make(map[string][]string, len(unitIDs))
	packageInputsByProject := typeScriptPackageInputsByProject(context, configs)
	for _, source := range sources {
		for directory := pathDirectory(source); ; directory = pathDirectory(directory) {
			for _, project := range projectsByDirectory[directory] {
				ownedByProject[project] = append(ownedByProject[project], source)
			}
			if directory == "." {
				break
			}
		}
	}
	for path, config := range configs {
		if !config.project {
			continue
		}
		// Including nested-project files is intentional: tsconfig include and
		// path aliases are difficult to prove without the compiler. Build that
		// ownership by walking source ancestors once instead of scanning every
		// source again for every project.
		owned := ownedByProject[path]
		if len(owned) == 0 {
			continue
		}
		configInputs, extendDiagnostic := tsConfigInputs(context, config, configs, packageInputsByProject[path])
		if extendDiagnostic != nil {
			diagnostics = append(diagnostics, *extendDiagnostic)
		}
		dependencies := []string{}
		for _, reference := range config.references {
			resolved := resolveTSConfigPath(context, config.directory, reference)
			if unitIDs[resolved] != "" {
				dependencies = append(dependencies, unitIDs[resolved])
			}
		}
		units = append(units, Unit{
			ID: unitIDs[path], Language: LanguageTypeScript, Mode: ModeTyped,
			Capabilities: []Capability{CapabilitySyntax, CapabilityTypes, CapabilityDependencies},
			Sources:      owned, ConfigInputs: configInputs, DirectDependencies: dependencies,
			Conservative: config.uncertain || extendDiagnostic != nil,
		})
	}
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
			// Overlapping tsconfig ownership is intentionally conservative, but
			// represented by the coordinator's compact language fingerprint
			// instead of a quadratic all-project dependency graph.
			units[index].Conservative = true
		}
	}
	// Files with no tsconfig ancestor remain useful syntax units. A tsconfig
	// elsewhere in the workspace must not silently claim them.
	looseSources := []string{}
	for _, source := range sources {
		if _, ok := nearestAncestor(pathDirectory(source), configDirs); !ok {
			looseSources = append(looseSources, source)
		}
	}
	units = append(units, typeScriptSyntaxUnits(context, looseSources)...)
	return units, diagnostics
}

func typeScriptSyntaxUnits(context plannerContext, sources []string) []Unit {
	packageDirectories := map[string]bool{}
	for _, file := range context.files {
		if lastPart(file) == "package.json" {
			packageDirectories[pathDirectory(file)] = true
		}
	}
	grouped := map[string][]string{}
	for _, source := range sources {
		directory, ok := nearestAncestor(pathDirectory(source), packageDirectories)
		if !ok {
			directory = "."
		}
		grouped[directory] = append(grouped[directory], source)
	}
	units := make([]Unit, 0, len(grouped))
	for directory, owned := range grouped {
		units = append(units, Unit{
			ID: "typescript:syntax:" + relativeIDPath(directory), Language: LanguageTypeScript, Mode: ModeSyntax,
			Capabilities: []Capability{CapabilitySyntax}, Sources: owned,
		})
	}
	return units
}

func isTypeScriptSource(path string) bool {
	for _, suffix := range []string{".ts", ".tsx", ".mts", ".cts"} {
		if strings.HasSuffix(path, suffix) {
			return true
		}
	}
	return false
}

func isTSConfig(path string) bool {
	base := lastPart(path)
	return strings.HasPrefix(base, "tsconfig") && strings.HasSuffix(base, ".json")
}

func readTSConfig(context plannerContext, path string) (*tsConfig, error) {
	data, err := context.read(path)
	if err != nil {
		return nil, err
	}
	var raw struct {
		Extends    string   `json:"extends"`
		Files      []string `json:"files"`
		Include    []string `json:"include"`
		References []struct {
			Path string `json:"path"`
		} `json:"references"`
	}
	if err := json.Unmarshal(relaxedJSON(data), &raw); err != nil {
		return nil, err
	}
	config := &tsConfig{
		path: path, directory: pathDirectory(path), extends: raw.Extends,
		project: true, hasRoots: raw.Files != nil || raw.Include != nil || raw.References != nil,
	}
	for _, reference := range raw.References {
		if reference.Path != "" {
			config.references = append(config.references, reference.Path)
		}
	}
	return config, nil
}

func tsConfigInputs(context plannerContext, config *tsConfig, configs map[string]*tsConfig, packageInputs []string) ([]string, *Diagnostic) {
	result := []string{config.path}
	seen := map[string]bool{config.path: true}
	current := config
	for current.extends != "" {
		resolved := resolveTSConfigPath(context, current.directory, current.extends)
		if resolved == "" || seen[resolved] {
			return append(result, packageInputs...), &Diagnostic{config.path, "extends target was not resolved; package configuration was added conservatively"}
		}
		seen[resolved] = true
		result = append(result, resolved)
		next := configs[resolved]
		if next == nil {
			parsed, err := readTSConfig(context, resolved)
			if err != nil {
				return append(result, packageInputs...), &Diagnostic{config.path, "extends chain could not be parsed; package configuration was added conservatively"}
			}
			next = parsed
		}
		current = next
	}
	return append(result, packageInputs...), nil
}

func resolveTSConfigPath(context plannerContext, directory, value string) string {
	if value == "" || (!strings.HasPrefix(value, ".") && !strings.HasPrefix(value, "/")) {
		return ""
	}
	if strings.HasPrefix(value, "/") {
		return ""
	}
	base := joinPath(directory, value)
	for _, candidate := range []string{base, base + ".json", joinPath(base, "tsconfig.json")} {
		if context.fileSet[candidate] {
			return candidate
		}
	}
	return ""
}

func typeScriptPackageInputsByProject(context plannerContext, configs map[string]*tsConfig) map[string][]string {
	byDirectory := map[string][]string{}
	for _, file := range context.files {
		base := lastPart(file)
		switch base {
		case "package.json", "package-lock.json", "pnpm-lock.yaml", "yarn.lock":
			byDirectory[pathDirectory(file)] = append(byDirectory[pathDirectory(file)], file)
		}
	}
	result := make(map[string][]string, len(configs))
	for path, config := range configs {
		if !config.project {
			continue
		}
		for directory := config.directory; ; directory = pathDirectory(directory) {
			result[path] = append(result[path], byDirectory[directory]...)
			if directory == "." {
				break
			}
		}
	}
	return result
}

var trailingJSONComma = regexp.MustCompile(`,\s*([}\]])`)

func relaxedJSON(data []byte) []byte {
	// Strip comments with a small string-aware scanner. tsconfig JSON permits
	// comments and trailing commas even though encoding/json does not.
	result := make([]byte, 0, len(data))
	inString := false
	escaped := false
	for index := 0; index < len(data); index++ {
		char := data[index]
		if inString {
			result = append(result, char)
			if escaped {
				escaped = false
			} else if char == '\\' {
				escaped = true
			} else if char == '"' {
				inString = false
			}
			continue
		}
		if char == '"' {
			inString = true
			result = append(result, char)
			continue
		}
		if char == '/' && index+1 < len(data) && data[index+1] == '/' {
			for index < len(data) && data[index] != '\n' {
				index++
			}
			result = append(result, '\n')
			continue
		}
		if char == '/' && index+1 < len(data) && data[index+1] == '*' {
			index += 2
			for index+1 < len(data) && !(data[index] == '*' && data[index+1] == '/') {
				index++
			}
			index++
			continue
		}
		result = append(result, char)
	}
	return trailingJSONComma.ReplaceAll(result, []byte("$1"))
}
