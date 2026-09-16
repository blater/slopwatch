package openairesponses

import (
	"context"
	"io"
)

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
