package unitplan

import (
	"encoding/json"
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
	return buildTypeScriptPlan(context, options)
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

func relaxedJSON(data []byte) []byte {
	return normalizeTSConfigJSON(data)
}
