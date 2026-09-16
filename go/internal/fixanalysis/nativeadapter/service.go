// Package nativeadapter is the sole translation boundary between native
// analysis reports and the provider-neutral fix analysis service.
package nativeadapter

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/blater/slopwatch/internal/fix"
	"github.com/blater/slopwatch/internal/fixanalysis"
	"github.com/blater/slopwatch/internal/native"
	"github.com/blater/slopwatch/internal/report"
)

type Config struct {
	InstallationRoot  string
	Languages         []string
	IncludeTests      bool
	TypeScriptTypes   bool
	FollowSymlinks    bool
	BaselineReadCache bool
	Clock             func() time.Time
}

// AnalyzerOptions is the construction snapshot supplied to Factory. Final
// verification always receives ReadCache=false regardless of Config.
type AnalyzerOptions struct {
	Languages       []string
	IncludeTests    bool
	TypeScriptTypes bool
	FollowSymlinks  bool
	ReadCache       bool
}

type Analyzer interface {
	Analyze(context.Context, []string, []string) (report.Document, error)
	ScoringIdentity() (string, error)
}

type Factory interface {
	New(workspace, installationRoot string, options AnalyzerOptions) (Analyzer, error)
}

type FactoryFunc func(workspace, installationRoot string, options AnalyzerOptions) (Analyzer, error)

func (function FactoryFunc) New(workspace, installationRoot string, options AnalyzerOptions) (Analyzer, error) {
	return function(workspace, installationRoot, options)
}

type Service struct {
	config  Config
	factory Factory
}

var _ fixanalysis.Service = (*Service)(nil)

func New(config Config) (*Service, error) {
	return NewWithFactory(config, nativeFactory{})
}

func NewWithFactory(config Config, factory Factory) (*Service, error) {
	if factory == nil {
		return nil, errors.New("native analysis adapter requires an analyzer factory")
	}
	if config.InstallationRoot == "" {
		return nil, errors.New("native analysis adapter requires an installation root")
	}
	config.Languages = append([]string(nil), config.Languages...)
	if config.Clock == nil {
		config.Clock = time.Now
	}
	return &Service{config: config, factory: factory}, nil
}

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

func (service *Service) Verify(ctx context.Context, request fixanalysis.VerificationRequest) (fixanalysis.VerificationResult, error) {
	if err := validateWorkspace(request.Candidate.RepositoryRoot, request.Candidate.AnalysisRoot); err != nil {
		return fixanalysis.VerificationResult{}, fmt.Errorf("verify candidate workspace: %w", err)
	}
	paths := contractPaths(request.Contract)
	if err := validateTargets(paths); err != nil {
		return fixanalysis.VerificationResult{}, fmt.Errorf("verify contract targets: %w", err)
	}
	if err := validateGoal(request.Contract.Goal); err != nil {
		return fixanalysis.VerificationResult{}, fmt.Errorf("verify contract goal: %w", err)
	}
	mapper, err := newPathMapper(request.Candidate.RepositoryRoot, request.Candidate.AnalysisRoot)
	if err != nil {
		return fixanalysis.VerificationResult{}, err
	}
	targets, err := mapper.analysisTargets(paths)
	if err != nil {
		return fixanalysis.VerificationResult{}, err
	}
	before, err := fingerprintTargets(request.Candidate.RepositoryRoot, paths)
	if err != nil {
		return fixanalysis.VerificationResult{}, fmt.Errorf("fingerprint candidate targets: %w", err)
	}
	document, catalogID, err := service.analyze(ctx, mapper.analysisRoot, targets, false)
	if err != nil {
		return fixanalysis.VerificationResult{}, fmt.Errorf("analyze candidate: %w", err)
	}
	after, err := fingerprintTargets(request.Candidate.RepositoryRoot, paths)
	if err != nil {
		return fixanalysis.VerificationResult{}, fmt.Errorf("fingerprint candidate targets after analysis: %w", err)
	}
	result := fixanalysis.VerificationResult{FingerprintBefore: before.aggregate, FingerprintAfter: after.aggregate}
	if catalogID != request.Contract.CatalogID {
		result.Diagnostic = fmt.Sprintf("analysis catalog changed: contract %q, candidate %q", request.Contract.CatalogID, catalogID)
		return result, nil
	}
	if document.ProfileSetHash != request.Contract.ProfileSetHash {
		result.Diagnostic = fmt.Sprintf("analysis profile changed: contract %q, candidate %q", request.Contract.ProfileSetHash, document.ProfileSetHash)
		return result, nil
	}
	files, err := exactFiles(document, mapper, paths)
	if err != nil {
		result.Diagnostic = err.Error()
		return result, nil
	}
	result.Files = make([]fixanalysis.FileResult, len(request.Contract.Targets))
	result.Complete = true
	result.TargetMet = true
	for index, baseline := range request.Contract.Targets {
		fileResult, convertErr := verifyFile(baseline, files[baseline.Path], request.Contract.Goal, request.Contract.RequireComplete)
		if convertErr != nil {
			return fixanalysis.VerificationResult{}, convertErr
		}
		result.Files[index] = fileResult
		result.Complete = result.Complete && fileResult.Complete
		result.TargetMet = result.TargetMet && fileResult.TargetMet
	}
	if before.aggregate != after.aggregate {
		result.Complete = false
		result.TargetMet = false
		result.Diagnostic = "candidate targets changed during analysis"
	}
	return cloneVerification(result), nil
}

func (service *Service) analyze(ctx context.Context, analysisRoot string, targets []string, readCache bool) (report.Document, string, error) {
	options := AnalyzerOptions{
		Languages:    append([]string(nil), service.config.Languages...),
		IncludeTests: service.config.IncludeTests, TypeScriptTypes: service.config.TypeScriptTypes,
		FollowSymlinks: service.config.FollowSymlinks, ReadCache: readCache,
	}
	analyzer, err := service.factory.New(analysisRoot, service.config.InstallationRoot, options)
	if err != nil {
		return report.Document{}, "", err
	}
	identity, err := analyzer.ScoringIdentity()
	if err != nil {
		return report.Document{}, "", err
	}
	if identity == "" {
		return report.Document{}, "", errors.New("analyzer returned an empty scoring identity")
	}
	document, err := analyzer.Analyze(ctx, append([]string(nil), targets...), append([]string(nil), options.Languages...))
	if err != nil {
		return report.Document{}, "", err
	}
	if document.SchemaVersion <= 0 {
		return report.Document{}, "", errors.New("analyzer returned an invalid report schema version")
	}
	if document.ProfileSetHash == "" {
		return report.Document{}, "", errors.New("analyzer returned an empty profile-set identity")
	}
	if !document.Calibrated {
		return report.Document{}, "", errors.New("analyzer returned an uncalibrated report")
	}
	identity = fmt.Sprintf("%s/report-schema-%d", identity, document.SchemaVersion)
	return document, identity, nil
}

type nativeFactory struct{}

func (nativeFactory) New(workspace, installationRoot string, options AnalyzerOptions) (Analyzer, error) {
	return native.New(workspace, installationRoot, native.Options{
		Languages: append([]string(nil), options.Languages...), IncludeTests: options.IncludeTests,
		TypeScriptTypes: options.TypeScriptTypes, FollowSymlinks: options.FollowSymlinks,
		ReadCache: options.ReadCache,
	})
}
