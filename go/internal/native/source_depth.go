package native

import (
	"context"
	"math"
	"strings"

	"github.com/blater/slopwatch/internal/report"
	"github.com/blater/slopwatch/internal/sourceestimate"
)

// Estimates are attached to fresh semantic units before partitioning and cache
// persistence. Warm reads reuse them; incremental runs only visit supplied units.
func withSourceDepthEstimates(ctx context.Context, request analyzerRequest, inputs scoreInputs) (scoreInputs, error) {
	if !sourceDepthEnabled(request) {
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
	var truncated map[string]bool
	var estimates map[string]sourceestimate.Result
	if run := sourceDepthRunFromContext(ctx); run != nil {
		var err error
		files, truncated, estimates, err = run.wait()
		if err != nil {
			return inputs, err
		}
	} else {
		var err error
		files, truncated, err = readSourceDepthFiles(ctx, request)
		if err != nil {
			return inputs, err
		}
	}
	progress := &sourceDepthProgress{ctx: ctx, inputs: &inputs, truncated: truncated, byBoundary: map[string][]sourceestimate.Result{}, finalized: map[string]bool{}}
	progress.shared = sharedDepthBoundaries(inputs)
	if estimates != nil {
		for _, file := range files {
			if estimate, ok := estimates[file.Path]; ok && progress.err == nil {
				progress.err = progress.apply(file, estimate)
			}
		}
	} else {
		fileCount := sourceRequestFileCount(request)
		_, _ = sourceestimate.AnalyzeWithAttributionProgressAndStatus(files, func(file sourceestimate.File, estimate sourceestimate.Result) {
			if progress.err == nil {
				progress.err = progress.apply(file, estimate)
			}
		}, func(status sourceestimate.AttributionProgress) {
			emitSourceEstimateProgress(ctx, request, status, fileCount)
		})
	}
	if progress.err != nil {
		return inputs, progress.err
	}
	progress.finalize(files)
	return inputs, nil
}

func sharedDepthBoundaries(inputs scoreInputs) map[string]bool {
	shared := make(map[string]bool)
	for id, boundary := range inputs.depth {
		shared[id] = len(boundary.Files) > 1
	}
	seen := map[string]int{}
	for _, ids := range inputs.depthByPath {
		for _, id := range ids {
			seen[id]++
			if seen[id] > 1 {
				shared[id] = true
			}
		}
	}
	return shared
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
