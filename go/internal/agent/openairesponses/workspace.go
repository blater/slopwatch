package openairesponses

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

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
		canonicalRoot: canonical,
		root:          root,
		stagingRoot:   stagingCanonical,
		staging:       staging,
		allowed:       make(map[fix.RepoPath]struct{}, len(policy.Allowed)),
		repository:    policy.Scope == "repository",
		config:        config,
	}
	if manifest != nil {
		if manifest.Count <= 0 || manifest.Path == "" || !filepath.IsAbs(manifest.Path) || filepath.Clean(manifest.Path) != manifest.Path || !withinPath(stagingCanonical, manifest.Path) {
			_ = root.Close()
			_ = staging.Close()
			return nil, errors.New("target manifest must be a file in candidate staging")
		}
		canonicalManifest, canonicalErr := filepath.EvalSymlinks(manifest.Path)
		info, statErr := os.Stat(manifest.Path)
		if canonicalErr != nil || canonicalManifest != manifest.Path || statErr != nil || !info.Mode().IsRegular() {
			_ = root.Close()
			_ = staging.Close()
			return nil, errors.New("target manifest is unavailable")
		}
		relative, relativeErr := filepath.Rel(stagingCanonical, manifest.Path)
		if relativeErr != nil {
			_ = root.Close()
			_ = staging.Close()
			return nil, errors.New("target manifest path is invalid")
		}
		result.targetManifest = filepath.ToSlash(relative)
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

func (tools *candidateTools) readTargetManifest(ctx context.Context) (readResult, error) {
	if tools.targetManifest == "" {
		return readResult{}, fault("unavailable", "no target manifest was supplied")
	}
	if err := ctx.Err(); err != nil {
		return readResult{}, err
	}
	info, err := tools.staging.Lstat(tools.targetManifest)
	if err != nil || !info.Mode().IsRegular() {
		return readResult{}, fault("read_failed", "target manifest is unavailable")
	}
	file, err := tools.staging.Open(tools.targetManifest)
	if err != nil {
		return readResult{}, fault("read_failed", "target manifest could not be opened")
	}
	contents, readErr := io.ReadAll(file)
	closeErr := file.Close()
	if readErr != nil || closeErr != nil {
		return readResult{}, fault("read_failed", "target manifest could not be read")
	}
	if err := ctx.Err(); err != nil {
		return readResult{}, err
	}
	return readResult{Path: "target-manifest", Content: string(contents)}, nil
}

func (tools *candidateTools) Close() error {
	return errors.Join(tools.root.Close(), tools.staging.Close())
}

func withinPath(root, value string) bool {
	relative, err := filepath.Rel(root, value)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func (tools *candidateTools) revalidateRoot() error {
	resolved, err := filepath.EvalSymlinks(tools.canonicalRoot)
	if err != nil || resolved != tools.canonicalRoot {
		return fault("workspace_changed", "candidate root identity changed")
	}
	pathInfo, err := os.Stat(tools.canonicalRoot)
	if err != nil {
		return fault("workspace_changed", "candidate root is unavailable")
	}
	handleInfo, err := tools.root.Stat(".")
	if err != nil || !os.SameFile(pathInfo, handleInfo) {
		return fault("workspace_changed", "candidate root identity changed")
	}
	return nil
}

func parseToolPath(value string, allowRoot bool) (string, error) {
	if allowRoot && (value == "" || value == ".") {
		return ".", nil
	}
	parsed, err := fix.ParseRepoPath(value)
	if err != nil || forbiddenGitPath(value) {
		return "", fault("invalid_path", "path must be a repository-relative path outside .git")
	}
	return parsed.String(), nil
}

func forbiddenGitPath(value string) bool {
	for _, component := range strings.Split(filepath.ToSlash(value), "/") {
		if strings.EqualFold(component, ".git") {
			return true
		}
	}
	return false
}

func (tools *candidateTools) inspect(pathname string, missingFinalAllowed bool) (os.FileInfo, error) {
	if err := tools.revalidateRoot(); err != nil {
		return nil, err
	}
	if pathname == "." {
		return tools.root.Stat(".")
	}
	components := strings.Split(pathname, "/")
	current := ""
	for index, component := range components {
		current = path.Join(current, component)
		info, err := tools.root.Lstat(current)
		if errors.Is(err, os.ErrNotExist) && missingFinalAllowed && index == len(components)-1 {
			return nil, nil
		}
		if err != nil {
			return nil, fault("not_found", "path does not exist")
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, fault("unsupported_file", "symbolic links are not accessible")
		}
		if index < len(components)-1 && !info.IsDir() {
			return nil, fault("invalid_path", "a path parent is not a directory")
		}
	}
	return tools.root.Lstat(pathname)
}

func (tools *candidateTools) mayWrite(pathname string) error {
	if tools.repository {
		return nil
	}
	repositoryPath, err := fix.ParseRepoPath(pathname)
	if err != nil {
		return fault("invalid_path", "invalid write path")
	}
	if _, allowed := tools.allowed[repositoryPath]; !allowed {
		return fault("write_denied", "path is outside the frozen write scope")
	}
	return nil
}

type listEntry struct {
	Path string `json:"path"`
	Kind string `json:"kind"`
	Size int64  `json:"size,omitempty"`
}

type listResult struct {
	Entries   []listEntry `json:"entries"`
	Truncated bool        `json:"truncated"`
}

func (tools *candidateTools) list(ctx context.Context, value string, recursive bool) (listResult, error) {
	pathname, err := parseToolPath(value, true)
	if err != nil {
		return listResult{}, err
	}
	info, err := tools.inspect(pathname, false)
	if err != nil {
		return listResult{}, err
	}
	if !info.IsDir() {
		return listResult{}, fault("not_directory", "list_files requires a directory")
	}
	queue := []string{pathname}
	result := listResult{Entries: make([]listEntry, 0)}
	for len(queue) > 0 {
		if err := ctx.Err(); err != nil {
			return listResult{}, err
		}
		directory := queue[0]
		queue = queue[1:]
		if _, err := tools.inspect(directory, false); err != nil {
			return listResult{}, err
		}
		file, err := tools.root.Open(directory)
		if err != nil {
			return listResult{}, fault("read_failed", "directory could not be opened")
		}
		for {
			batch, readErr := file.Readdir(128)
			for _, item := range batch {
				if strings.EqualFold(item.Name(), ".git") {
					continue
				}
				relative := item.Name()
				if directory != "." {
					relative = path.Join(directory, item.Name())
				}
				kind := "file"
				switch {
				case item.Mode()&os.ModeSymlink != 0:
					kind = "blocked"
				case item.IsDir():
					kind = "directory"
					if recursive {
						queue = append(queue, relative)
					}
				case !item.Mode().IsRegular():
					kind = "blocked"
				}
				result.Entries = append(result.Entries, listEntry{Path: relative, Kind: kind, Size: item.Size()})
				if len(result.Entries) >= tools.config.maxListEntries {
					result.Truncated = true
					_ = file.Close()
					sort.Slice(result.Entries, func(i, j int) bool { return result.Entries[i].Path < result.Entries[j].Path })
					return result, nil
				}
			}
			if errors.Is(readErr, io.EOF) {
				break
			}
			if readErr != nil {
				_ = file.Close()
				return listResult{}, fault("read_failed", "directory could not be read")
			}
		}
		if err := file.Close(); err != nil {
			return listResult{}, fault("read_failed", "directory could not be closed")
		}
	}
	sort.Slice(result.Entries, func(i, j int) bool { return result.Entries[i].Path < result.Entries[j].Path })
	return result, nil
}

type readResult struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

func (tools *candidateTools) read(ctx context.Context, value string) (readResult, error) {
	pathname, err := parseToolPath(value, false)
	if err != nil {
		return readResult{}, err
	}
	info, err := tools.inspect(pathname, false)
	if err != nil {
		return readResult{}, err
	}
	if !info.Mode().IsRegular() {
		return readResult{}, fault("unsupported_file", "only regular files may be read")
	}
	if info.Size() > tools.config.maxReadBytes {
		return readResult{}, fault("file_too_large", "file exceeds the configured read limit")
	}
	if err := ctx.Err(); err != nil {
		return readResult{}, err
	}
	file, err := tools.root.Open(pathname)
	if err != nil {
		return readResult{}, fault("read_failed", "file could not be opened")
	}
	contents, readErr := io.ReadAll(io.LimitReader(file, tools.config.maxReadBytes+1))
	closeErr := file.Close()
	if readErr != nil || closeErr != nil {
		return readResult{}, fault("read_failed", "file could not be read")
	}
	if int64(len(contents)) > tools.config.maxReadBytes {
		return readResult{}, fault("file_too_large", "file exceeds the configured read limit")
	}
	if err := ctx.Err(); err != nil {
		return readResult{}, err
	}
	return readResult{Path: pathname, Content: string(contents)}, nil
}

type mutationResult struct {
	Path  string `json:"path"`
	Bytes int    `json:"bytes,omitempty"`
	OK    bool   `json:"ok"`
}

func (tools *candidateTools) write(ctx context.Context, value, contents string) (mutationResult, error) {
	pathname, err := parseToolPath(value, false)
	if err != nil {
		return mutationResult{}, err
	}
	if err := tools.mayWrite(pathname); err != nil {
		return mutationResult{}, err
	}
	if int64(len(contents)) > tools.config.maxWriteBytes {
		return mutationResult{}, fault("file_too_large", "content exceeds the configured write limit")
	}
	info, err := tools.inspect(pathname, true)
	if err != nil {
		return mutationResult{}, err
	}
	mode := os.FileMode(0o644)
	if info != nil {
		if !info.Mode().IsRegular() {
			return mutationResult{}, fault("unsupported_file", "only regular files may be replaced")
		}
		mode = info.Mode().Perm()
	}
	parent := path.Dir(pathname)
	parentInfo, err := tools.inspect(parent, false)
	if err != nil || !parentInfo.IsDir() {
		return mutationResult{}, fault("invalid_path", "write parent must be an existing directory")
	}
	if err := ctx.Err(); err != nil {
		return mutationResult{}, err
	}
	temporary, err := randomTemporaryName(".")
	if err != nil {
		return mutationResult{}, fault("write_failed", "temporary file name could not be created")
	}
	file, err := tools.staging.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return mutationResult{}, fault("write_failed", "temporary file could not be created")
	}
	removeTemporary := true
	defer func() {
		if removeTemporary {
			_ = tools.staging.Remove(temporary)
		}
	}()
	writeErr := writeAll(ctx, file, []byte(contents))
	if writeErr == nil {
		writeErr = file.Sync()
	}
	closeErr := file.Close()
	if errors.Is(writeErr, context.Canceled) || errors.Is(writeErr, context.DeadlineExceeded) {
		return mutationResult{}, writeErr
	}
	if writeErr != nil || closeErr != nil {
		return mutationResult{}, fault("write_failed", "file content could not be persisted")
	}
	if err := tools.revalidateRoot(); err != nil {
		return mutationResult{}, err
	}
	// Recheck the destination immediately before the rooted atomic rename.
	if current, statErr := tools.root.Lstat(pathname); statErr == nil && !current.Mode().IsRegular() {
		return mutationResult{}, fault("unsupported_file", "write destination changed type")
	} else if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		return mutationResult{}, fault("write_failed", "write destination could not be checked")
	}
	if err := tools.replaceStagedFile(temporary, pathname); err != nil {
		return mutationResult{}, fault("write_failed", "atomic file replacement failed")
	}
	removeTemporary = false
	return mutationResult{Path: pathname, Bytes: len(contents), OK: true}, nil
}

