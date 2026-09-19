package native

import (
	"github.com/blater/slopwatch/internal/report"
	"github.com/blater/slopwatch/internal/sourceestimate"
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
)

// A contract file declares types and constants, but implements no behavior.
// Reject variables (initializers can execute) and every function declaration,
// including bodyless external functions. Parse errors never establish a role.
func goDeclarationOnly(source []byte) bool {
	file, err := parser.ParseFile(token.NewFileSet(), "", source, parser.SkipObjectResolution)
	if err != nil {
		return false
	}
	declarations := false
	for _, decl := range file.Decls {
		general, ok := decl.(*ast.GenDecl)
		if !ok {
			return false
		}
		switch general.Tok {
		case token.IMPORT:
		case token.TYPE, token.CONST:
			declarations = true
		default:
			return false
		}
	}
	return declarations
}

// File roots share the package analysis index, never the package score.
func projectGoFileDepth(inputs *scoreInputs, file sourceestimate.File, completeSource bool) {
	ids := inputs.depthByPath[file.Path]
	var own []string
	var reasons []any
	for _, id := range ids {
		boundary, exists := inputs.depth[id]
		if !exists {
			continue
		}
		if boundary.Boundary["view"] == "namespace" {
			reasons = appendUniqueAny(reasons, boundary.Reasons...)
			delete(inputs.depth, id)
			continue
		}
		boundary.Scope = "file"
		inputs.depth[id] = boundary
		own = append(own, id)
	}
	declaration := completeSource && goDeclarationOnly(file.Source)
	if len(own) == 0 || declaration {
		for _, id := range own {
			delete(inputs.depth, id)
		}
		id := "source:go:" + file.Path
		boundary := report.DepthBoundary{ID: id, Scope: "file", State: "partial", Files: []string{file.Path}, Reasons: reasons,
			Boundary: map[string]any{"artifact": file.Path, "audience": "source", "view": "file", "symbol": file.Path}}
		if declaration {
			zero := 0.0
			boundary.State, boundary.Shallow = "measured", &zero
			boundary.PolicyRevision = ShallowPolicyRevisionV4
			boundary.Raw = map[string]any{"zero_reason": "recognized_role"}
			boundary.DeclarationFiles = []string{file.Path}
			boundary.Evidence = []any{map[string]any{"kind": "declaration-only-contract", "status": "proven"}}
		}
		inputs.depth[id] = boundary
		own = []string{id}
	}
	inputs.depthByPath[file.Path] = own
}

func sourceRoleEvidence(roles []string) []any {
	result := make([]any, 0, len(roles))
	for _, role := range roles {
		result = append(result, map[string]any{"kind": role, "status": "proven"})
	}
	return result
}

// Preserve precise behavioral measurements while separately admitting internal
// audiences omitted by an export-only inventory. Partial source projections
// belong to the file, never to every class contained in it.
func projectAttributedFileDepth(inputs *scoreInputs, file sourceestimate.File, estimate sourceestimate.Result) {
	ids := inputs.depthByPath[file.Path]
	own := make([]string, 0, len(ids)+1)
	var reasons []any
	fallback := len(ids) == 0 || len(estimate.Abstractions) > 1 || len(estimate.Findings) > 0
	for _, abstraction := range estimate.Abstractions {
		if abstraction.Audience != "external" {
			fallback = true
		}
	}
	for _, id := range ids {
		boundary, exists := inputs.depth[id]
		if !exists {
			continue
		}
		symbol, _ := boundary.Boundary["symbol"].(string)
		role := ""
		for owner, candidate := range estimate.Supporting {
			if symbol == owner || strings.HasSuffix(symbol, "."+owner) || strings.HasSuffix(symbol, "$"+owner) {
				role = candidate
				break
			}
		}
		if role != "" || estimate.RoleOnly {
			zero := 0.0
			boundary.State, boundary.Shallow, boundary.Estimated = "measured", &zero, false
			boundary.PolicyRevision, boundary.InventoryFingerprint = ShallowPolicyRevisionV4, ""
			roles := estimate.Roles
			if role != "" {
				roles = []string{role}
			}
			boundary.Evidence = sourceRoleEvidence(roles)
			boundary.Raw = map[string]any{"B8": 0, "H": 0, "zero_reason": "recognized_role"}
		}
		boundary.Scope = "file"
		// An outer contract's N/A must not hide source-inventoried nested or
		// internal implementations from the file's numeric projection.
		if boundary.State == "not_applicable" && estimate.Applicable {
			fallback = true
		}
		if needsSourceDepth(boundary) {
			fallback = true
			reasons = appendUniqueAny(reasons, boundary.Reasons...)
			reasons = appendUniqueAny(reasons, boundary.PreciseReasons...)
			delete(inputs.depth, id)
		} else {
			inputs.depth[id] = boundary
			own = append(own, id)
		}
	}
	if fallback && !estimate.RoleOnly || len(own) == 0 {
		id := "source:" + file.Language + ":" + file.Path
		inputs.depth[id] = report.DepthBoundary{ID: id, Scope: "file", State: "partial", Files: []string{file.Path}, Reasons: reasons,
			Boundary: map[string]any{"artifact": file.Path, "view": "file", "audience": "source", "symbol": file.Path}}
		own = append(own, id)
	}
	inputs.depthByPath[file.Path] = own
}
