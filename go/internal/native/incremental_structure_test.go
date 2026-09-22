package native

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"testing"
)

func TestIncrementalCallChainHasNoCanonicalInventoryWalk(t *testing.T) {
	files := []string{"incremental_changes.go", "incremental_sources.go", "incremental_selection.go", "incremental_cache.go", "../unitplan/incremental.go", "../unitplan/incremental_delta.go", "../workspace/monitor_events.go", "../workspace/monitor_directories.go", "../workspace/monitor_watch.go"}
	startup := map[string]bool{"incremental_sources.go:initializeSession": true, "incremental_sources.go:verifyInitialSession": true, "incremental.go:InitializeSources": true, "monitor_watch.go:registerAll": true}
	banned := map[string]bool{"PlanWorkspace": true, "workspaceFiles": true, "discoverPolicy": true, "discoverPolicyWithMissingTargets": true, "allChangeUnits": true, "filterPlannedUnits": true, "InitializeSources": true, "Walk": true, "WalkDir": true, "ReadDir": true}
	for _, path := range files {
		tree, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, decl := range tree.Decls {
			function, ok := decl.(*ast.FuncDecl)
			if !ok || function.Body == nil || startup[filepath.Base(path)+":"+function.Name.Name] {
				continue
			}
			ast.Inspect(function.Body, func(node ast.Node) bool {
				if loop, ok := node.(*ast.RangeStmt); ok {
					if field, ok := loop.X.(*ast.SelectorExpr); ok {
						root, plain := field.X.(*ast.Ident)
						if plain && (root.Name == "index" && (field.Sel.Name == "units" || field.Sel.Name == "sources") || root.Name == "m" && (field.Sel.Name == "known" || field.Sel.Name == "watched")) {
							t.Errorf("%s:%s ranges over canonical %s.%s", path, function.Name.Name, root.Name, field.Sel.Name)
						}
					}
				}
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}
				name := ""
				switch target := call.Fun.(type) {
				case *ast.Ident:
					name = target.Name
				case *ast.SelectorExpr:
					name = target.Sel.Name
				}
				if banned[name] {
					// The only enumeration permitted by the event chain is registration of
					// the named genuinely new subtree, through its instrumented filesystem.
					if filepath.Base(path) == "monitor_watch.go" && function.Name.Name == "addDirectory" && name == "ReadDir" {
						return true
					}
					t.Errorf("%s:%s calls forbidden %s", path, function.Name.Name, name)
				}
				return true
			})
		}
	}
}
