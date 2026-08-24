package goadapter

import (
	"fmt"
	"go/ast"
	"go/importer"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type typeGroup struct {
	importPath string
	name       string
	indices    []int
	files      []*ast.File
	info       *types.Info
	pkg        *types.Package
	checking   bool
	checked    bool
	failed     bool
	reason     string
}

type exactSourceImporter struct {
	fset     *token.FileSet
	groups   map[string]*typeGroup
	fallback types.Importer
}

type moduleLocation struct {
	root string
	path string
}

type moduleResolver struct {
	workspace string
	byDir     map[string]moduleLocation
}

func (loader *exactSourceImporter) Import(path string) (*types.Package, error) {
	group := loader.groups[path]
	if group == nil {
		return loader.fallback.Import(path)
	}
	if group.checked {
		if group.failed {
			return group.pkg, fmt.Errorf("type-check %s failed", path)
		}
		return group.pkg, nil
	}
	if group.checking {
		group.failed = true
		return nil, fmt.Errorf("Go package import cycle involving %s", path)
	}
	group.checking = true
	configuration := types.Config{
		Importer: loader,
		Error: func(err error) {
			group.failed = true
			if group.reason == "" {
				if typed, ok := err.(types.Error); ok {
					group.reason = typed.Msg
				} else {
					group.reason = err.Error()
				}
			}
		},
	}
	group.pkg, _ = configuration.Check(group.importPath, loader.fset, group.files, group.info)
	group.checking = false
	group.checked = true
	if group.pkg == nil {
		group.pkg = types.NewPackage(group.importPath, group.name)
	}
	if group.failed {
		return group.pkg, fmt.Errorf("type-check %s failed", path)
	}
	return group.pkg, nil
}

func modulePath(path string) string {
	contents, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(contents), "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) != 2 || fields[0] != "module" {
			continue
		}
		if unquoted, err := strconv.Unquote(fields[1]); err == nil {
			return unquoted
		}
		return fields[1]
	}
	return ""
}

func newModuleResolver(workspace string) *moduleResolver {
	return &moduleResolver{workspace: workspace, byDir: make(map[string]moduleLocation)}
}

func (resolver *moduleResolver) moduleFor(directory string) moduleLocation {
	visited := make([]string, 0, 4)
	for current := directory; ; current = filepath.Dir(current) {
		if cached, exists := resolver.byDir[current]; exists {
			for _, path := range visited {
				resolver.byDir[path] = cached
			}
			return cached
		}
		visited = append(visited, current)
		if module := modulePath(filepath.Join(current, "go.mod")); module != "" {
			result := moduleLocation{root: current, path: module}
			for _, path := range visited {
				resolver.byDir[path] = result
			}
			return result
		}
		if current == resolver.workspace || filepath.Dir(current) == current {
			break
		}
	}
	for _, path := range visited {
		resolver.byDir[path] = moduleLocation{}
	}
	return moduleLocation{}
}

func (resolver *moduleResolver) sourceImportPath(item source) string {
	qualify := func(path string) string {
		if strings.HasSuffix(item.file.Name.Name, "_test") {
			return path + "_test"
		}
		return path
	}
	directory := filepath.Dir(filepath.Join(resolver.workspace, filepath.FromSlash(item.rel)))
	module := resolver.moduleFor(directory)
	if module.path != "" {
		relative, err := filepath.Rel(module.root, directory)
		if err == nil && relative != "." {
			return qualify(strings.TrimSuffix(module.path, "/") + "/" + filepath.ToSlash(relative))
		}
		return qualify(module.path)
	}
	directoryID := filepath.ToSlash(filepath.Dir(item.rel))
	if directoryID == "." {
		directoryID = "root"
	}
	return qualify("slopslap.local/" + directoryID)
}

func typeCheck(root string, sources []source, fset *token.FileSet) {
	byKey := make(map[string]*typeGroup)
	modules := newModuleResolver(root)
	for index, item := range sources {
		key := packageID(item)
		group := byKey[key]
		if group == nil {
			group = &typeGroup{
				importPath: modules.sourceImportPath(item), name: item.file.Name.Name,
				info: &types.Info{
					Types:      make(map[ast.Expr]types.TypeAndValue),
					Defs:       make(map[*ast.Ident]types.Object),
					Uses:       make(map[*ast.Ident]types.Object),
					Selections: make(map[*ast.SelectorExpr]*types.Selection),
				},
			}
			byKey[key] = group
		}
		group.indices = append(group.indices, index)
		group.files = append(group.files, item.file)
	}
	loader := &exactSourceImporter{
		fset: fset, groups: make(map[string]*typeGroup),
		fallback: importer.ForCompiler(fset, "source", nil),
	}
	groups := make([]*typeGroup, 0, len(byKey))
	for _, group := range byKey {
		if existing := loader.groups[group.importPath]; existing != nil {
			group.failed = true
			existing.failed = true
		} else {
			loader.groups[group.importPath] = group
		}
		groups = append(groups, group)
	}
	sort.Slice(groups, func(left, right int) bool { return groups[left].importPath < groups[right].importPath })
	for _, group := range groups {
		_, _ = loader.Import(group.importPath)
		for _, index := range group.indices {
			sources[index].typeInfo = group.info
			sources[index].typesAvailable = !group.failed
			sources[index].typeReason = group.reason
		}
	}
}
