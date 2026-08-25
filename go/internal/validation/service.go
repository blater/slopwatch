// Package validation defines trusted, non-shell validation plans and their
// execution port. Repository preferences may select plan IDs but cannot create
// executable definitions.
package validation

import (
	"context"
	"time"

	"github.com/blater/slopwatch/internal/fix"
)

type CheckID string

type Check struct {
	ID               CheckID
	Label            string
	Executable       string
	Arguments        []string
	WorkingDirectory fix.RepoPath
	Required         bool
	Timeout          time.Duration
	MaxOutputBytes   int64
}

type Plan struct {
	ID     string
	Checks []Check
}

type CheckResult struct {
	ID         CheckID
	StartedAt  time.Time
	FinishedAt time.Time
	ExitCode   int
	Passed     bool
	Output     string
	Truncated  bool
	Diagnostic string
}

type Result struct {
	Checks            []CheckResult
	FingerprintBefore string
	FingerprintAfter  string
	Passed            bool
	Diagnostic        string
}

func (result Result) Stable() bool {
	return result.FingerprintBefore != "" && result.FingerprintBefore == result.FingerprintAfter
}

type Service interface {
	Validate(context.Context, fix.CandidateIdentity, Plan) (Result, error)
}
