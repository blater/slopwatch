package sourceestimate

import (
	"fmt"
	"go/ast"
	"go/parser"
	gotoken "go/token"
	"strings"
	"testing"
)

func TestGoUnusedInputFindsComputedSiblingArgument(t *testing.T) {
	_, results := AnalyzeWithAttribution([]File{
		{Path: "helper.go", Language: "go", Source: []byte(`package sample
func target(unused, used int) int { return used }
`)},
		{Path: "caller.go", Language: "go", Source: []byte(`package sample
func Caller(input int) int { return target(input + 2, input) }
`)},
	})
	findings := results["helper.go"].Findings
	if len(findings) != 1 {
		t.Fatalf("expected one source-proven finding, got %+v (result=%+v)", findings, results["helper.go"])
	}
	finding := findings[0]
	if finding.Kind != "unused-input" || finding.Operation != "helper.go#target/0" || finding.Parameter != "unused" {
		t.Fatalf("unexpected finding identity: %+v", finding)
	}
	if len(finding.CallerFiles) != 1 || finding.CallerFiles[0] != "caller.go" {
		t.Fatalf("unexpected caller files: %+v", finding.CallerFiles)
	}
	if finding.UnnecessaryBurden != 0.25 {
		t.Fatalf("unexpected bounded burden: %+v", finding)
	}
}

func TestGoUnusedInputRejectsUnprovenCases(t *testing.T) {
	tests := []struct {
		name   string
		helper string
		caller string
	}{
		{
			name:   "unreachable caller cost",
			helper: "func target(unused int) int { return 7 }",
			caller: "func Caller(input int) int { if false { return target(input + 2) }; return input }",
		},
		{
			name:   "used parameter",
			helper: "func target(input int) int { return input }",
			caller: "func Caller(input int) int { return target(input + 2) }",
		},
		{
			name:   "literal only",
			helper: "func target(unused int) int { return 7 }",
			caller: "func Caller(input int) int { return target(2) + input }",
		},
		{
			name:   "non arithmetic argument",
			helper: "func target(unused int) int { return 7 }",
			caller: "func Caller(input int) int { return target(read()) + input }\nfunc read() int { return 1 }",
		},
		{
			name:   "function value escape",
			helper: "func target(unused int) int { return 7 }",
			caller: "var callback = target\nfunc Caller(input int) int { return callback(input + 2) }",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, results := AnalyzeWithAttribution([]File{
				{Path: "helper.go", Language: "go", Source: []byte("package sample\n" + test.helper + "\n")},
				{Path: "caller.go", Language: "go", Source: []byte("package sample\n" + test.caller + "\n")},
			})
			if got := results["helper.go"].Findings; len(got) != 0 {
				t.Fatalf("unproven case produced findings: %+v", got)
			}
		})
	}
}

func TestGoUnusedInputAllowsNonIntegralParameterAndMultiStatementCaller(t *testing.T) {
	_, results := AnalyzeWithAttribution([]File{
		{Path: "helper.go", Language: "go", Source: []byte(`package sample
func target(unused string) string { return "ok" }
`)},
		{Path: "caller.go", Language: "go", Source: []byte(`package sample
func Caller(input string) string {
		value := target(input)
		return value
}
`)},
		{Path: "broken.go", Language: "go", Source: []byte(`package sample
func broken( {
`)},
	})
	if got := results["helper.go"].Findings; len(got) != 1 {
		t.Fatalf("valid sibling proof was suppressed by unrelated incomplete source: %+v", got)
	}
}

func TestGoUnusedInputRejectsUnreachableSiblingCall(t *testing.T) {
	_, results := AnalyzeWithAttribution([]File{
		{Path: "helper.go", Language: "go", Source: []byte(`package sample
func target(unused int) int { return 7 }
`)},
		{Path: "caller.go", Language: "go", Source: []byte(`package sample
func Caller(input int) int {
		if false { target(input) }
		return input
}
`)},
	})
	if got := results["helper.go"].Findings; len(got) != 0 {
		t.Fatalf("dead sibling call established a finding: %+v", got)
	}
}

func TestGoUnusedInputSourceInfersPublicFreeFunctionOnly(t *testing.T) {
	_, results := AnalyzeWithAttribution([]File{{Path: "public.go", Language: "go", Source: []byte(`package sample
func PublicUsed(used int) int { return used }
func PublicUnused(used int, unused string) int { return used }
func PublicEscaped(used int, unused string) int { return used }
var callback = PublicEscaped
`)}})
	findings := results["public.go"].Findings
	if len(findings) != 1 {
		t.Fatalf("expected only the source-inferred public unused input: %+v", findings)
	}
	finding := findings[0]
	if finding.Operation != "public.go#PublicUnused/1" || finding.Parameter != "unused" {
		t.Fatalf("unexpected public finding identity: %+v", finding)
	}
	if finding.CallerFiles != nil {
		t.Fatalf("source-inferred finding should not claim caller witnesses: %+v", finding)
	}
}

