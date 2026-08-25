package validation

import (
	"context"
	"errors"

	"github.com/blater/slopwatch/internal/fix"
	"github.com/blater/slopwatch/internal/isolation"
)

// ConfiningExecutor adapts the shared platform confinement boundary to
// validation without exposing provider/runtime details to validation plans.
type ConfiningExecutor struct {
	Confinement    isolation.CandidateConfinement
	SensitiveRoots []string
}

func (executor ConfiningExecutor) Readiness(ctx context.Context) Readiness {
	if executor.Confinement == nil {
		return Readiness{Diagnostic: "validation confinement service is unavailable"}
	}
	capability := executor.Confinement.Capability(ctx)
	return Readiness{Ready: capability.Available && capability.CrashContainment, Diagnostic: capability.Diagnostic}
}

func (executor ConfiningExecutor) ExecutableReadiness(ctx context.Context, executables []string) error {
	checker, ok := executor.Confinement.(interface {
		ExecutableReady(context.Context, string) error
	})
	if !ok {
		return errors.New("validation confinement does not prove in-image executables")
	}
	for _, executable := range executables {
		if err := checker.ExecutableReady(ctx, executable); err != nil {
			return err
		}
	}
	return nil
}

func (executor ConfiningExecutor) RunValidation(ctx context.Context, candidate fix.CandidateIdentity, request isolation.Request) (isolation.Result, isolation.Conformance, error) {
	policy := isolation.CandidatePolicy{CandidateRoot: candidate.RepositoryRoot, GitCommonDir: candidate.GitCommonDir, SensitiveRoots: append([]string(nil), executor.SensitiveRoots...)}
	return executor.Confinement.RunCandidate(ctx, policy, request)
}
