package unitplan

import (
	"go/parser"
	"go/token"
	"strconv"
	"strings"
)

type goPackage struct {
	directory string
	regular   []string
	tests     []string
	moduleDir string
	module    string
}

func planGo(context plannerContext, _ Options) ([]Unit, []Diagnostic) {
	moduleDirs := map[string]bool{}
	for _, path := range context.files {
		if strings.HasSuffix(path, "/go.mod") || path == "go.mod" {
			moduleDirs[pathDirectory(path)] = true
		}
	}
	packages := map[string]*goPackage{}
	for _, path := range context.files {
		if !strings.HasSuffix(path, ".go") {
			continue
		}
		directory := pathDirectory(path)
		pkg := packages[directory]
		if pkg == nil {
			pkg = &goPackage{directory: directory}
			packages[directory] = pkg
		}
		if strings.HasSuffix(path, "_test.go") {
			pkg.tests = append(pkg.tests, path)
		} else {
			pkg.regular = append(pkg.regular, path)
		}
	}

	var diagnostics []Diagnostic
	for _, pkg := range packages {
		if directory, ok := nearestAncestor(pkg.directory, moduleDirs); ok {
			pkg.moduleDir = directory
			data, err := context.read(joinPath(directory, "go.mod"))
			if err == nil {
				pkg.module = goModulePath(string(data))
			}
			if pkg.module == "" {
				diagnostics = append(diagnostics, Diagnostic{joinPath(directory, "go.mod"), "module path was not recognized; local imports use conservative matching"})
			}
		}
	}

	productionIDs := map[string]string{}
	importOwners := map[string]string{}
	for _, pkg := range packages {
		if len(pkg.regular) == 0 {
			continue
		}
		id := "go:package:" + relativeIDPath(pkg.directory)
		productionIDs[pkg.directory] = id
		if pkg.module != "" {
			relative := strings.TrimPrefix(strings.TrimPrefix(pkg.directory, pkg.moduleDir), "/")
			importPath := pkg.module
			if relative != "" && relative != "." {
				importPath += "/" + relative
			}
			importOwners[importPath] = id
		}
	}

	var units []Unit
	for _, pkg := range packages {
		configs := goConfigInputs(context, pkg)
		if len(pkg.regular) > 0 {
			dependencies, uncertain := goDependencies(context, pkg.regular, importOwners)
			if uncertain || pkg.module == "" {
				dependencies = append(dependencies, otherUnitIDs(productionIDs, pkg.directory)...)
			}
			units = append(units, Unit{
				ID: productionIDs[pkg.directory], Language: LanguageGo, Mode: ModeProject,
				Capabilities: []Capability{CapabilitySyntax, CapabilityTypes, CapabilityDependencies},
				Sources:      pkg.regular, ConfigInputs: configs, DirectDependencies: dependencies,
				Conservative: pkg.module == "" || uncertain,
			})
		}
		if len(pkg.tests) > 0 {
			dependencies, uncertain := goDependencies(context, pkg.tests, importOwners)
			if productionIDs[pkg.directory] != "" {
				dependencies = append(dependencies, productionIDs[pkg.directory])
			}
			if uncertain || pkg.module == "" {
				dependencies = append(dependencies, otherUnitIDs(productionIDs, pkg.directory)...)
			}
			units = append(units, Unit{
				ID: "go:test-package:" + relativeIDPath(pkg.directory), Language: LanguageGo, Mode: ModeProject,
				Capabilities: []Capability{CapabilitySyntax, CapabilityTypes, CapabilityDependencies, CapabilityTests},
				Sources:      pkg.tests, ConfigInputs: configs, DirectDependencies: dependencies,
				Conservative: pkg.module == "" || uncertain,
			})
		}
	}
	return units, diagnostics
}

func goConfigInputs(context plannerContext, pkg *goPackage) []string {
	var result []string
	if pkg.moduleDir != "" {
		for _, name := range []string{"go.mod", "go.sum"} {
			path := joinPath(pkg.moduleDir, name)
			if context.fileSet[path] {
				result = append(result, path)
			}
		}
	}
	for _, path := range context.files {
		if path == "go.work" || path == "go.work.sum" || strings.HasSuffix(path, "/go.work") || strings.HasSuffix(path, "/go.work.sum") {
			directory := pathDirectory(path)
			if pathWithin(pkg.directory, directory) {
				result = append(result, path)
			}
		}
	}
	return result
}

func goDependencies(context plannerContext, sources []string, owners map[string]string) ([]string, bool) {
	var dependencies []string
	uncertain := false
	for _, source := range sources {
		data, err := context.read(source)
		if err != nil {
			uncertain = true
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), source, data, parser.ImportsOnly)
		if err != nil {
			uncertain = true
			continue
		}
		for _, spec := range file.Imports {
			path, err := strconv.Unquote(spec.Path.Value)
			if err == nil && owners[path] != "" {
				dependencies = append(dependencies, owners[path])
			}
		}
	}
	return dependencies, uncertain
}

func otherUnitIDs(ids map[string]string, directory string) []string {
	var result []string
	for candidate, id := range ids {
		if candidate != directory {
			result = append(result, id)
		}
	}
	return result
}

func goModulePath(contents string) string {
	for _, line := range strings.Split(contents, "\n") {
		fields := strings.Fields(strings.TrimSpace(strings.SplitN(line, "//", 2)[0]))
		if len(fields) == 2 && fields[0] == "module" {
			return strings.Trim(fields[1], `"`)
		}
	}
	return ""
}
