// Package fixanalysis defines the analysis port consumed by fix orchestration.
// Concrete analyzer/report/cache types stay behind an adapter.
package fixanalysis

import (
	"context"
	"time"

	"github.com/blater/slopwatch/internal/fix"
)

type BaselineRequest struct {
	Workspace       fix.WorkspaceIdentity
	Targets         []fix.RepoPath
	Goal            fix.ScoringGoal
	RequiredMetrics []fix.MetricID
	FreshBy         time.Time
}

type BaselineSnapshot struct {
	Workspace   fix.WorkspaceIdentity
	Contract    fix.ScoringContract
	Fingerprint string
	PreparedAt  time.Time
}

type VerificationRequest struct {
	Candidate fix.CandidateIdentity
	Contract  fix.ScoringContract
}

type FileResult struct {
	Path       fix.RepoPath
	Score      float64
	Metrics    map[fix.MetricID]fix.MetricValue
	Complete   bool
	Compliant  bool
	Diagnostic string
}

type VerificationResult struct {
	Files             []FileResult
	FingerprintBefore string
	FingerprintAfter  string
	Complete          bool
	Compliant         bool
	Diagnostic        string
}

func (result VerificationResult) Stable() bool {
	return result.FingerprintBefore != "" && result.FingerprintBefore == result.FingerprintAfter
}

type Service interface {
	PrepareBaseline(context.Context, BaselineRequest) (BaselineSnapshot, error)
	Verify(context.Context, VerificationRequest) (VerificationResult, error)
}
