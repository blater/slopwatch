package goadapter

import (
	"go/ast"
	"go/token"
	"go/types"

	"slopslap.dev/structural/internal/facts"
)

func typeSpecifications(file *ast.File) []*ast.TypeSpec {
	output := make([]*ast.TypeSpec, 0)
	for _, declaration := range file.Decls {
		general, ok := declaration.(*ast.GenDecl)
		if !ok || general.Tok != token.TYPE {
			continue
		}
		for _, specification := range general.Specs {
			if typeSpec, ok := specification.(*ast.TypeSpec); ok {
				output = append(output, typeSpec)
			}
		}
	}
	return output
}

func typeShape(typeSpec *ast.TypeSpec) (string, map[string]struct{}, int) {
	fields := make(map[string]struct{})
	switch value := typeSpec.Type.(type) {
	case *ast.StructType:
		for _, field := range value.Fields.List {
			if len(field.Names) == 0 {
				fields[receiver(field.Type)] = struct{}{}
			}
			for _, name := range field.Names {
				fields[name.Name] = struct{}{}
			}
		}
		return "struct", fields, 0
	case *ast.InterfaceType:
		methodCount := 0
		for _, field := range value.Methods.List {
			if len(field.Names) == 0 {
				methodCount++
			} else {
				methodCount += len(field.Names)
			}
		}
		return "interface", fields, methodCount
	default:
		return "type", fields, 0
	}
}

func declaredType(b *analysisContext, item source, typeSpec *ast.TypeSpec) *typeRecord {
	kind, fields, interfaceMethods := typeShape(typeSpec)
	fact := &facts.Type{
		Name: typeSpec.Name.Name, Kind: kind, Location: location(b, typeSpec),
		InterfaceMethodCount: interfaceMethods, MethodFields: make(map[string][]string),
	}
	identity := typeSpec.Name.Name
	if item.typesAvailable {
		if object, ok := item.typeInfo.Defs[typeSpec.Name].(*types.TypeName); ok {
			if named, ok := types.Unalias(object.Type()).(*types.Named); ok {
				identity = namedTypeIdentity(named)
			}
		}
		fact.ForeignTypes = referencedTypes(typeSpec.Type, item.typeInfo, identity)
	} else {
		fact.ForeignTypes = namedTypes(typeSpec.Type, typeSpec.Name.Name)
	}
	return &typeRecord{fact: fact, fields: fields, identity: identity}
}

func collectTypeDeclarations(b *analysisContext, sources []source) map[string]*typeRecord {
	records := make(map[string]*typeRecord)
	for _, item := range sources {
		b.current = item
		for _, typeSpec := range typeSpecifications(item.file) {
			records[packageID(item)+"|"+typeSpec.Name.Name] = declaredType(b, item, typeSpec)
		}
	}
	return records
}
