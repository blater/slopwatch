package goadapter

import (
	"go/ast"
	"go/token"
	"go/types"
	"sort"

	"slopslap.dev/structural/internal/facts"
)

// applyGoPassiveEnum proves the narrow data-only enum package role. Every
// declaration must be an import, a named primitive type, or a typed constant
// of one of those types; any function, variable, or composite type rejects the
// role. This keeps an enum beside executable behavior from acquiring an
// exemption.
func applyGoPassiveEnum(group *depthNamespace) {
	for _, reason := range group.boundary.Reasons {
		if reason.Code == "incomplete_package_inventory" {
			return
		}
	}
	typesByName := map[*types.TypeName]string{}
	exportedType := false
	for _, item := range group.sources {
		if !item.typesAvailable || item.typeInfo == nil {
			return
		}
		for _, declaration := range item.file.Decls {
			gen, ok := declaration.(*ast.GenDecl)
			if !ok {
				return
			}
			if gen.Tok != token.TYPE {
				continue
			}
			for _, spec := range gen.Specs {
				typeSpec, ok := spec.(*ast.TypeSpec)
				if !ok || typeSpec.Assign.IsValid() || typeSpec.TypeParams != nil {
					return
				}
				name, ok := item.typeInfo.Defs[typeSpec.Name].(*types.TypeName)
				if !ok {
					return
				}
				if _, ok := name.Type().Underlying().(*types.Basic); !ok {
					return
				}
				typesByName[name] = typeSpec.Name.Name
				exportedType = exportedType || typeSpec.Name.IsExported()
			}
		}
	}
	if len(typesByName) == 0 || !exportedType {
		return
	}
	exportedConstant := false
	constantCount := 0
	for _, item := range group.sources {
		for _, declaration := range item.file.Decls {
			gen, ok := declaration.(*ast.GenDecl)
			if !ok {
				return
			}
			switch gen.Tok {
			case token.IMPORT:
			case token.TYPE:
			case token.CONST:
				for _, spec := range gen.Specs {
					values, ok := spec.(*ast.ValueSpec)
					if !ok || len(values.Names) == 0 {
						return
					}
					for _, name := range values.Names {
						if name.Name == "_" {
							continue
						}
						constant, ok := item.typeInfo.Defs[name].(*types.Const)
						if !ok {
							return
						}
						typeName, ok := constant.Type().(*types.Named)
						if !ok || typesByName[typeName.Obj()] == "" {
							return
						}
						constantCount++
						exportedConstant = exportedConstant || name.IsExported()
					}
				}
			default:
				return
			}
		}
	}
	if constantCount == 0 || !exportedConstant {
		return
	}
	names := make([]string, 0, len(typesByName))
	for _, name := range typesByName {
		names = append(names, name)
	}
	sort.Strings(names)
	group.boundary.Evidence = append(group.boundary.Evidence, facts.Evidence{
		ID: "passive-enum-v1:" + group.boundary.Identity.String(), Kind: "passive-enum-v1", Status: "proven",
		Details: map[string]any{"types": names, "constants": constantCount},
	})
}
