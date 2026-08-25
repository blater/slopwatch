package validation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/blater/slopwatch/internal/fix"
	"github.com/blater/slopwatch/internal/isolation"
)

// Runner executes only pre-registered executable-plus-argv checks. Repository
// settings may select a Plan ID, but must never construct these definitions.
type Runner struct {
	executor    Executor
	environment []string
	workspace   isolation.WorkspaceLimits
}

type RunnerConfig struct {
	// Environment is the installation-owned environment inside the immutable
	// validation image. It is never derived from the host process or repository.
	Environment []string
	// WorkspaceLimits are visible installation/user policy and are propagated
	// unchanged to the confined copy boundary.
	WorkspaceLimits isolation.WorkspaceLimits
}

// Executor is deliberately deeper than the generic process lifecycle port.
// Implementations must prove confinement before launching the executable.
type Executor interface {
	RunValidation(context.Context, fix.CandidateIdentity, isolation.Request) (isolation.Result, isolation.Conformance, error)
}

type readinessExecutor interface {
	Readiness(context.Context) Readiness
}

type executableReadinessExecutor interface {
	ExecutableReadiness(context.Context, []string) error
}

type ExecutorFunc func(context.Context, fix.CandidateIdentity, isolation.Request) (isolation.Result, isolation.Conformance, error)

func (function ExecutorFunc) RunValidation(ctx context.Context, candidate fix.CandidateIdentity, request isolation.Request) (isolation.Result, isolation.Conformance, error) {
	return function(ctx, candidate, request)
}
func (ExecutorFunc) Readiness(context.Context) Readiness                 { return Readiness{Ready: true} }
func (ExecutorFunc) ExecutableReadiness(context.Context, []string) error { return nil }

type DenyAllExecutor struct{ Diagnostic string }

func (executor DenyAllExecutor) Readiness(context.Context) Readiness {
	diagnostic := executor.Diagnostic
	if diagnostic == "" {
		diagnostic = "validation confinement has not been proven on this platform"
	}
	return Readiness{Diagnostic: diagnostic}
}

func (executor DenyAllExecutor) RunValidation(context.Context, fix.CandidateIdentity, isolation.Request) (isolation.Result, isolation.Conformance, error) {
	diagnostic := executor.Diagnostic
	if diagnostic == "" {
		diagnostic = "validation confinement has not been proven on this platform"
	}
	return isolation.Result{}, isolation.Conformance{Diagnostic: diagnostic}, errors.New(diagnostic)
}

func NewRunner(executor Executor, configs ...RunnerConfig) (*Runner, error) {
	if executor == nil {
		return nil, errors.New("validation runner requires a process executor")
	}
	if len(configs) != 1 {
		return nil, errors.New("validation runner requires one explicit typed configuration")
	}
	config := configs[0]
	if len(config.Environment) == 0 {
		return nil, errors.New("validation runner requires an installation-owned environment")
	}
	for _, item := range config.Environment {
		if item == "" || strings.ContainsAny(item, "\x00\r\n") || !strings.Contains(item, "=") {
			return nil, errors.New("validation runner environment is invalid")
		}
	}
	if err := config.WorkspaceLimits.Validate(); err != nil {
		return nil, fmt.Errorf("validation runner workspace limits: %w", err)
	}
	return &Runner{executor: executor, environment: append([]string(nil), config.Environment...), workspace: config.WorkspaceLimits}, nil
}

