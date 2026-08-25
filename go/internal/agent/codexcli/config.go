// Package codexcli adapts the stable Codex CLI non-interactive JSONL protocol
// to the provider-neutral agent contract.
package codexcli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/blater/slopwatch/internal/agent"
	"github.com/blater/slopwatch/internal/fix"
)

const (
	RuntimeKind       agent.RuntimeKind = "codex-cli"
	permissionProfile                   = "slopwatch"
)

func resolveExecutable(value string) (string, error) {
	if value == "" {
		value = "codex"
	}
	if !strings.ContainsRune(value, filepath.Separator) {
		resolved, err := exec.LookPath(value)
		if err != nil {
			return "", err
		}
		value = resolved
	}
	absolute, err := filepath.Abs(value)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(absolute)
	if err != nil {
		return "", err
	}
	if info.IsDir() || info.Mode()&0o111 == 0 {
		return "", fmt.Errorf("codex executable is not executable: %s", absolute)
	}
	return absolute, nil
}

func permissionArguments(profile agent.Profile, workspace fix.CandidateIdentity) ([]string, error) {
	root, err := canonicalAbsolute(workspace.RepositoryRoot)
	if err != nil {
		return nil, fmt.Errorf("candidate root: %w", err)
	}
	if root != filepath.Clean(workspace.RepositoryRoot) {
		return nil, fmt.Errorf("candidate root must be canonical")
	}
	common, err := canonicalAbsolute(workspace.GitCommonDir)
	if err != nil {
		return nil, fmt.Errorf("git common directory: %w", err)
	}
	if common != filepath.Clean(workspace.GitCommonDir) {
		return nil, fmt.Errorf("git common directory must be canonical")
	}
	denied := []string{common}
	if raw := profile.Options["denied_read_roots"]; raw != "" {
		for _, item := range filepath.SplitList(raw) {
			absolute, pathErr := canonicalAbsolute(item)
			if pathErr != nil {
				return nil, fmt.Errorf("denied read root: %w", pathErr)
			}
			denied = append(denied, absolute)
		}
	}
	sort.Strings(denied)
	filesystem := []string{
		strconv.Quote(":root") + "=" + strconv.Quote("deny"),
		strconv.Quote(":minimal") + "=" + strconv.Quote("read"),
		strconv.Quote(":tmpdir") + "=" + strconv.Quote("deny"),
		strconv.Quote(":slash_tmp") + "=" + strconv.Quote("deny"),
	}
	workspaceRules := []string{
		strconv.Quote(".") + "=" + strconv.Quote("write"),
		strconv.Quote(".git") + "=" + strconv.Quote("deny"),
	}
	for _, path := range denied {
		filesystem = append(filesystem, strconv.Quote(path)+"="+strconv.Quote("deny"))
	}
	filesystem = append(filesystem, strconv.Quote(":workspace_roots")+"={"+strings.Join(workspaceRules, ",")+"}")
	definition := "{workspace_roots={" + strconv.Quote(root) + "=true},filesystem={" +
		strings.Join(filesystem, ",") + "},network={enabled=false}}"
	return []string{
		"-c", "default_permissions=" + strconv.Quote(permissionProfile),
		"-c", "permissions." + permissionProfile + "=" + definition,
		"-c", "shell_environment_policy.inherit=" + strconv.Quote("none"),
	}, nil
}

func canonicalAbsolute(value string) (string, error) {
	if value == "" || !filepath.IsAbs(value) {
		return "", fmt.Errorf("absolute path required")
	}
	cleaned := filepath.Clean(value)
	resolved, err := filepath.EvalSymlinks(cleaned)
	if err != nil {
		return "", err
	}
	return resolved, nil
}

func transportEnvironment(getenv func(string) string) []string {
	keys := []string{
		"CODEX_HOME", "HOME", "HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY",
		"SSL_CERT_DIR", "SSL_CERT_FILE",
	}
	environment := []string{"LANG=C.UTF-8", "LC_ALL=C", "TERM=dumb"}
	path := getenv("PATH")
	if path == "" {
		path = "/usr/bin:/bin:/usr/sbin:/sbin"
	}
	environment = append(environment, "PATH="+path)
	for _, key := range keys {
		if value := getenv(key); value != "" {
			environment = append(environment, key+"="+value)
		}
	}
	return environment
}
