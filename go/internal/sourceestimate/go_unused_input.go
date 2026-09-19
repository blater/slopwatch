package sourceestimate

import (
	"go/ast"
	"go/parser"
	gotoken "go/token"
	"sort"
	"strings"
)

// annotateGoUnusedInputs records only the narrow case in which a private Go
// helper demonstrably accepts work that its body discards.  The proof is kept
// separate from the token estimate because it needs package-wide syntax and
// identifier identity.  An incomplete package is deliberately silent.
func annotateGoUnusedInputs(units []unit) {
	packages := map[string][]int{}
	for index, unit := range units {
		if normalizeLanguage(unit.file.Language, unit.file.Path) != "go" {
			continue
		}
		packages[unit.pkg] = append(packages[unit.pkg], index)
	}
	for _, indexes := range packages {
		annotateGoUnusedInputPackage(units, indexes)
	}
}

type goUnusedInputPackage struct {
	units     []unit
	indexes   []int
	fset      *gotoken.FileSet
	files     []*ast.File
	fileIndex map[*ast.File]int
	nodeFile  map[ast.Node]int
	parents   map[ast.Node]ast.Node
	refs      map[*goUnusedInputFunction][]goUnusedInputCall
	refValid  map[*goUnusedInputFunction]bool
	byName    map[string][]*goUnusedInputFunction
	callerArg map[*ast.FuncDecl]goUnusedCallerArgs
}

type goUnusedInputFunction struct {
	decl       *ast.FuncDecl
	unit       int
	paramNames []string
	operation  *operation
	exposed    bool
}

type goUnusedInputCall struct {
	call     *ast.CallExpr
	caller   *ast.FuncDecl
	callerAt int
}

func annotateGoUnusedInputPackage(units []unit, indexes []int) {
	pkg := &goUnusedInputPackage{
		units:     units,
		indexes:   append([]int(nil), indexes...),
		fset:      gotoken.NewFileSet(),
		fileIndex: map[*ast.File]int{},
		nodeFile:  map[ast.Node]int{},
		parents:   map[ast.Node]ast.Node{},
		refs:      map[*goUnusedInputFunction][]goUnusedInputCall{},
		refValid:  map[*goUnusedInputFunction]bool{},
		byName:    map[string][]*goUnusedInputFunction{},
		callerArg: map[*ast.FuncDecl]goUnusedCallerArgs{},
	}
	if !pkg.parse() {
		return
	}
	functions := pkg.functions()
	pkg.indexReferences(functions)
	for _, function := range functions {
		pkg.annotateFunction(function)
	}
}

func (p *goUnusedInputPackage) parse() bool {
	if len(p.indexes) == 0 {
		return false
	}
	files := make([]*ast.File, 0, len(p.indexes))
	for _, index := range p.indexes {
		unit := p.units[index]
		if unit.limited || !unit.lexicallyValid || goUnusedInputExcluded(unit.file.Source) {
			continue
		}
		file, err := parser.ParseFile(p.fset, unit.file.Path, unit.file.Source, parser.ParseComments)
		if err != nil || file == nil {
			continue
		}
		for _, imported := range file.Imports {
			if imported.Path.Value == `"unsafe"` || imported.Path.Value == `"C"` {
				file = nil
				break
			}
		}
		if file == nil {
			continue
		}
		files = append(files, file)
		p.fileIndex[file] = index
		indexGoUnusedParents(p.parents, p.nodeFile, file, index)
	}
	p.files = files
	return len(files) != 0
}

func goUnusedInputExcluded(source []byte) bool {
	text := string(source)
	lower := strings.ToLower(text)
	if strings.Contains(lower, "//go:build") || strings.Contains(lower, "// +build") || strings.Contains(lower, "//go:linkname") {
		return true
	}
	// The generated-file convention is intentionally conservative.  A package
	// containing generated source is not a complete hand-written proof surface.
	if strings.Contains(lower, "code generated") && strings.Contains(lower, "do not edit") {
		return true
	}
	return false
}

