package goadapter

import (
	"fmt"
	"go/token"
	"go/types"
	"slopslap.dev/structural/internal/builtincontracts"
)

type depthImporter struct {
	packages map[string]*types.Package
	bindings map[*types.Func]builtincontracts.Entry
}

func newDepthImporter() *depthImporter {
	loader := &depthImporter{packages: map[string]*types.Package{}, bindings: map[*types.Func]builtincontracts.Entry{}}
	entry, ok := builtincontracts.Lookup("go", "errors.New", "(string)error", "resolved_standard_library", nil)
	if !ok {
		return loader
	}
	pkg := types.NewPackage("errors", "errors")
	parameter := types.NewVar(token.NoPos, pkg, "text", types.Typ[types.String])
	result := types.NewVar(token.NoPos, pkg, "", types.Universe.Lookup("error").Type())
	signature := types.NewSignatureType(nil, nil, nil, types.NewTuple(parameter), types.NewTuple(result), false)
	function := types.NewFunc(token.NoPos, pkg, "New", signature)
	pkg.Scope().Insert(function)
	pkg.MarkComplete()
	loader.packages[pkg.Path()] = pkg
	loader.bindings[function] = entry
	return loader
}
func (loader *depthImporter) Import(path string) (*types.Package, error) {
	if pkg := loader.packages[path]; pkg != nil {
		return pkg, nil
	}
	return nil, fmt.Errorf("SHALLOW v4 import facts unavailable: %s", path)
}
