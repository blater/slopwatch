// Package naming creates human-readable random identifiers for Slopmochi.
package naming

import (
	"fmt"

	"github.com/blater/goname"
)

// New returns a prefixed, three-word name suitable for identifiers and paths.
func New(prefix string) (string, error) {
	options := goname.DefaultOptions()
	options.Words = 3
	options.MaxLetters = 8
	options.Complexity = goname.ComplexityLarge
	name, err := goname.GenerateWithOptions(options)
	if err != nil {
		return "", fmt.Errorf("create %s name: %w", prefix, err)
	}
	return prefix + "-" + name, nil
}