// replaceStagedFile pins both source and destination directories before the
// rename. The final mutation is relative to those handles, so swapping a
// destination parent for an external symlink cannot redirect the write.
func (tools *candidateTools) replaceStagedFile(temporary, pathname string) error {
	source, err := tools.staging.Open(".")
	if err != nil {
		return err
	}
	destination, err := tools.root.Open(path.Dir(pathname))
	if err != nil {
		_ = source.Close()
		return err
	}
	renameErr := renameRootedFile(source, temporary, destination, path.Base(pathname))
	var syncErr error
	if renameErr == nil {
		syncErr = errors.Join(source.Sync(), destination.Sync())
	}
	return errors.Join(renameErr, syncErr, source.Close(), destination.Close())
}

func (tools *candidateTools) delete(ctx context.Context, value string) (mutationResult, error) {
	pathname, err := parseToolPath(value, false)
	if err != nil {
		return mutationResult{}, err
	}
	if err := tools.mayWrite(pathname); err != nil {
		return mutationResult{}, err
	}
	info, err := tools.inspect(pathname, false)
	if err != nil {
		return mutationResult{}, err
	}
	if !info.Mode().IsRegular() {
		return mutationResult{}, fault("unsupported_file", "only regular files may be deleted")
	}
	if err := ctx.Err(); err != nil {
		return mutationResult{}, err
	}
	if err := tools.root.Remove(pathname); err != nil {
		return mutationResult{}, fault("delete_failed", "file could not be deleted")
	}
	if err := tools.syncDirectory(path.Dir(pathname)); err != nil {
		return mutationResult{}, fault("delete_failed", "deletion could not be synchronized")
	}
	return mutationResult{Path: pathname, OK: true}, nil
}

