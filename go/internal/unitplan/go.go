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
	moduleDirs := goModuleDirectories(context.files)
	packages := groupGoPackages(context.files)
	diagnostics := loadGoModules(context, packages, moduleDirs)
	productionIDs, importOwners := goPackageOwners(packages)
	return goAnalysisUnits(context, packages, productionIDs, importOwners), diagnostics
}

func goModuleDirectories(files []string) map[string]bool {
	moduleDirs := map[string]bool{}
	for _, path := range files {
		if strings.HasSuffix(path, "/go.mod") || path == "go.mod" {
			moduleDirs[pathDirectory(path)] = true
		}
	}
	return moduleDirs
}

func groupGoPackages(files []string) map[string]*goPackage {
	packages := map[string]*goPackage{}
	for _, path := range files {
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
	return packages
}

func loadGoModules(context plannerContext, packages map[string]*goPackage, moduleDirs map[string]bool) []Diagnostic {
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
	return diagnostics
}

func goPackageOwners(packages map[string]*goPackage) (map[string]string, map[string]string) {
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
	return productionIDs, importOwners
}

func goAnalysisUnits(context plannerContext, packages map[string]*goPackage, productionIDs, importOwners map[string]string) []Unit {
	var units []Unit
	for _, pkg := range packages {
		configs := goConfigInputs(context, pkg)
		if unit, ok := goProductionUnit(context, pkg, configs, productionIDs, importOwners); ok {
			units = append(units, unit)
		}
		if unit, ok := goTestUnit(context, pkg, configs, productionIDs, importOwners); ok {
			units = append(units, unit)
		}
	}
	return units
}

func goProductionUnit(context plannerContext, pkg *goPackage, configs []string, productionIDs, importOwners map[string]string) (Unit, bool) {
	if len(pkg.regular) == 0 {
		return Unit{}, false
	}
	dependencies, uncertain := goDependencies(context, pkg.regular, importOwners)
	return Unit{
		ID: productionIDs[pkg.directory], Language: LanguageGo, Mode: ModeProject,
		Capabilities: []Capability{CapabilitySyntax, CapabilityTypes, CapabilityDependencies},
		Sources:      pkg.regular, ConfigInputs: configs, DirectDependencies: dependencies,
		Conservative: pkg.module == "" || uncertain,
	}, true
}

func goTestUnit(context plannerContext, pkg *goPackage, configs []string, productionIDs, importOwners map[string]string) (Unit, bool) {
	if len(pkg.tests) == 0 {
		return Unit{}, false
	}
	dependencies, uncertain := goDependencies(context, pkg.tests, importOwners)
	if productionID := productionIDs[pkg.directory]; productionID != "" {
		dependencies = append(dependencies, productionID)
	}
	return Unit{
		ID: "go:test-package:" + relativeIDPath(pkg.directory), Language: LanguageGo, Mode: ModeProject,
		Capabilities: []Capability{CapabilitySyntax, CapabilityTypes, CapabilityDependencies, CapabilityTests},
		Sources:      pkg.tests, ConfigInputs: configs, DirectDependencies: dependencies,
		Conservative: pkg.module == "" || uncertain,
	}, true
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

func goModulePath(contents string) string {
	for _, line := range strings.Split(contents, "\n") {
		fields := strings.Fields(strings.TrimSpace(strings.SplitN(line, "//", 2)[0]))
		if len(fields) == 2 && fields[0] == "module" {
			return strings.Trim(fields[1], `"`)
		}
	}
	return ""
}