// referenceGoUnusedCallReachable rejects only syntactically certain dead witnesses. It
// intentionally leaves ordinary conditional calls usable; those still have a
// live path unless a literal condition or prior unconditional transfer proves
// otherwise.
func referenceGoUnusedCallReachable(parents map[ast.Node]ast.Node, call *ast.CallExpr) bool {
	for parent := parents[call]; parent != nil; parent = parents[parent] {
		switch enclosing := parent.(type) {
		case *ast.IfStmt:
			if referenceGoUnusedDescendant(parents, enclosing.Body, call) {
				if known, value := referenceGoUnusedBool(enclosing.Cond); known && !value {
					return false
				}
			} else if enclosing.Else != nil && referenceGoUnusedDescendant(parents, enclosing.Else, call) {
				if known, value := referenceGoUnusedBool(enclosing.Cond); known && value {
					return false
				}
			}
		case *ast.ForStmt:
			if enclosing.Body != nil && referenceGoUnusedDescendant(parents, enclosing.Body, call) {
				if known, value := referenceGoUnusedBool(enclosing.Cond); known && !value {
					return false
				}
			}
		case *ast.BinaryExpr:
			if referenceGoUnusedDescendant(parents, enclosing.Y, call) {
				switch enclosing.Op {
				case gotoken.LAND:
					if known, value := referenceGoUnusedBool(enclosing.X); known && !value {
						return false
					}
				case gotoken.LOR:
					if known, value := referenceGoUnusedBool(enclosing.X); known && value {
						return false
					}
				}
			}
		case *ast.BlockStmt:
			for index, statement := range enclosing.List {
				if !referenceGoUnusedDescendant(parents, statement, call) {
					continue
				}
				for _, previous := range enclosing.List[:index] {
					if referenceGoUnusedTerminates(previous) {
						return false
					}
				}
				break
			}
		}
	}
	return true
}

func referenceGoUnusedDescendant(parents map[ast.Node]ast.Node, root, target ast.Node) bool {
	for node := target; node != nil; node = parents[node] {
		if node == root {
			return true
		}
	}
	return false
}

func referenceGoUnusedTerminates(statement ast.Stmt) bool {
	switch statement.(type) {
	case *ast.ReturnStmt:
		return true
	case *ast.BranchStmt:
		return true
	default:
		return false
	}
}

func referenceGoUnusedBool(expression ast.Expr) (known, value bool) {
	switch expression := expression.(type) {
	case *ast.Ident:
		if expression.Name == "true" {
			return true, true
		}
		if expression.Name == "false" {
			return true, false
		}
	case *ast.ParenExpr:
		return referenceGoUnusedBool(expression.X)
	case *ast.UnaryExpr:
		if expression.Op == gotoken.NOT {
			known, value := referenceGoUnusedBool(expression.X)
			return known, !value
		}
	case *ast.BinaryExpr:
		leftKnown, left := referenceGoUnusedBool(expression.X)
		rightKnown, right := referenceGoUnusedBool(expression.Y)
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

func TestGoUnusedContextConservativeBranches(t *testing.T) {
	source := `package p
func Caller(x int) {
 if false { target(x) } else { target(x) }
 if true { target(x) } else if unknown() { target(x) }
 for ; false; target(x) { target(x) }
 _ = false && target(x)
 _ = true || target(x)
 _ = !(false || true) && target(x)
 _ = func() { target(x); return; target(x) }
 { goto label; target(x); label: target(x) }
 return
 target(x)
}`
	file, err := parser.ParseFile(gotoken.NewFileSet(), "p.go", source, 0)
	if err != nil {
		t.Fatal(err)
	}
	parents, files, contexts := map[ast.Node]ast.Node{}, map[ast.Node]int{}, map[ast.Node]goUnusedContext{}
	indexGoUnusedContext(parents, files, contexts, file, 0)
	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		if contexts[call].dead == referenceGoUnusedCallReachable(parents, call) {
			t.Fatalf("reachability changed at %v", call.Pos())
		}
		var caller *ast.FuncDecl
		for parent := parents[call]; parent != nil; parent = parents[parent] {
			if decl, ok := parent.(*ast.FuncDecl); ok {
				caller = decl
				break
			}
			if _, ok := parent.(*ast.FuncLit); ok {
				break
			}
		}
		if contexts[call].caller != caller {
			t.Fatalf("caller changed at %v", call.Pos())
		}
		return true
	})
}

