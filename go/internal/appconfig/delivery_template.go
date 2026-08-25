package appconfig

import (
	"errors"
	"strings"
)

var branchTemplateTokens = map[string]struct{}{
	"target-stem":  {},
	"job-short-id": {},
	"date":         {},
	"metrics":      {},
}

// ValidateBranchTemplate rejects unknown or malformed substitutions. A job ID
// token is available in the suggested default but is not mandatory: users may
// follow an organisation-owned naming convention, and delivery preflight
// reports any actual local or remote collision.
func ValidateBranchTemplate(value string) error {
	if strings.TrimSpace(value) == "" || strings.ContainsAny(value, "\x00\r\n") {
		return errors.New("branch template must be non-empty and single-line")
	}
	remaining := value
	for {
		open := strings.IndexByte(remaining, '{')
		close := strings.IndexByte(remaining, '}')
		if open < 0 && close < 0 {
			break
		}
		if open < 0 || close < open {
			return errors.New("branch template contains unmatched braces")
		}
		token := remaining[open+1 : close]
		if _, ok := branchTemplateTokens[token]; !ok {
			return errors.New("branch template contains an unsupported token")
		}
		remaining = remaining[close+1:]
	}
	if strings.ContainsAny(remaining, "{}") {
		return errors.New("branch template contains unmatched braces")
	}
	return nil
}

func PreviewBranchTemplate(value string) string {
	return strings.NewReplacer("{target-stem}", "widget", "{job-short-id}", "a1b2c3d4", "{date}", "20260825", "{metrics}", "score").Replace(value)
}
