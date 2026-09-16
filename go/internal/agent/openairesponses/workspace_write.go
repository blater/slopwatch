package openairesponses

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path"
)

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