func randomTemporaryName(parent string) (string, error) {
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return "", err
	}
	name := ".slopwatch-agent-" + hex.EncodeToString(nonce[:]) + ".tmp"
	if parent == "." {
		return name, nil
	}
	return path.Join(parent, name), nil
}

func writeAll(ctx context.Context, writer io.Writer, contents []byte) error {
	for len(contents) > 0 {
		if err := ctx.Err(); err != nil {
			return err
		}
		chunk := contents
		if len(chunk) > 64<<10 {
			chunk = chunk[:64<<10]
		}
		written, err := writer.Write(chunk)
		if err != nil {
			return err
		}
		if written == 0 {
			return io.ErrShortWrite
		}
		contents = contents[written:]
	}
	return nil
}

func (tools *candidateTools) syncDirectory(directory string) error {
	handle, err := tools.root.Open(directory)
	if err != nil {
		return err
	}
	err = handle.Sync()
	closeErr := handle.Close()
	if err != nil {
		return err
	}
	return closeErr
}

func encodeToolValue(value any, maximum int64) (string, error) {
	payload, err := json.Marshal(value)
	if err != nil {
		return "", fault("tool_failed", "tool result could not be encoded")
	}
	if int64(len(payload)) > maximum {
		return "", fault("result_too_large", "tool result exceeds the configured output limit")
	}
	return string(payload), nil
}

