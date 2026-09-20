package nativeadapter

import (
	"context"
	"errors"
	"fmt"

	"github.com/blater/slopwatch/internal/fix"
	"github.com/blater/slopwatch/internal/fixanalysis"
	"github.com/blater/slopwatch/internal/report"
)

func (service *Service) PrepareBaseline(ctx context.Context, request fixanalysis.BaselineRequest) (fixanalysis.BaselineSnapshot, error) {
	if err := validateWorkspace(request.Workspace.RepositoryRoot, request.Workspace.AnalysisRoot); err != nil {
		return fixanalysis.BaselineSnapshot{}, fmt.Errorf("prepare baseline workspace: %w", err)
	}
	if err := validateTargets(request.Targets); err != nil {
		return fixanalysis.BaselineSnapshot{}, fmt.Errorf("prepare baseline targets: %w", err)
	}
	if err := validateGoal(request.Goal); err != nil {
		return fixanalysis.BaselineSnapshot{}, fmt.Errorf("prepare baseline goal: %w", err)
	}
	mapper, err := newPathMapper(request.Workspace.RepositoryRoot, request.Workspace.AnalysisRoot)
	if err != nil {
		return fixanalysis.BaselineSnapshot{}, err
	}
	targets, err := mapper.analysisTargets(request.Targets)
	if err != nil {
		return fixanalysis.BaselineSnapshot{}, err
	}
	before, err := fingerprintTargets(request.Workspace.RepositoryRoot, request.Targets)
	if err != nil {
		return fixanalysis.BaselineSnapshot{}, fmt.Errorf("fingerprint baseline targets: %w", err)
	}
	document, catalogID, err := service.analyzeReport(ctx, mapper.analysisRoot, targets, service.config.BaselineReadCache, false)
	if err != nil {
		return fixanalysis.BaselineSnapshot{}, fmt.Errorf("analyze baseline: %w", err)
	}
	after, err := fingerprintTargets(request.Workspace.RepositoryRoot, request.Targets)
	if err != nil {
		return fixanalysis.BaselineSnapshot{}, fmt.Errorf("fingerprint baseline targets after analysis: %w", err)
	}
	if before.aggregate != after.aggregate {
		return fixanalysis.BaselineSnapshot{}, errors.New("baseline targets changed during analysis")
	}
	files := baselineFiles(document, mapper, request.Targets)
	snapshots := make([]fix.TargetSnapshot, len(request.Targets))
	for index, path := range request.Targets {
		file := files[path]
		if hasEstimatedDepth(file, document.Depth) {
			file.Complete = false
		}
		snapshot, snapshotErr := targetSnapshot(path, before.files[path], file, mapper)
		if snapshotErr != nil {
			return fixanalysis.BaselineSnapshot{}, snapshotErr
		}
		snapshots[index] = snapshot
	}
	preparedAt := service.config.Clock()
	contract := fix.ScoringContract{
		CatalogID: catalogID, ProfileSetHash: document.ProfileSetHash,
		Targets: snapshots, Goal: cloneGoal(request.Goal),
	}
	return fixanalysis.BaselineSnapshot{
		Workspace: request.Workspace, Contract: cloneContract(contract),
		Fingerprint: before.aggregate, PreparedAt: preparedAt,
	}, nil
}

// baselineFiles projects only requested paths. Missing or ambiguous report entries
// leave an incomplete snapshot; report metadata never broadens filesystem scope.
func baselineFiles(document report.Document, mapper pathMapper, targets []fix.RepoPath) map[fix.RepoPath]report.File {
	result := make(map[fix.RepoPath]report.File, len(targets))
	seen := make(map[fix.RepoPath]bool)
	for _, target := range targets {
		result[target] = report.File{}
	}
	for _, file := range document.Files {
		path, err := mapper.repositoryPath(file.Path)
		if err != nil {
			continue
		}
		if _, requested := result[path]; !requested {
			continue
		}
		if seen[path] {
			result[path] = report.File{}
			continue
		}
		seen[path] = true
		if document.Truncated || !document.Calibrated || document.SchemaVersion <= 0 || document.ProfileSetHash == "" {
			file.Complete = false
		}
		result[path] = file
	}
	return result
}