func indexGoUnusedParents(parents map[ast.Node]ast.Node, nodeFile map[ast.Node]int, file *ast.File, index int) {
	stack := make([]ast.Node, 0, 32)
	ast.Inspect(file, func(node ast.Node) bool {
		if node == nil {
			if len(stack) != 0 {
				stack = stack[:len(stack)-1]
			}
			return true
		}
		if len(stack) != 0 {
			parents[node] = stack[len(stack)-1]
		}
		nodeFile[node] = index
		stack = append(stack, node)
		return true
	})
}

func (p *goUnusedInputPackage) functions() []*goUnusedInputFunction {
	byOperation := map[string]*operation{}
	for _, index := range p.indexes {
		for _, op := range p.units[index].ops {
			if op.owner == "" && op.language == "go" {
				byOperation[op.name+"\x00"+itoa(index)] = op
			}
		}
	}
	result := make([]*goUnusedInputFunction, 0)
	for _, file := range p.files {
		index := p.fileIndex[file]
		ast.Inspect(file, func(node ast.Node) bool {
			decl, ok := node.(*ast.FuncDecl)
			if !ok || decl.Recv != nil || decl.Name == nil || decl.Name.Name == "_" {
				return true
			}
			if decl.Doc != nil && strings.Contains(decl.Doc.Text(), "Deprecated:") {
				return true
			}
			params, ok := goUnusedNamedParams(decl)
			if !ok || len(params) == 0 {
				return true
			}
			result = append(result, &goUnusedInputFunction{
				decl:       decl,
				unit:       index,
				paramNames: params,
				operation:  byOperation[decl.Name.Name+"\x00"+itoa(index)],
				exposed:    ast.IsExported(decl.Name.Name),
			})
			return true
		})
	}
	return result
}

func goUnusedNamedParams(decl *ast.FuncDecl) ([]string, bool) {
	if decl.Type == nil || decl.Type.Params == nil || decl.Type.Params.NumFields() == 0 {
		return nil, false
	}
	names := make([]string, 0)
	for _, field := range decl.Type.Params.List {
		if _, variadic := field.Type.(*ast.Ellipsis); variadic || len(field.Names) == 0 {
			return nil, false
		}
		for _, ident := range field.Names {
			if ident == nil || ident.Name == "_" {
				return nil, false
			}
			names = append(names, ident.Name)
		}
	}
	return names, true
}

func (p *goUnusedInputPackage) annotateFunction(function *goUnusedInputFunction) {
	if function.operation == nil || function.decl.Body == nil {
		return
	}
	unused, usedCount := goUnusedParameters(function.decl.Body, function.paramNames)
	if len(unused) == 0 {
		return
	}
	calls, valid := p.callsFor(function)
	if !valid {
		return
	}
	for _, parameterIndex := range unused {
		callerFiles := map[string]bool{}
		computedSibling := false
		for _, inbound := range calls {
			if parameterIndex >= len(inbound.call.Args) {
				continue
			}
			pure, computed := p.pureGoUnusedArgument(inbound.caller, inbound.call.Args[parameterIndex])
			if !pure {
				continue
			}
			if computed && inbound.callerAt != function.unit {
				callerFiles[p.units[inbound.callerAt].file.Path] = true
				computedSibling = true
			}
		}
		if !computedSibling && (!function.exposed || usedCount == 0) {
			continue
		}
		var files []string
		if computedSibling {
			files = make([]string, 0, len(callerFiles))
			for file := range callerFiles {
				files = append(files, file)
			}
			sort.Strings(files)
		}
		function.operation.findings = append(function.operation.findings, Finding{
			Kind:              "unused-input",
			Operation:         function.operation.id,
			Parameter:         function.paramNames[parameterIndex],
			CallerFiles:       files,
			UnnecessaryBurden: 0.25,
		})
	}
}

func goUnusedParameters(body *ast.BlockStmt, names []string) ([]int, int) {
	unused := make([]int, 0, len(names))
	usedCount := 0
	for index, parameter := range names {
		used := false
		ast.Inspect(body, func(node ast.Node) bool {
			ident, ok := node.(*ast.Ident)
			if ok && ident.Name == parameter {
				used = true
				return false
			}
			return true
		})
		if !used {
			unused = append(unused, index)
		} else {
			usedCount++
		}
	}
	return unused, usedCount
}