func (runner *Runner) Preflight(ctx context.Context, _ fix.WorkspaceIdentity, plan Plan) Readiness {
	result := Readiness{Required: plan.ID != ""}
	if plan.ID == "" {
		result.Ready = true
		return result
	}
	if len(plan.Checks) == 0 {
		result.Diagnostic = fmt.Sprintf("validation plan %q has no checks", plan.ID)
		return result
	}
	for _, check := range plan.Checks {
		if err := validateCheck(check); err != nil {
			result.Diagnostic = err.Error()
			return result
		}
	}
	checker, ok := runner.executor.(readinessExecutor)
	if !ok {
		result.Diagnostic = "validation executor does not report confinement readiness"
		return result
	}
	ready := checker.Readiness(ctx)
	ready.Required = true
	if !ready.Ready && ready.Diagnostic == "" {
		ready.Diagnostic = "validation executor is unavailable"
	}
	if !ready.Ready {
		return ready
	}
	executableChecker, ok := runner.executor.(executableReadinessExecutor)
	if !ok {
		return Readiness{Required: true, Diagnostic: "validation executor does not prove in-image executables"}
	}
	executables := make([]string, 0, len(plan.Checks))
	for _, check := range plan.Checks {
		executables = append(executables, check.Executable)
	}
	if err := executableChecker.ExecutableReadiness(ctx, executables); err != nil {
		return Readiness{Required: true, Diagnostic: err.Error()}
	}
	return ready
}

