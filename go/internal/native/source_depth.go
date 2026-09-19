package native

import (
	"context"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/blater/slopwatch/internal/report"
	"github.com/blater/slopwatch/internal/sourceestimate"
)

// Estimates are attached to fresh semantic units before partitioning and cache
// persistence. Warm reads reuse them; incremental runs only visit supplied units.
func withSourceDepthEstimates(ctx context.Context, request analyzerRequest, inputs scoreInputs) (scoreInputs, error) {
	enabled := false
	for _, component := range request.Components {
		enabled = enabled || component.ID == "module_shallowness" && component.Version == ShallowDefinitionV4
	}
	if !enabled {
		return inputs, nil
	}
	needed := false
	for _, unit := range request.Units {
		needed = needed || len(unit.Paths) > 0 // classify audiences even when public routes were measured
	}
	for _, unit := range request.Units {
		for _, path := range unit.Paths {
			ids := inputs.depthByPath[path]
			needed = needed || len(ids) == 0
			for _, id := range ids {
				needed = needed || needsSourceDepth(inputs.depth[id])
			}
		}
	}
	if !needed {
		return inputs, nil
	}
	var files []sourceestimate.File
	seen := map[string]bool{}
	truncated := map[string]bool{}
	const maxSourceBytes = 2 << 20
	for _, unit := range request.Units {
		for _, path := range unit.Paths {
			if err := ctx.Err(); err != nil {
				return inputs, err
			}
			if seen[path] {
				continue
			}
			seen[path] = true
			stream, err := os.Open(filepath.Join(request.Workspace, filepath.FromSlash(path)))
			if err != nil {
				// Preserve the analyzer's read/syntax diagnostics. An unreadable
				// source cannot honestly receive a source-based estimate.
				continue
			}
			data, readErr := io.ReadAll(io.LimitReader(stream, maxSourceBytes+1))
			closeErr := stream.Close()
			if readErr != nil || closeErr != nil {
				continue
			}
			if len(data) > maxSourceBytes {
				truncated[path] = true
				data = data[:maxSourceBytes]
			}
			files = append(files, sourceestimate.File{Path: path, Language: unit.Language, Source: data})
		}
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	estimates, fileEstimates := sourceestimate.AnalyzeWithAttribution(files)
	byBoundary := map[string][]sourceestimate.Result{}
	for _, file := range files {
		if err := ctx.Err(); err != nil {
			return inputs, err
		}
		estimate, exists := estimates[file.Path]
		if attributed, ok := fileEstimates[file.Path]; ok {
			estimate, exists = attributed, true
			if file.Language == "go" {
				projectGoFileDepth(&inputs, file, !truncated[file.Path])
			}
			projectAttributedFileDepth(&inputs, file, estimate)
		}
		if !exists {
			continue
		}
		if truncated[file.Path] {
			estimate.Limitations = append(estimate.Limitations, "source_byte_limit")
			if !estimate.Applicable {
				// A truncated prefix cannot demonstrate absence of abstraction.
				estimate.Applicable, estimate.Burden, estimate.Hidden = true, 1, 1
				estimate.Categories = map[string]float64{"unknown_outcome": 1}
			}
		}
		inputs.languages[file.Path] = file.Language
		if inputs.coverage[file.Path] == nil {
			inputs.coverage[file.Path] = map[string]string{"module_shallowness": "partial"}
		}
		ids := inputs.depthByPath[file.Path]
		if len(ids) == 0 {
			id := "source:" + file.Language + ":" + file.Path
			inputs.depth[id] = report.DepthBoundary{ID: id, State: "partial", Files: []string{file.Path},
				Boundary: map[string]any{"artifact": file.Path, "audience": "source", "view": "file", "symbol": file.Path}}
			inputs.depthByPath[file.Path] = []string{id}
			ids = inputs.depthByPath[file.Path]
		}
		for _, id := range ids {
			boundary := inputs.depth[id]
			if !needsSourceDepth(boundary) {
				if boundary.State == "measured" && estimate.Grade != nil && !estimate.RoleOnly && len(boundary.DeclarationFiles) == 0 {
					inputs.depth[id] = gradedMeasuredBoundary(boundary, estimate)
				}
				continue
			}
			if _, attributed := fileEstimates[file.Path]; attributed {
				// The bounded result selects the maximum declared abstraction.
				// An incomplete adapter aggregate has a different denominator.
				boundary.Raw = nil
				inputs.depth[id] = boundary
			}
			byBoundary[id] = append(byBoundary[id], estimate)
		}
	}
	// Package boundaries can span many files. Combine once, preserving shared
	// helper responsibility identities, rather than replacing the same boundary
	// repeatedly with whichever source file happened to be visited last.
	for id, estimates := range byBoundary {
		inputs.depth[id] = estimateDepthBoundary(inputs.depth[id], sourceestimate.Merge(estimates...))
	}

	for _, file := range files {

		// The adapter's old availability state describes precise evidence. Once
		// a source rating exists, recompute the numeric projection independently.
		state := ""
		for _, id := range inputs.depthByPath[file.Path] {
			state = mergeDepthState(state, inputs.depth[id].State)
		}
		inputs.depthStates[file.Path] = state
	}
	return inputs, nil
}

func needsSourceDepth(boundary report.DepthBoundary) bool {
	// Role/non-applicability proofs are independent of behavior precision. A
	// supporting provider may retain estimated evidence without exposing an
	// independently scored abstraction; source inventory must not undo that role.
	if boundary.State == "not_applicable" {
		return false
	}
	return boundary.Estimated || (boundary.State != "measured" && boundary.State != "not_applicable") ||
		(boundary.State == "measured" && boundary.Shallow == nil)
}

func estimateDepthBoundary(boundary report.DepthBoundary, estimate sourceestimate.Result) report.DepthBoundary {
	// Retain demonstrated non-applicability. A weak parser failing to find a
	// callable cannot erase an already inventoried abstraction.
	if !estimate.Applicable && boundary.State == "not_applicable" {
		return boundary
	}
	if !estimate.Applicable && estimate.NoAbstractionProven && len(boundary.Raw) == 0 {
		boundary.State, boundary.Shallow = "not_applicable", nil
		boundary.Estimated = false
		boundary.PolicyRevision = ShallowPolicyRevisionV4
		boundary.Evidence = []any{map[string]any{"id": boundary.ID + "/empty", "kind": "source-no-abstraction", "status": "not_applicable"}}
		return boundary
	}
	if estimate.RoleOnly {
		zero := 0.0
		boundary.State, boundary.Shallow, boundary.Estimated = "measured", &zero, false
		boundary.PolicyRevision, boundary.InventoryFingerprint = ShallowPolicyRevisionV4, ""
		boundary.Raw = map[string]any{"B8": 0, "H": 0, "zero_reason": "recognized_role"}
		boundary.Evidence = sourceRoleEvidence(estimate.Roles)
		return report.CompactDepthBoundary(boundary)
	}
	burden := estimate.Burden
	if !estimate.Applicable {
		estimate.Categories = map[string]float64{"unknown_outcome": 1}
		estimate.Limitations = append(estimate.Limitations, "unresolved_source_surface")
	}
	// Where inventory is available retain its caller-burden units. Recognized
	// responsibility is a lower bound, never a substitute for unknown work.
	if value, err := number(boundary.Raw["B8"]); err == nil && value > 0 {
		burden = value / 8
	}
	knownHidden := 0.0
	if value, err := number(boundary.Raw["H"]); err == nil && value > 0 {
		knownHidden = value
	}
	if burden <= 0 {
		burden = 1 // one inventoried service with an otherwise unresolved surface
	}
	descriptiveRatio := sourceestimate.ResponsibilityRatio(burden, math.Max(estimate.RecognizedHidden(), knownHidden))
	value := estimate.PublishedPenalty()
	penaltySupport := "graded-caller-responsibility"

	boundary.State, boundary.Estimated, boundary.Shallow = "partial", true, &value
	boundary.PolicyRevision, boundary.InventoryFingerprint = ShallowPolicyRevisionV4, ""
	boundary.Dependencies = appendUnique(boundary.Dependencies, estimate.Dependencies...)
	if len(boundary.PreciseReasons) == 0 {
		boundary.PreciseReasons = append([]any(nil), boundary.Reasons...)
	}
	boundary.Raw = map[string]any{"B8": burden * 8, "H": math.Max(estimate.RecognizedHidden(), knownHidden), "responsibility_ratio": descriptiveRatio,
		"graded": sourceGradeDetails(estimate.Grade), "zero_reason": sourceZeroReason(estimate),
		"estimate": map[string]any{"model": "graded-caller-responsibility-v1", "burden": burden, "hidden": estimate.RecognizedHidden(),
			"recognized_hidden": estimate.RecognizedHidden(), "uncertain_burden": estimate.Assessment().UncertainBurden,
			"descriptive_ratio": descriptiveRatio, "published_penalty": value, "penalty_support": penaltySupport,
			"uncertainty_policy": sourceestimate.UncertaintyPolicy, "known_hidden": knownHidden, "source_hidden": estimate.RecognizedHidden(),
			"categories": estimate.Categories, "limitations": estimate.Limitations, "roles": estimate.Roles,
			"findings": sourceFindingDetails(estimate.Findings), "abstractions": sourceAbstractionDetails(estimate.Abstractions)}}

	boundary.Evidence = []any{map[string]any{"id": boundary.ID + "/source-estimate", "kind": "source-responsibility-v1", "status": "estimated"}}
	return report.CompactDepthBoundary(boundary)
}

func sourceAbstractionDetails(items []sourceestimate.Abstraction) []any {
	result := make([]any, 0, len(items))
	for _, item := range items {
		projection := sourceestimate.Result{Applicable: true, Burden: item.Burden, Hidden: item.RecognizedHidden, Grade: item.Grade}
		value := projection.PublishedPenalty()
		result = append(result, map[string]any{"graded": sourceGradeDetails(item.Grade), "name": item.Name, "audience": item.Audience, "burden": item.Burden, "hidden": item.RecognizedHidden, "recognized_hidden": item.RecognizedHidden, "uncertain_burden": item.UncertainBurden, "limitations": item.Limitations, "findings": sourceFindingDetails(item.Findings), "published_penalty": value})
	}
	return result
}

func sourceFindingDetails(findings []sourceestimate.Finding) []any {
	result := make([]any, 0, len(findings))
	for _, f := range findings {
		support := "source-inferred"
		if len(f.CallerFiles) > 0 {
			support = "observed-callers"
		}
		result = append(result, map[string]any{"support": support, "kind": f.Kind, "operation": f.Operation, "parameter": f.Parameter, "caller_files": f.CallerFiles, "unnecessary_burden": f.UnnecessaryBurden})
	}
	return result
}

func sourceZeroReason(estimate sourceestimate.Result) string {
	if estimate.RoleOnly {
		return "recognized_role"
	}
	if estimate.Grade != nil {
		return estimate.Grade.ZeroReason
	}
	if estimate.PublishedPenalty() == 0 {
		if len(estimate.Limitations) > 0 {
			return "conservative_uncertainty"
		}
		return "lowest_range_supported"
	}
	return ""
}

func gradedMeasuredBoundary(boundary report.DepthBoundary, estimate sourceestimate.Result) report.DepthBoundary {
	if boundary.Raw == nil {
		boundary.Raw = map[string]any{}
	}
	if boundary.Shallow != nil {
		boundary.Raw["responsibility_ratio"] = *boundary.Shallow
	}
	// Preserve precise semantic facts and fix-safety fingerprint independently of
	// the public calibration. A source estimate cannot upgrade those proofs.
	symbol, _ := boundary.Boundary["symbol"].(string)
	for _, item := range estimate.Abstractions {
		if symbol == item.Name || strings.HasSuffix(symbol, "."+item.Name) {
			estimate.Grade = item.Grade
			estimate.Burden = item.Burden
			estimate.Hidden = item.RecognizedHidden
			break
		}
	}
	value := estimate.PublishedPenalty()
	boundary.Shallow = &value
	boundary.Raw["graded"] = sourceGradeDetails(estimate.Grade)
	boundary.Raw["zero_reason"] = sourceZeroReason(estimate)
	boundary.Raw["penalty_basis"] = "graded-caller-responsibility"
	boundary.PolicyRevision = ShallowPolicyRevisionV4
	if estimate.Grade != nil && len(estimate.Grade.MaterialLimitations) > 0 {
		boundary.Estimated = true
		boundary.InventoryFingerprint = ""
	}
	return boundary
}

func sourceGradeDetails(grade *sourceestimate.GradedEvidence) map[string]any {
	if grade == nil {
		return nil
	}
	return map[string]any{
		"residual_burden": grade.ResidualBurden, "supported_burden": grade.SupportedBurden, "hidden_responsibility": grade.Hidden,
		"estimated_hidden_responsibility": grade.EstimatedHidden,
		"responsibilities":                grade.Responsibilities, "material_limitations": grade.MaterialLimitations, "zero_reason": grade.ZeroReason,
		"surface": map[string]any{"operation_units": grade.Surface.OperationUnits, "input_units": grade.Surface.InputUnits, "representation_units": grade.Surface.RepresentationUnits, "evidence": grade.Surface.Evidence, "constraints": grade.Surface.Constraints},
	}
}
