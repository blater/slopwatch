// Package goadapter translates native Go syntax into normalized structural facts.
package goadapter

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"slopslap.dev/structural/internal/facts"
)

type source struct {
	rel            string
	file           *ast.File
	imports        map[string]string
	typeInfo       *types.Info
	typesAvailable bool
	typeReason     string
}

type analysisContext struct {
	fset           *token.FileSet
	current        source
	activeReceiver string
	functions      []*facts.Function
	operations     []*facts.PublicOperation
}

// Adapter is the built-in Go frontend for the structural host.
type Adapter struct{}

// Language returns the registry key claimed by the adapter.
func (Adapter) Language() string { return "go" }

// FactSchemaVersion declares the normalized contract emitted by this adapter.
func (Adapter) FactSchemaVersion() int { return facts.SchemaVersion }

// ParserModes describes the deterministic parser configuration used per source.
func (Adapter) ParserModes() []string {
	return []string{"go-ast-syntax", "go-types-exact-source", "go-ast-type-fallback"}
}

// Analyze translates exact Go files into normalized structural facts.
func (Adapter) Analyze(workspace string, requested []string, _ map[string]any) (*facts.Program, error) {
	return Analyze(workspace, requested)
}

// Analyze parses each exact source once and returns a deterministic fact set.
func Analyze(workspace string, requested []string) (*facts.Program, error) {
	root, err := filepath.EvalSymlinks(workspace)
	if err != nil {
		return nil, fmt.Errorf("resolve workspace: %w", err)
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("absolute workspace: %w", err)
	}
	fset := token.NewFileSet()
	sources, err := parseSources(root, requested, fset)
	if err != nil {
		return nil, err
	}
	typeCheck(root, sources, fset)

	b := &analysisContext{fset: fset}
	collectFunctions(b, sources)
	types := collectTypes(b, sources)
	representation := representationExposures(types)
	sortFunctions(b.functions)
	sortOperations(b.operations)
	return &facts.Program{
		Functions: b.functions, Types: types, PublicOperations: b.operations,
		Representation: representation, Files: sourcePaths(sources),
	}, nil
}

func parseSources(root string, requested []string, fset *token.FileSet) ([]source, error) {
	sources := make([]source, 0, len(requested))
	seen := make(map[string]struct{}, len(requested))
	for _, raw := range requested {
		rel, absolute, err := canonicalSource(root, raw)
		if err != nil {
			return nil, err
		}
		if _, exists := seen[rel]; exists {
			return nil, fmt.Errorf("duplicate Go source path: %s", rel)
		}
		seen[rel] = struct{}{}
		parsed, err := parser.ParseFile(fset, absolute, nil, parser.AllErrors)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", rel, err)
		}
		imports := make(map[string]string)
		for _, spec := range parsed.Imports {
			path, unquoteErr := strconv.Unquote(spec.Path.Value)
			if unquoteErr != nil {
				return nil, fmt.Errorf("parse import in %s: %w", rel, unquoteErr)
			}
			name := filepath.Base(path)
			if spec.Name != nil {
				name = spec.Name.Name
			}
			imports[name] = path
		}
		sources = append(sources, source{rel: rel, file: parsed, imports: imports})
	}
	sort.Slice(sources, func(left, right int) bool { return sources[left].rel < sources[right].rel })
	return sources, nil
}

func collectFunctions(b *analysisContext, sources []source) {
	for _, item := range sources {
		b.current = item
		for _, declaration := range item.file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Body == nil {
				continue
			}
			b.functions = append(b.functions, functionDeclaration(b, function))
		}
	}
}

func representationExposures(types []*facts.Type) []*facts.RepresentationExposure {
	representation := make([]*facts.RepresentationExposure, 0)
	for _, item := range types {
		for _, field := range item.Fields {
			if !field.Public || !field.Mutable {
				continue
			}
			representation = append(representation, &facts.RepresentationExposure{
				StableID: fmt.Sprintf("%s:%s:%s", item.Location.Path, item.Name, field.Name),
				Kind:     "public-mutable-field", Entity: item.Name + "." + field.Name,
				Location: item.Location, Evidence: "exported Go struct field is mutable at the language boundary",
				Confidence: "exact",
			})
		}
	}
	return representation
}

func sortFunctions(functions []*facts.Function) {
	sort.Slice(functions, func(left, right int) bool {
		a, c := functions[left].Location, functions[right].Location
		if a.Path != c.Path {
			return a.Path < c.Path
		}
		if a.Line != c.Line {
			return a.Line < c.Line
		}
		return a.Column < c.Column
	})
}

func sortOperations(operations []*facts.PublicOperation) {
	sort.Slice(operations, func(left, right int) bool {
		a, c := operations[left].Location, operations[right].Location
		if a.Path != c.Path {
			return a.Path < c.Path
		}
		if a.Line != c.Line {
			return a.Line < c.Line
		}
		return a.Column < c.Column
	})
}

func sourcePaths(sources []source) []string {
	files := make([]string, 0, len(sources))
	for _, item := range sources {
		files = append(files, item.rel)
	}
	// Type facts have a deterministic AST fallback. This is essential for the
	// distributed analyzer, which must not require a Go installation (and its
	// GOROOT/package export data) merely to inspect source that has imports.
	return files
}

func canonicalSource(workspace, raw string) (string, string, error) {
	if raw == "" || strings.Contains(raw, "\\") || filepath.IsAbs(raw) {
		return "", "", fmt.Errorf("non-canonical Go source path: %q", raw)
	}
	clean := filepath.Clean(filepath.FromSlash(raw))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", "", fmt.Errorf("Go source escapes workspace: %q", raw)
	}
	if filepath.ToSlash(clean) != raw {
		return "", "", fmt.Errorf("non-canonical Go source path: %q", raw)
	}
	absolute := filepath.Join(workspace, clean)
	metadata, err := os.Stat(absolute)
	if err != nil {
		return "", "", fmt.Errorf("inspect %s: %w", raw, err)
	}
	if !metadata.Mode().IsRegular() {
		return "", "", fmt.Errorf("Go source is not a regular file: %s", raw)
	}
	if !strings.HasSuffix(strings.ToLower(raw), ".go") {
		return "", "", fmt.Errorf("unsupported Go source extension: %s", raw)
	}
	return raw, absolute, nil
}
