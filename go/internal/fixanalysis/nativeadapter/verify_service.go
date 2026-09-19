package nativeadapter

import (
	"context"
	"fmt"

	"github.com/blater/slopwatch/internal/fixanalysis"
)

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
		fileResult, convertErr := verifyFile(baseline, files[baseline.Path], document.Depth, request.Contract.Goal, request.Contract.RequireComplete)
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