func TestGoUnusedParameterReadWork(t *testing.T) {
	for _, size := range []int{64, 256, 1024} {
		names := make([]string, size)
		var body strings.Builder
		body.WriteString("package p; func f() {\n")
		for i := range names {
			names[i] = fmt.Sprintf("p%d", i)
			if i%2 == 0 {
				fmt.Fprintf(&body, "_ = %s\n", names[i])
			}
		}
		body.WriteString("}")
		file, err := parser.ParseFile(gotoken.NewFileSet(), "p.go", body.String(), 0)
		if err != nil {
			t.Fatal(err)
		}
		block := file.Decls[0].(*ast.FuncDecl).Body
		unused, used, work := goUnusedParameters(block, names)
		nodes := 0
		ast.Inspect(block, func(node ast.Node) bool {
			if node != nil {
				nodes++
			}
			return true
		})
		if used != size/2 || len(unused) != size/2 || work != nodes+size {
			t.Fatalf("size=%d used=%d unused=%v work=%d nodes=%d", size, used, unused, work, nodes)
		}
		for i, index := range unused {
			if index != 2*i+1 {
				t.Fatal("parameter order changed")
			}
		}
	}
}

func TestGoUnusedGroupedParameterMembershipWork(t *testing.T) {
	for _, size := range []int{64, 256, 1024} {
		var source strings.Builder
		source.WriteString("package p\nfunc caller(")
		for i := 0; i < size; i++ {
			if i != 0 {
				source.WriteString(",")
			}
			fmt.Fprintf(&source, "f%d", i)
		}
		source.WriteString(" int) {}\ntype Fields struct {")
		for i := 0; i < size; i++ {
			if i != 0 {
				source.WriteString(",")
			}
			fmt.Fprintf(&source, "f%d", i)
		}
		source.WriteString(" int }")
		file, err := parser.ParseFile(gotoken.NewFileSet(), "p.go", source.String(), 0)
		if err != nil {
			t.Fatal(err)
		}
		pkg := &goUnusedInputPackage{parents: map[ast.Node]ast.Node{}, nodeFile: map[ast.Node]int{}, context: map[ast.Node]goUnusedContext{}}
		work := indexGoUnusedContext(pkg.parents, pkg.nodeFile, pkg.context, file, 0)
		nodes, names := 0, 0
		ast.Inspect(file, func(node ast.Node) bool {
			if node != nil {
				nodes++
			}
			ident, ok := node.(*ast.Ident)
			if !ok || !pkg.context[node].fieldName {
				return true
			}
			names++
			// Parameter names remain reference candidates; struct field names do not.
			parent := pkg.parents[pkg.parents[ident]]
			_, functionType := pkg.parents[parent].(*ast.FuncType)
			if pkg.goUnusedIgnoreReferenceIdent(ident) == functionType {
				t.Fatalf("field-name reference semantics changed at %v", ident.Pos())
			}
			return true
		})
		if names != 2*size || work > 3*nodes {
			t.Fatalf("size=%d names=%d work=%d nodes=%d", size, names, work, nodes)
		}
	}
}

func TestGoUnusedDuplicateDeclarationsInvalidateOnce(t *testing.T) {
	for _, size := range []int{64, 256, 1024} {
		var source strings.Builder
		source.WriteString("package p\n")
		for i := 0; i < size; i++ {
			if i%2 == 0 {
				source.WriteString("func same(same int) {}\n")
			} else {
				source.WriteString("func same(x, y int) {}\n")
			}
		}
		source.WriteString("func caller(x int) {\n")
		for i := 0; i < size; i++ {
			source.WriteString("same(x); same(x,x)\n")
		}
		source.WriteString("}")
		file, err := parser.ParseFile(gotoken.NewFileSet(), "p.go", source.String(), 0)
		if err != nil {
			t.Fatal(err)
		}
		pkg := &goUnusedInputPackage{files: []*ast.File{file}, parents: map[ast.Node]ast.Node{}, nodeFile: map[ast.Node]int{}, context: map[ast.Node]goUnusedContext{}, byName: map[string][]*goUnusedInputFunction{}, refValid: map[*goUnusedInputFunction]bool{}, refs: map[*goUnusedInputFunction][]goUnusedInputCall{}}
		indexGoUnusedContext(pkg.parents, pkg.nodeFile, pkg.context, file, 0)
		functions := []*goUnusedInputFunction{}
		for _, decl := range file.Decls {
			function := decl.(*ast.FuncDecl)
			if function.Name.Name == "same" {
				params, _ := goUnusedNamedParams(function)
				functions = append(functions, &goUnusedInputFunction{decl: function, paramNames: params})
			}
		}
		pkg.indexReferences(functions)
		for _, function := range functions {
			if pkg.refValid[function] {
				t.Fatal("duplicate declaration retained a valid proof")
			}
		}
		nodes := 0
		ast.Inspect(file, func(node ast.Node) bool {
			if node != nil {
				nodes++
			}
			return true
		})
		if pkg.referenceWork > nodes+2*size {
			t.Fatalf("size=%d reference work=%d nodes=%d", size, pkg.referenceWork, nodes)
		}
	}
}
