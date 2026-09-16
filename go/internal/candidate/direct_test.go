package candidate

import (
	"path/filepath"
	"testing"
)

func TestDirectCandidateAllowsSupportingRefactorsAndNeverRollsBackCurrentFiles(t *testing.T) {
	workspace, allowed, other := directFixture(t)
	service, err := NewDirectService(filepath.Join(t.TempDir(), "current"))
	if err != nil {
		t.Fatal(err)
	}
	identity := prepareDirectFixture(t, service, workspace)
	writeDirectFile(t, allowed, "package fixed\n")
	writeDirectFile(t, other, "package changed_elsewhere\n")
	diff, err := service.Diff(t.Context(), identity)
	if err != nil {
		t.Fatal(err)
	}
	assertDirectDiff(t, diff)
	if err := service.Discard(t.Context(), identity); err != nil {
		t.Fatal(err)
	}
	assertDirectFileUnchangedByDiscard(t, allowed)
}
