package openairesponses

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/blater/slopwatch/internal/agent"
	"github.com/blater/slopwatch/internal/fix"
)

type candidateTools struct {
	canonicalRoot  string
	root           *os.Root
	stagingRoot    string
	staging        *os.Root
	targetManifest string
	allowed        map[fix.RepoPath]struct{}
	repository     bool
	config         resolvedConfig
}

type toolFault struct {
	code    string
	message string
}

func (fault *toolFault) Error() string { return fault.message }

func fault(code, message string) error { return &toolFault{code: code, message: message} }

func newCandidateTools(identity fix.CandidateIdentity, policy agent.WritePolicy, config resolvedConfig, manifest *agent.TargetManifest) (*candidateTools, error) {
	if identity.RepositoryRoot == "" || !filepath.IsAbs(identity.RepositoryRoot) || filepath.Clean(identity.RepositoryRoot) != identity.RepositoryRoot {
		return nil, errors.New("candidate repository root must be a clean absolute path")
	}
	canonical, err := filepath.EvalSymlinks(identity.RepositoryRoot)
	if err != nil || canonical != identity.RepositoryRoot {
		return nil, errors.New("candidate repository root must already be canonical")
	}
	info, err := os.Stat(canonical)
	if err != nil || !info.IsDir() {
		return nil, errors.New("candidate repository root is unavailable")
	}
	root, err := os.OpenRoot(canonical)
	if err != nil {
		return nil, errors.New("candidate repository root could not be opened")
	}
	if identity.StagingRoot == "" || !filepath.IsAbs(identity.StagingRoot) || filepath.Clean(identity.StagingRoot) != identity.StagingRoot {
		_ = root.Close()
		return nil, errors.New("candidate staging root is required")
	}
	stagingCanonical, err := filepath.EvalSymlinks(identity.StagingRoot)
	if err != nil || stagingCanonical != identity.StagingRoot || withinPath(canonical, stagingCanonical) {
		_ = root.Close()
		return nil, errors.New("candidate staging root must be private and outside the candidate")
	}
	stagingInfo, err := os.Stat(stagingCanonical)
	if err != nil || !stagingInfo.IsDir() || stagingInfo.Mode().Perm() != 0o700 {
		_ = root.Close()
		return nil, errors.New("candidate staging root is unavailable or not private")
	}
	staging, err := os.OpenRoot(stagingCanonical)
	if err != nil {
		_ = root.Close()
		return nil, errors.New("candidate staging root could not be opened")
	}
	result := &candidateTools{
		canonicalRoot: canonical, root: root, stagingRoot: stagingCanonical, staging: staging,
		allowed: make(map[fix.RepoPath]struct{}, len(policy.Allowed)), repository: policy.Scope == "repository", config: config,
	}
	if err := result.configureManifest(manifest); err != nil {
		_ = root.Close()
		_ = staging.Close()
		return nil, err
	}
	for _, allowed := range policy.Allowed {
		parsed, parseErr := fix.ParseRepoPath(allowed.String())
		if parseErr != nil || forbiddenGitPath(parsed.String()) {
			_ = root.Close()
			_ = staging.Close()
			return nil, errors.New("write policy contains an invalid path")
		}
		result.allowed[parsed] = struct{}{}
	}
	return result, nil
}

func (tools *candidateTools) configureManifest(manifest *agent.TargetManifest) error {
	if manifest == nil {
		return nil
	}
	if manifest.Count <= 0 || manifest.Path == "" || !filepath.IsAbs(manifest.Path) || filepath.Clean(manifest.Path) != manifest.Path || !withinPath(tools.stagingRoot, manifest.Path) {
		return errors.New("target manifest must be a file in candidate staging")
	}
	canonical, canonicalErr := filepath.EvalSymlinks(manifest.Path)
	info, statErr := os.Stat(manifest.Path)
	if canonicalErr != nil || canonical != manifest.Path || statErr != nil || !info.Mode().IsRegular() {
		return errors.New("target manifest is unavailable")
	}
	relative, relativeErr := filepath.Rel(tools.stagingRoot, manifest.Path)
	if relativeErr != nil {
		return errors.New("target manifest path is invalid")
	}
	tools.targetManifest = filepath.ToSlash(relative)
	return nil
}
