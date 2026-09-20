package follow

import (
	"github.com/blater/slopwatch/internal/report"
	"path/filepath"
	"time"
)

// A replacement may change boundary identities. Keep shared ledgers while any
// displayed file still references them; remove only retired, unreferenced IDs.
func pruneReplacedDepth(document *report.Document, retired map[string]bool) {
	if len(retired) == 0 {
		return
	}
	for _, file := range document.Files {
		for _, component := range file.Components {
			for _, id := range component.DepthBoundaryIDs {
				delete(retired, id)
			}
		}
	}
	for id := range retired {
		delete(document.Depth, id)
	}
}

func mergeDocument(model *Model, result analysisResult) {
	if len(model.files.BaseDocument.Files) == 0 && len(model.files.Document.Files) > 0 {
		model.files.BaseDocument = model.files.Document
	}
	if result.full {
		model.files.BaseDocument = result.document
		return
	}
	replace := map[string]bool{}
	for _, path := range result.replace {
		replace[filepath.ToSlash(path)] = true
	}
	for _, file := range result.document.Files {
		replace[filepath.ToSlash(file.Path)] = true
	}
	retiredDepth := map[string]bool{}
	kept := make([]report.File, 0, len(model.files.BaseDocument.Files)+len(result.document.Files))
	for _, file := range model.files.BaseDocument.Files {
		if !replace[file.Path] {
			kept = append(kept, file)
		} else {
			for _, component := range file.Components {
				for _, id := range component.DepthBoundaryIDs {
					retiredDepth[id] = true
				}
			}
		}
	}
	model.files.BaseDocument.Files = append(kept, result.document.Files...)
	pruneReplacedDepth(&model.files.BaseDocument, retiredDepth)
	model.files.BaseDocument.Diagnostics = mergeDiagnostics(model.files.BaseDocument.Diagnostics, result.document.Diagnostics, replace, result.document.ExecutionPlans)
	model.files.BaseDocument.ExecutionPlans = mergeExecutionPlans(model.files.BaseDocument.ExecutionPlans, result.document.ExecutionPlans)
	if len(result.document.Depth) > 0 {
		if model.files.BaseDocument.Depth == nil {
			model.files.BaseDocument.Depth = map[string]report.DepthBoundary{}
		}
		for id, boundary := range result.document.Depth {
			model.files.BaseDocument.Depth[id] = boundary
		}
	}
}

func merge(model *Model, result analysisResult) {
	oldScores := map[string]float64{}
	oldRanks := map[string]int{}
	model.files.ensureScoreDistribution()
	affected := map[string]bool{}
	if !result.full {
		for _, path := range result.replace {
			affected[filepath.ToSlash(path)] = true
		}
		for _, file := range result.document.Files {
			affected[filepath.ToSlash(file.Path)] = true
		}
	}
	oldAffected := map[string]report.File{}
	for _, file := range model.files.Document.Files {
		oldScores[file.Path] = file.Score
		oldRanks[file.Path] = file.Rank
		if affected[filepath.ToSlash(file.Path)] {
			oldAffected[filepath.ToSlash(file.Path)] = file
		}
	}
	mergeDocument(model, result)
	// Keep the projected copy synchronized before rebuilding weights. This is
	// also required when an incremental result removes its final affected row.
	model.files.Document = model.files.BaseDocument
	projectWeightedDocument(model)
	if result.full {
		model.files.rebuildScoreDistribution()
	} else {
		model.files.replaceScoreDistribution(oldAffected, projectWeightedFiles(*model, result.document.Files), affected)
	}
	model.files.Document.SortAndRank()
	model.files.pruneMarks()
	model.files.HorizontalOffset = min(model.files.HorizontalOffset, maxPathOffset(*model))
	now := time.Now()
	mergeRows(model, result, oldScores, oldRanks, now, result.full && len(oldScores) == 0)
	model.files.refreshDisplayFiles(model.options.Limit)
	restoreSelection(model)
}