func (runner *Runner) Validate(ctx context.Context, candidate fix.CandidateIdentity, plan Plan) (Result, error) {
	if plan.ID == "" || candidate.RepositoryRoot == "" || !filepath.IsAbs(candidate.RepositoryRoot) {
		return Result{}, errors.New("validation candidate and plan are required")
	}
	if len(plan.Checks) == 0 {
		return Result{}, fmt.Errorf("validation plan %q has no checks", plan.ID)
	}
	for _, check := range plan.Checks {
		if err := validateCheck(check); err != nil {
			return Result{}, err
		}
	}
	canonicalRoot, err := filepath.EvalSymlinks(candidate.RepositoryRoot)
	if err != nil {
		return Result{}, fmt.Errorf("canonicalize validation candidate: %w", err)
	}
	candidate.RepositoryRoot = canonicalRoot
	before, err := treeFingerprint(ctx, candidate.RepositoryRoot, runner.workspace)
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
	after, err := treeFingerprint(ctx, candidate.RepositoryRoot, runner.workspace)
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
	if err := validateCheck(check); err != nil {
		return result, err
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
	run, conformance, err := runner.executor.RunValidation(ctx, candidate, isolation.Request{Executable: check.Executable, Arguments: append([]string(nil), check.Arguments...),
		Directory: working, Environment: append([]string(nil), runner.environment...), Limits: isolation.Limits{WallTime: check.Timeout, TerminateGrace: 2 * time.Second, MaxStdoutBytes: check.MaxOutputBytes, MaxStderrBytes: check.MaxOutputBytes}, WorkspaceLimits: runner.workspace})
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
	result.Output = boundedCombined(run.Stdout, run.Stderr, check.MaxOutputBytes)
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

func validateCheck(check Check) error {
	if check.ID == "" || check.Executable == "" || !filepath.IsAbs(check.Executable) {
		return errors.New("validation check requires an ID and absolute executable")
	}
	if check.Timeout <= 0 {
		return fmt.Errorf("validation check %s requires a positive timeout", check.ID)
	}
	if check.MaxOutputBytes <= 0 {
		return fmt.Errorf("validation check %s requires a positive output-byte limit", check.ID)
	}
	return nil
}

func validationEligible(value isolation.Conformance) bool {
	return value.CandidateWrite && value.OutsideWriteDenied && value.GitMetadataDenied &&
		value.SensitiveReadsDenied && value.ToolNetworkPolicy && value.CrashContainment
}

func boundedCombined(stdout, stderr []byte, limit int64) string {
	combined := append(append([]byte(nil), stdout...), stderr...)
	if int64(len(combined)) > limit {
		combined = combined[:limit]
	}
	return string(combined)
}

func treeFingerprint(ctx context.Context, root string, limits isolation.WorkspaceLimits) (string, error) {
	if err := limits.Validate(); err != nil {
		return "", fmt.Errorf("validation fingerprint workspace limits: %w", err)
	}
	type treeEntry struct {
		path, kind string
		mode       uint32
		size       int64
	}
	var entries []treeEntry
	var files, directories, pathBytes int64
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, relErr := filepath.Rel(root, path)
		if relErr != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return errors.New("validation fingerprint escaped candidate")
		}
		if path != root {
			added := int64(len(filepath.ToSlash(relative)))
			if added > limits.MaxPathBytes-pathBytes {
				return fmt.Errorf("validation fingerprint exceeds %d path bytes", limits.MaxPathBytes)
			}
			pathBytes += added
		}
		if entry.Name() == ".git" {
			if filepath.Clean(relative) != ".git" {
				return fmt.Errorf("validation fingerprint rejects nested git metadata %q", filepath.ToSlash(relative))
			}
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if path == root || entry.IsDir() {
			if path != root {
				if directories >= limits.MaxDirectories {
					return fmt.Errorf("validation fingerprint exceeds %d directories", limits.MaxDirectories)
				}
				directories++
			}
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		item := treeEntry{path: filepath.ToSlash(relative)}
		switch mode := info.Mode(); {
		case mode.IsRegular():
			if info.Size() < 0 || info.Size() > limits.MaxFileBytes {
				return fmt.Errorf("validation fingerprint file %q exceeds %d bytes", relative, limits.MaxFileBytes)
			}
			item.kind, item.mode = "regular", 0o100644
			item.size = info.Size()
			if mode.Perm()&0o111 != 0 {
				item.mode = 0o100755
			}
		case mode&os.ModeSymlink != 0:
			item.kind, item.mode = "symlink", 0o120000
		default:
			return fmt.Errorf("validation fingerprint rejects special file %q", relative)
		}
		if files >= limits.MaxFiles {
			return fmt.Errorf("validation fingerprint exceeds %d files", limits.MaxFiles)
		}
		files++
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
	var totalBytes int64
	buffer := make([]byte, 64<<10)
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		if entry.kind == "symlink" {
			target, err := rootHandle.Readlink(entry.path)
			if err != nil {
				return "", err
			}
			added := int64(len(target))
			if added > limits.MaxTotalBytes-totalBytes {
				return "", fmt.Errorf("validation fingerprint exceeds %d total bytes", limits.MaxTotalBytes)
			}
			totalBytes += added
			_, _ = hasher.Write([]byte(entry.path))
			_, _ = hasher.Write([]byte{0})
			_, _ = hasher.Write([]byte(entry.kind))
			_, _ = hasher.Write([]byte{0, byte(entry.mode >> 16), byte(entry.mode >> 8), byte(entry.mode)})
			_, _ = hasher.Write([]byte(target))
			_, _ = hasher.Write([]byte{0})
			continue
		}
		_, _ = hasher.Write([]byte(entry.path))
		_, _ = hasher.Write([]byte{0})
		_, _ = hasher.Write([]byte(entry.kind))
		_, _ = hasher.Write([]byte{0, byte(entry.mode >> 16), byte(entry.mode >> 8), byte(entry.mode)})
		file, err := rootHandle.Open(entry.path)
		if err != nil {
			return "", err
		}
		var fileBytes int64
		for {
			if err := ctx.Err(); err != nil {
				_ = file.Close()
				return "", err
			}
			read, readErr := file.Read(buffer)
			if read > 0 {
				added := int64(read)
				if added > limits.MaxFileBytes-fileBytes {
					_ = file.Close()
					return "", fmt.Errorf("validation fingerprint file %q exceeds %d bytes", entry.path, limits.MaxFileBytes)
				}
				if added > limits.MaxTotalBytes-totalBytes {
					_ = file.Close()
					return "", fmt.Errorf("validation fingerprint exceeds %d total bytes", limits.MaxTotalBytes)
				}
				fileBytes += added
				totalBytes += added
				_, _ = hasher.Write(buffer[:read])
			}
			if errors.Is(readErr, io.EOF) {
				break
			}
			if readErr != nil {
				_ = file.Close()
				return "", readErr
			}
		}
		if err := file.Close(); err != nil {
			return "", err
		}
		if fileBytes != entry.size {
			return "", fmt.Errorf("validation fingerprint file %q changed while being read", entry.path)
		}
		_, _ = hasher.Write([]byte{0})
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

func isWithin(root, path string) bool {
	relative, err := filepath.Rel(root, path)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}
