package fixapp

import (
	"sort"

	"github.com/blater/slopwatch/internal/candidate"
	"github.com/blater/slopwatch/internal/fix"
	"github.com/blater/slopwatch/internal/sourcepath"
)

func (record *jobRecord) applyDiffInventory(diff candidate.DiffSnapshot) {
	existing := presentationByPath(record.presentation.Targets)
	record.baseScope, record.presentation.Scope = diff.Scope, diff.Scope
	record.diffHash, record.presentation.DiffFingerprint = diff.Fingerprint, diff.Fingerprint
	record.diffPaths = inventoryPaths(diff)
	files := baselineFileMap(record.input.Baseline.Contract, existing)
	for _, file := range diff.Files {
		projectDiffFile(files, existing, file)
	}
	record.presentation.Targets = sortedPresentations(files)
}

func presentationByPath(values []fix.FilePresentation) map[fix.RepoPath]fix.FilePresentation {
	result := make(map[fix.RepoPath]fix.FilePresentation, len(values))
	for _, file := range values {
		result[file.Path] = file
	}
	return result
}

func inventoryPaths(diff candidate.DiffSnapshot) map[fix.RepoPath]bool {
	paths := make(map[fix.RepoPath]bool, len(diff.Files)*2)
	for _, file := range diff.Files {
		if file.Path != "" {
			paths[file.Path] = true
		}
		if file.Previous != "" {
			paths[file.Previous] = true
		}
	}
	return paths
}

func baselineFileMap(contract fix.ScoringContract, existing map[fix.RepoPath]fix.FilePresentation) map[fix.RepoPath]fix.FilePresentation {
	files := make(map[fix.RepoPath]fix.FilePresentation, len(contract.Targets))
	for _, file := range baselineTargets(contract) {
		if prior, ok := existing[file.Path]; ok {
			file.VerifiedScore = prior.VerifiedScore
			file.VerifiedMetrics = append([]fix.MetricValue(nil), prior.VerifiedMetrics...)
			file.Verification = prior.Verification
		}
		files[file.Path] = file
	}
	return files
}

func projectDiffFile(files map[fix.RepoPath]fix.FilePresentation, existing map[fix.RepoPath]fix.FilePresentation, file candidate.DiffFile) {
	if !sourcepath.IsSourceFile(file.Path.String()) {
		return
	}
	projected, exists := files[file.Path]
	if !exists {
		projected = fix.FilePresentation{Path: file.Path, Classification: "supporting"}
		if prior, ok := existing[file.Path]; ok {
			projected = prior
		}
	}
	projected.Changed, projected.ChangeStatus, projected.PreviousPath = true, file.Status, file.Previous
	files[file.Path] = projected
}

func sortedPresentations(files map[fix.RepoPath]fix.FilePresentation) []fix.FilePresentation {
	paths := make([]string, 0, len(files))
	for path := range files {
		paths = append(paths, path.String())
	}
	sort.Strings(paths)
	result := make([]fix.FilePresentation, 0, len(paths))
	for _, path := range paths {
		result = append(result, files[fix.RepoPath(path)])
	}
	return result
}
