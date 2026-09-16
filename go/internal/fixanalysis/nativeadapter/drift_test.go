package nativeadapter

import (
	"context"
	"strings"
	"testing"

	"github.com/blater/slopwatch/internal/fixanalysis"
	"github.com/blater/slopwatch/internal/report"
)

func TestVerifyRejectsCatalogOrProfileDriftWithoutAcceptingReport(t *testing.T) {
	t.Parallel()
	for name, values := range map[string][3]string{
		"catalog": {"catalog-v2", "profile-v1", "catalog changed"},
		"profile": {"catalog-v1", "profile-v2", "profile changed"},
	} {
		catalog, profile, detail := values[0], values[1], values[2]
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			candidate := makeCandidate(t, root, "candidate")
			document := testDocument("a.go", 70, 7, 5)
			document.ProfileSetHash = profile
			factory := &fakeFactory{catalogs: []string{catalog}, documents: []report.Document{document}}
			result, err := mustService(t, factory).Verify(context.Background(), fixanalysis.VerificationRequest{
				Candidate: candidate, Contract: testContract("go/a.go"),
			})
			if err != nil || result.TargetMet || !strings.Contains(result.Diagnostic, detail) {
				t.Fatalf("Verify() = %#v, %v", result, err)
			}
		})
	}
}
