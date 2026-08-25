package validation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/blater/slopwatch/internal/fix"
	"github.com/blater/slopwatch/internal/isolation"
)

const defaultValidationOutput = int64(1 << 20)

// Runner executes only pre-registered executable-plus-argv checks. Repository
// settings may select a Plan ID, but must never construct these definitions.
type Runner struct {
	executor Executor
	environ  func() []string
}

// Executor is deliberately deeper than the generic process lifecycle port.
// Implementations must prove confinement before launching the executable.
type Executor interface {
	RunValidation(context.Context, fix.CandidateIdentity, isolation.Request) (isolation.Result, isolation.Conformance, error)
}

type ExecutorFunc func(context.Context, fix.CandidateIdentity, isolation.Request) (isolation.Result, isolation.Conformance, error)

func (function ExecutorFunc) RunValidation(ctx context.Context, candidate fix.CandidateIdentity, request isolation.Request) (isolation.Result, isolation.Conformance, error) {
	return function(ctx, candidate, request)
}

type DenyAllExecutor struct{ Diagnostic string }

func (executor DenyAllExecutor) RunValidation(context.Context, fix.CandidateIdentity, isolation.Request) (isolation.Result, isolation.Conformance, error) {
	diagnostic := executor.Diagnostic
	if diagnostic == "" {
		diagnostic = "validation confinement has not been proven on this platform"
	}
	return isolation.Result{}, isolation.Conformance{Diagnostic: diagnostic}, errors.New(diagnostic)
}

func NewRunner(executor Executor) (*Runner, error) {
	if executor == nil {
		return nil, errors.New("validation runner requires a process executor")
	}
	return &Runner{executor: executor, environ: validationEnvironment}, nil
}

func (runner *Runner) Validate(ctx context.Context, candidate fix.CandidateIdentity, plan Plan) (Result, error) {
	if plan.ID == "" || candidate.RepositoryRoot == "" || !filepath.IsAbs(candidate.RepositoryRoot) {
		return Result{}, errors.New("validation candidate and plan are required")
	}
	canonicalRoot, err := filepath.EvalSymlinks(candidate.RepositoryRoot)
	if err != nil {
		return Result{}, fmt.Errorf("canonicalize validation candidate: %w", err)
	}
	candidate.RepositoryRoot = canonicalRoot
	before, err := treeFingerprint(candidate.RepositoryRoot)
	if err != nil {
		return Result{}, fmt.Errorf("fingerprint candidate before validation: %w", err)
	}
	result := Result{FingerprintBefore: before, Passed: true}
	for _, check := range plan.Checks {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		checkResult, runErr := runner.runCheck(ctx, candidate, check)
		result.Checks = append(result.Checks, checkResult)
		if runErr != nil {
			return result, runErr
		}
		if check.Required && !checkResult.Passed {
			result.Passed = false
		}
	}
	after, err := treeFingerprint(candidate.RepositoryRoot)
	if err != nil {
		return result, fmt.Errorf("fingerprint candidate after validation: %w", err)
	}
	result.FingerprintAfter = after
	if before != after {
		result.Passed = false
		result.Diagnostic = "candidate changed while validation was running"
	}
	return result, nil
}

