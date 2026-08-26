package fix

import (
	"errors"
	"strings"
	"testing"
)

func TestParseRepoPath(t *testing.T) {
	t.Parallel()
	valid := []string{"main.go", "internal/service.go", "a b/c.go"}
	for _, value := range valid {
		value := value
		t.Run("valid_"+strings.ReplaceAll(value, "/", "_"), func(t *testing.T) {
			t.Parallel()
			got, err := ParseRepoPath(value)
			if err != nil || got.String() != value {
				t.Fatalf("ParseRepoPath(%q) = %q, %v", value, got, err)
			}
		})
	}
	invalid := []string{"", ".", "..", "../x", "a/../x", "/tmp/x", `a\\b`, "a//b", "a/./b", "x\x00y"}
	for _, value := range invalid {
		value := value
		t.Run("invalid_"+strings.ReplaceAll(value, "/", "_"), func(t *testing.T) {
			t.Parallel()
			_, err := ParseRepoPath(value)
			if !errors.Is(err, ErrInvalidRepoPath) {
				t.Fatalf("ParseRepoPath(%q) error = %v", value, err)
			}
		})
	}
}

func TestIDsAreTypedAndUnique(t *testing.T) {
	t.Parallel()
	first, err := NewJobID()
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewJobID()
	if err != nil {
		t.Fatal(err)
	}
	if first == second || !strings.HasPrefix(string(first), "job-") {
		t.Fatalf("unexpected ids %q and %q", first, second)
	}
	parts := strings.Split(string(first), "-")
	if len(parts) != 4 {
		t.Fatalf("job ID %q does not contain a three-word goname", first)
	}
}
