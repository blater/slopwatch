package fixapp

import (
	"github.com/blater/slopwatch/internal/fix"
	"github.com/blater/slopwatch/internal/fixanalysis"
)

func applyVerifiedFiles(record *jobRecord, verification fixanalysis.VerificationResult) {
	for index := range record.presentation.Targets {
		verified, ok := verifiedFile(verification, record.presentation.Targets[index].Path)
		if !ok {
			continue
		}
		score := verified.Score
		record.presentation.Targets[index].VerifiedScore = &score
		record.presentation.Targets[index].Verification = verified.Diagnostic
		record.presentation.Targets[index].VerifiedMetrics = metricValues(verified.Metrics)
	}
}

func verifiedFile(verification fixanalysis.VerificationResult, path fix.RepoPath) (fixanalysis.FileResult, bool) {
	for _, verified := range verification.Files {
		if verified.Path == path {
			return verified, true
		}
	}
	return fixanalysis.FileResult{}, false
}
