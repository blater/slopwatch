package naming

import (
	"strings"
	"testing"
)

func TestNewReturnsPrefixedThreeWordName(t *testing.T) {
	value, err := New("job")
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(value, "-")
	if len(parts) != 4 || parts[0] != "job" {
		t.Fatalf("New(job) = %q, want job plus three words", value)
	}
	for _, part := range parts[1:] {
		if part == "" || len([]rune(part)) > 8 {
			t.Fatalf("New(job) = %q, invalid word %q", value, part)
		}
	}
}
