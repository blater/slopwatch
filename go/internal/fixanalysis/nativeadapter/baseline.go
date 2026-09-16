package nativeadapter

import (
	"context"
	"errors"
	"fmt"

	"github.com/blater/slopwatch/internal/fix"
	"github.com/blater/slopwatch/internal/fixanalysis"
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
	document, catalogID, err := service.analyze(ctx, mapper.analysisRoot, targets, service.config.BaselineReadCache)
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
	files, err := exactFiles(document, mapper, request.Targets)
	if err != nil {
		return fixanalysis.BaselineSnapshot{}, fmt.Errorf("prepare baseline report: %w", err)
	}
	snapshots := make([]fix.TargetSnapshot, len(request.Targets))
	for index, path := range request.Targets {
		file := files[path]
		snapshot, snapshotErr := targetSnapshot(path, before.files[path], file, mapper)
		if snapshotErr != nil {
			return fixanalysis.BaselineSnapshot{}, snapshotErr
		}
		for _, id := range request.RequiredMetrics {
			metric, ok := snapshot.Metrics[id]
			if !ok || !metric.Complete {
				return fixanalysis.BaselineSnapshot{}, fmt.Errorf("baseline target %q: required metric %q is missing or incomplete", path, id)
			}
		}
		if !snapshot.Complete {
			return fixanalysis.BaselineSnapshot{}, fmt.Errorf("baseline target %q is incomplete", path)
		}
		if err := requiredMetricsComplete(snapshot.Metrics, request.Goal); err != nil {
			return fixanalysis.BaselineSnapshot{}, fmt.Errorf("baseline target %q: %w", path, err)
		}
		snapshots[index] = snapshot
	}
	preparedAt := service.config.Clock()
	if !request.FreshBy.IsZero() && preparedAt.Before(request.FreshBy) {
		return fixanalysis.BaselineSnapshot{}, fmt.Errorf("baseline prepared at %s before required freshness %s", preparedAt, request.FreshBy)
	}
	contract := fix.ScoringContract{
		CatalogID: catalogID, ProfileSetHash: document.ProfileSetHash,
		Targets: snapshots, Goal: cloneGoal(request.Goal), RequireComplete: true,
	}
	return fixanalysis.BaselineSnapshot{
		Workspace: request.Workspace, Contract: cloneContract(contract),
		Fingerprint: before.aggregate, PreparedAt: preparedAt,
	}, nil
}
