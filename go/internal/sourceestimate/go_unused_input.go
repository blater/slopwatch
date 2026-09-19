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
	unused := make([]int, 0, len(function.paramNames))
	usedCount := 0
	for index, parameter := range function.paramNames {
		used := false
		ast.Inspect(function.decl.Body, func(node ast.Node) bool {
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

func (p *goUnusedInputPackage) indexReferences(functions []*goUnusedInputFunction) {
	for _, function := range functions {
		p.byName[function.decl.Name.Name] = append(p.byName[function.decl.Name.Name], function)
		p.refValid[function] = true
	}
	for _, file := range p.files {
		ast.Inspect(file, func(node ast.Node) bool {
			ident, ok := node.(*ast.Ident)
			if !ok {
				return true
			}
			candidates := p.byName[ident.Name]
			if len(candidates) == 0 {
				return true
			}
			if goUnusedIgnoreReferenceIdent(p.parents, ident) {
				return true
			}
			for _, function := range candidates {
				if ident == function.decl.Name {
					continue
				}
				if goUnusedOwnSignature(p.parents, function.decl, ident) {
					continue
				}
				call, ok := p.parents[ident].(*ast.CallExpr)
				if !ok || call.Fun != ident || len(call.Args) != len(function.paramNames) {
					p.refValid[function] = false
					continue
				}
				caller, callerAt := p.enclosingFunction(call)
				if caller == nil {
					p.refValid[function] = false
					continue
				}
				if !goUnusedCallReachable(p.parents, call) {
					continue
				}
				p.refs[function] = append(p.refs[function], goUnusedInputCall{call: call, caller: caller, callerAt: callerAt})
			}
			return true
		})
	}
}

func goUnusedOwnSignature(parents map[ast.Node]ast.Node, decl *ast.FuncDecl, ident *ast.Ident) bool {
	field, ok := parents[ident].(*ast.Field)
	if !ok {
		return false
	}
	fieldList, ok := parents[field].(*ast.FieldList)
	if !ok {
		return false
	}
	functionType, ok := parents[fieldList].(*ast.FuncType)
	if !ok {
		return false
	}
	for node := parents[functionType]; node != nil; node = parents[node] {
		if node == decl {
			return true
		}
		if _, isFunction := node.(*ast.FuncDecl); isFunction {
			return false
		}
	}
	return false
}

func goUnusedIgnoreReferenceIdent(parents map[ast.Node]ast.Node, ident *ast.Ident) bool {
	switch parent := parents[ident].(type) {
	case *ast.SelectorExpr:
		return parent.Sel == ident
	case *ast.KeyValueExpr:
		return parent.Key == ident
	case *ast.TypeSpec:
		return parent.Name == ident
	case *ast.Field:
		for _, name := range parent.Names {
			if name != ident {
				continue
			}
			fieldList, ok := parents[parent].(*ast.FieldList)
			if !ok {
				return true
			}
			if _, functionType := parents[fieldList].(*ast.FuncType); functionType {
				return false
			}
			return true
		}
	}
	return false
}

// goUnusedCallReachable rejects only syntactically certain dead witnesses. It
// intentionally leaves ordinary conditional calls usable; those still have a
// live path unless a literal condition or prior unconditional transfer proves
// otherwise.
func goUnusedCallReachable(parents map[ast.Node]ast.Node, call *ast.CallExpr) bool {
	for parent := parents[call]; parent != nil; parent = parents[parent] {
		switch enclosing := parent.(type) {
		case *ast.IfStmt:
			if goUnusedDescendant(parents, enclosing.Body, call) {
				if known, value := goUnusedBool(enclosing.Cond); known && !value {
					return false
				}
			} else if enclosing.Else != nil && goUnusedDescendant(parents, enclosing.Else, call) {
				if known, value := goUnusedBool(enclosing.Cond); known && value {
					return false
				}
			}
		case *ast.ForStmt:
			if enclosing.Body != nil && goUnusedDescendant(parents, enclosing.Body, call) {
				if known, value := goUnusedBool(enclosing.Cond); known && !value {
					return false
				}
			}
		case *ast.BinaryExpr:
			if goUnusedDescendant(parents, enclosing.Y, call) {
				switch enclosing.Op {
				case gotoken.LAND:
					if known, value := goUnusedBool(enclosing.X); known && !value {
						return false
					}
				case gotoken.LOR:
					if known, value := goUnusedBool(enclosing.X); known && value {
						return false
					}
				}
			}
		case *ast.BlockStmt:
			for index, statement := range enclosing.List {
				if !goUnusedDescendant(parents, statement, call) {
					continue
				}
				for _, previous := range enclosing.List[:index] {
					if goUnusedTerminates(previous) {
						return false
					}
				}
				break
			}
		}
	}
	return true
}

func goUnusedDescendant(parents map[ast.Node]ast.Node, root, target ast.Node) bool {
	for node := target; node != nil; node = parents[node] {
		if node == root {
			return true
		}
	}
	return false
}

func goUnusedTerminates(statement ast.Stmt) bool {
	switch statement.(type) {
	case *ast.ReturnStmt:
		return true
	case *ast.BranchStmt:
		return true
	default:
		return false
	}
}

func goUnusedBool(expression ast.Expr) (known, value bool) {
	switch expression := expression.(type) {
	case *ast.Ident:
		if expression.Name == "true" {
			return true, true
		}
		if expression.Name == "false" {
			return true, false
		}
	case *ast.ParenExpr:
		return goUnusedBool(expression.X)
	case *ast.UnaryExpr:
		if expression.Op == gotoken.NOT {
			known, value := goUnusedBool(expression.X)
			return known, !value
		}
	case *ast.BinaryExpr:
		leftKnown, left := goUnusedBool(expression.X)
		rightKnown, right := goUnusedBool(expression.Y)
		if leftKnown && rightKnown {
			switch expression.Op {
			case gotoken.LAND:
				return true, left && right
			case gotoken.LOR:
				return true, left || right
			}
		}
	}
	return false, false
}

func (p *goUnusedInputPackage) callsFor(function *goUnusedInputFunction) ([]goUnusedInputCall, bool) {
	return p.refs[function], p.refValid[function]
}

func (p *goUnusedInputPackage) enclosingFunction(node ast.Node) (*ast.FuncDecl, int) {
	for parent := p.parents[node]; parent != nil; parent = p.parents[parent] {
		if decl, ok := parent.(*ast.FuncDecl); ok {
			return decl, p.nodeFile[decl]
		}
		if _, ok := parent.(*ast.FuncLit); ok {
			return nil, -1
		}
	}
	return nil, -1
}

func (p *goUnusedInputPackage) pureGoUnusedArgument(caller *ast.FuncDecl, expression ast.Expr) (pure, computed bool) {
	info, ok := p.callerArg[caller]
	if !ok {
		info = goUnusedCallerArgs{allowed: map[string]bool{}}
		if caller != nil && caller.Type != nil && caller.Type.Params != nil {
			for _, field := range caller.Type.Params.List {
				if _, variadic := field.Type.(*ast.Ellipsis); variadic {
					info.shadowed = true
					break
				}
				for _, ident := range field.Names {
					info.allowed[ident.Name] = true
				}
			}
		}
		if !info.shadowed {
			info.shadowed = goUnusedCallerShadowed(caller, info.allowed)
		}
		p.callerArg[caller] = info
	}
	if info.shadowed {
		return false, false
	}
	return pureGoUnusedExpression(info.allowed, expression)
}

type goUnusedCallerArgs struct {
	allowed  map[string]bool
	shadowed bool
}

func goUnusedCallerShadowed(caller *ast.FuncDecl, allowed map[string]bool) bool {
	if caller == nil || caller.Body == nil {
		return false
	}
	shadowed := false
	ast.Inspect(caller.Body, func(node ast.Node) bool {
		switch declaration := node.(type) {
		case *ast.AssignStmt:
			if declaration.Tok == gotoken.DEFINE {
				for _, left := range declaration.Lhs {
					if ident, ok := left.(*ast.Ident); ok && allowed[ident.Name] {
						shadowed = true
						return false
					}
				}
			}
		case *ast.ValueSpec:
			for _, ident := range declaration.Names {
				if allowed[ident.Name] {
					shadowed = true
					return false
				}
			}
		case *ast.RangeStmt:
			if ident, ok := declaration.Key.(*ast.Ident); ok && allowed[ident.Name] {
				shadowed = true
				return false
			}
			if ident, ok := declaration.Value.(*ast.Ident); ok && allowed[ident.Name] {
				shadowed = true
				return false
			}
		case *ast.FuncLit:
			if declaration.Type != nil && declaration.Type.Params != nil {
				for _, field := range declaration.Type.Params.List {
					for _, ident := range field.Names {
						if allowed[ident.Name] {
							shadowed = true
							return false
						}
					}
				}
			}
		}
		return !shadowed
	})
	return shadowed
}

func pureGoUnusedExpression(allowed map[string]bool, expression ast.Expr) (bool, bool) {
	switch value := expression.(type) {
	case *ast.BasicLit:
		return true, false
	case *ast.Ident:
		return allowed[value.Name], allowed[value.Name]
	case *ast.ParenExpr:
		return pureGoUnusedExpression(allowed, value.X)
	case *ast.UnaryExpr:
		if value.Op != gotoken.ADD && value.Op != gotoken.SUB {
			return false, false
		}
		pure, _ := pureGoUnusedExpression(allowed, value.X)
		return pure, pure
	case *ast.BinaryExpr:
		if value.Op != gotoken.ADD && value.Op != gotoken.SUB && value.Op != gotoken.MUL {
			return false, false
		}
		leftPure, leftComputed := pureGoUnusedExpression(allowed, value.X)
		rightPure, rightComputed := pureGoUnusedExpression(allowed, value.Y)
		return leftPure && rightPure, leftComputed || rightComputed
	default:
		return false, false
	}
}