func encodeToolError(err error) string {
	code := "tool_failed"
	message := "tool operation failed"
	var typed *toolFault
	if errors.As(err, &typed) {
		code = typed.code
		message = typed.message
	} else if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		code = "canceled"
		message = "tool operation was canceled"
	}
	payload, marshalErr := json.Marshal(map[string]any{
		"ok":    false,
		"error": map[string]string{"code": code, "message": message},
	})
	if marshalErr != nil {
		return `{"ok":false,"error":{"code":"tool_failed","message":"tool operation failed"}}`
	}
	return string(payload)
}

func (tools *candidateTools) execute(ctx context.Context, call functionCall) (string, *fix.RepoPath, error) {
	var value any
	var changed *fix.RepoPath
	switch call.Name {
	case "read_target_manifest":
		if err := decodeNoArguments(call.Arguments); err != nil {
			return "", nil, err
		}
		var err error
		value, err = tools.readTargetManifest(ctx)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return "", nil, err
			}
			return encodeToolError(err), nil, nil
		}
	case "list_files":
		arguments, err := decodeListArguments(call.Arguments)
		if err != nil {
			return "", nil, err
		}
		value, err = tools.list(ctx, arguments.Path, arguments.Recursive)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return "", nil, err
			}
			return encodeToolError(err), nil, nil
		}
	case "read_file":
		arguments, err := decodePathArguments(call.Arguments)
		if err != nil {
			return "", nil, err
		}
		value, err = tools.read(ctx, arguments.Path)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return "", nil, err
			}
			return encodeToolError(err), nil, nil
		}
	case "write_file":
		arguments, err := decodeWriteArguments(call.Arguments)
		if err != nil {
			return "", nil, err
		}
		value, err = tools.write(ctx, arguments.Path, arguments.Content)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return "", nil, err
			}
			return encodeToolError(err), nil, nil
		}
		parsed, _ := fix.ParseRepoPath(arguments.Path)
		changed = &parsed
	case "delete_file":
		arguments, err := decodePathArguments(call.Arguments)
		if err != nil {
			return "", nil, err
		}
		value, err = tools.delete(ctx, arguments.Path)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return "", nil, err
			}
			return encodeToolError(err), nil, nil
		}
		parsed, _ := fix.ParseRepoPath(arguments.Path)
		changed = &parsed
	default:
		return "", nil, fmt.Errorf("unknown function tool %q", call.Name)
	}
	encoded, err := encodeToolValue(value, tools.config.maxToolOutputBytes)
	return encoded, changed, err
}
