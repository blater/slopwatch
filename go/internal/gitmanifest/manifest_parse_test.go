package gitmanifest

import "testing"

func TestBuildRejectsTraversalAndMalformedStatus(t *testing.T) {
	for _, status := range [][]byte{[]byte("?? ../escape\x00"), []byte("M  file"), []byte("R  new\x00")} {
		if _, err := Build(t.TempDir(), status); err == nil {
			t.Fatalf("accepted %q", status)
		}
	}
}
