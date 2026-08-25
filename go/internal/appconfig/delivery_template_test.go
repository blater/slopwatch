package appconfig

import (
	"strings"
	"testing"
)

func TestValidateBranchTemplateAcceptsLongOrganisationConvention(t *testing.T) {
	template := strings.Repeat("organisation/platform/", 16) + "{target-stem}-{job-short-id}"
	if len(template) <= 240 {
		t.Fatalf("test template length = %d, want more than former ceiling", len(template))
	}
	if err := ValidateBranchTemplate(template); err != nil {
		t.Fatalf("ValidateBranchTemplate rejected organisation convention: %v", err)
	}
}

func TestValidateBranchTemplateStillRejectsUnsafeStructure(t *testing.T) {
	for _, template := range []string{"", "org/fix\nnext", "org/{unknown}", "org/{target-stem"} {
		if err := ValidateBranchTemplate(template); err == nil {
			t.Fatalf("ValidateBranchTemplate(%q) unexpectedly succeeded", template)
		}
	}
}
