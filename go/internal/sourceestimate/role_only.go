package sourceestimate

import (
	"go/ast"
	"go/parser"
	gotoken "go/token"
)

func goRoleOnlyFile(u unit) bool {
	if u.limited || !u.lexicallyValid {
		return false
	}
	parsed, err := parser.ParseFile(gotoken.NewFileSet(), u.file.Path, u.file.Source, parser.SkipObjectResolution)
	if err != nil {
		return false
	}
	for _, declaration := range parsed.Decls {
		general, ok := declaration.(*ast.GenDecl)
		if !ok || general.Tok != gotoken.VAR {
			continue
		}
		for _, spec := range general.Specs {
			if value, ok := spec.(*ast.ValueSpec); ok && len(value.Values) > 0 {
				return false
			}
		}
	}
	return true
}

func typeScriptRoleOnlyFile(u unit, supporting map[string]string) bool {
	for i := 0; i < len(u.tokens); {
		switch u.tokens[i].text {
		case ";", "export", "default":
			i++
		case "import":
			for i < len(u.tokens) && u.tokens[i].kind != "string" && u.tokens[i].text != ";" {
				i++
			}
			if i < len(u.tokens) {
				i++
			}
		case "class":
			if i+1 >= len(u.tokens) || supporting[u.tokens[i+1].text] == "" {
				return false
			}
			i += 2
			for i < len(u.tokens) && u.tokens[i].text != "{" {
				i++
			}
			if i >= len(u.tokens) {
				return false
			}
			end := matching(u.tokens, i, "{", "}")
			if end < 0 {
				return false
			}
			i = end + 1
		default:
			return false
		}
	}
	return !u.limited && u.lexicallyValid
}