func (runner *Runner) runCheck(ctx context.Context, candidate fix.CandidateIdentity, check Check) (CheckResult, error) {
	started := time.Now()
	result := CheckResult{ID: check.ID, StartedAt: started, ExitCode: -1}
	if check.ID == "" || check.Executable == "" || !filepath.IsAbs(check.Executable) {
		return result, errors.New("validation check requires an ID and absolute executable")
	}
	working := candidate.RepositoryRoot
	if check.WorkingDirectory != "" {
		if _, err := fix.ParseRepoPath(check.WorkingDirectory.String()); err != nil {
			return result, fmt.Errorf("validation check %s working directory: %w", check.ID, err)
		}
		working = filepath.Join(working, filepath.FromSlash(check.WorkingDirectory.String()))
	}
	working, err := filepath.EvalSymlinks(working)
	if err != nil {
		return result, fmt.Errorf("validation check %s working directory: %w", check.ID, err)
	}
	if !isWithin(candidate.RepositoryRoot, working) {
		return result, fmt.Errorf("validation check %s working directory escapes candidate", check.ID)
	}
	timeout := check.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Minute
	}
	outputLimit := check.MaxOutputBytes
	if outputLimit <= 0 {
		outputLimit = defaultValidationOutput
	}
	run, conformance, err := runner.executor.RunValidation(ctx, candidate, isolation.Request{Executable: check.Executable, Arguments: append([]string(nil), check.Arguments...),
		Directory: working, Environment: runner.environ(), Limits: isolation.Limits{WallTime: timeout, TerminateGrace: 2 * time.Second, MaxStdoutBytes: outputLimit, MaxStderrBytes: outputLimit}})
	result.FinishedAt = time.Now()
	if err != nil {
		result.Diagnostic = err.Error()
		return result, fmt.Errorf("validation check %s: %w", check.ID, err)
	}
	if !validationEligible(conformance) {
		result.Diagnostic = fmt.Sprintf("validation confinement failed: %v", conformance.FailedGates())
		return result, errors.New(result.Diagnostic)
	}
	result.ExitCode = run.ExitCode
	result.Truncated = run.StdoutTruncated || run.StderrTruncated
	result.Output = boundedCombined(run.Stdout, run.Stderr, outputLimit)
	result.Passed = run.Successful() && !result.Truncated
	if run.TimedOut {
		result.Diagnostic = "validation timed out"
	} else if run.Canceled {
		result.Diagnostic = "validation canceled"
	} else if result.Truncated {
		result.Diagnostic = "validation output exceeded its limit"
	} else if run.ExitCode != 0 {
		result.Diagnostic = fmt.Sprintf("validation exited with status %d", run.ExitCode)
	}
	return result, nil
}

func validationEligible(value isolation.Conformance) bool {
	return value.CandidateWrite && value.OutsideWriteDenied && value.GitMetadataDenied &&
		value.SensitiveReadsDenied && value.ToolNetworkPolicy && value.CrashContainment
}

func validationEnvironment() []string {
	path := os.Getenv("PATH")
	if path == "" {
		path = "/usr/bin:/bin:/usr/sbin:/sbin"
	}
	return []string{"LANG=C.UTF-8", "LC_ALL=C", "PATH=" + path, "CI=1", "GIT_TERMINAL_PROMPT=0"}
}

func boundedCombined(stdout, stderr []byte, limit int64) string {
	combined := append(append([]byte(nil), stdout...), stderr...)
	if int64(len(combined)) > limit {
		combined = combined[:limit]
	}
	return string(combined)
}

func treeFingerprint(root string) (string, error) {
	type treeEntry struct {
		path, kind string
		mode       uint32
	}
	var entries []treeEntry
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Name() == ".git" {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if path == root || entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return errors.New("validation fingerprint escaped candidate")
		}
		item := treeEntry{path: filepath.ToSlash(relative)}
		switch mode := info.Mode(); {
		case mode.IsRegular():
			item.kind, item.mode = "regular", 0o100644
			if mode.Perm()&0o111 != 0 {
				item.mode = 0o100755
			}
		case mode&os.ModeSymlink != 0:
			item.kind, item.mode = "symlink", 0o120000
		default:
			return fmt.Errorf("validation fingerprint rejects special file %q", relative)
		}
		entries = append(entries, item)
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].path < entries[j].path })
	rootHandle, err := os.OpenRoot(root)
	if err != nil {
		return "", err
	}
	defer rootHandle.Close()
	hasher := sha256.New()
	for _, entry := range entries {
		var contents []byte
		if entry.kind == "symlink" {
			target, err := rootHandle.Readlink(entry.path)
			if err != nil {
				return "", err
			}
			contents = []byte(target)
		} else {
			contents, err = rootHandle.ReadFile(entry.path)
			if err != nil {
				return "", err
			}
		}
		_, _ = hasher.Write([]byte(entry.path))
		_, _ = hasher.Write([]byte{0})
		_, _ = hasher.Write([]byte(entry.kind))
		_, _ = hasher.Write([]byte{0, byte(entry.mode >> 16), byte(entry.mode >> 8), byte(entry.mode)})
		_, _ = hasher.Write(contents)
		_, _ = hasher.Write([]byte{0})
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

func isWithin(root, path string) bool {
	relative, err := filepath.Rel(root, path)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}
