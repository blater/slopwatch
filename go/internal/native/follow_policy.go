package native

import (
	"context"
	"github.com/blater/slopwatch/internal/sourceignore"
)

type followMatcherKey struct{}

// WithFollowMatcher carries the monitor's frozen startup policy into analysis.
// Ordinary one-shot callers continue to load and validate their own policy.
func WithFollowMatcher(ctx context.Context, matcher *sourceignore.Matcher) context.Context {
	return context.WithValue(ctx, followMatcherKey{}, matcher)
}

// ContextSourceEligible uses the backend's context filtering, independently of
// the stricter report discovery rules for test-named directories.
func ContextSourceEligible(path, language string, includeTests bool) bool {
	return includeTests || !isTestPath(path, language)
}
