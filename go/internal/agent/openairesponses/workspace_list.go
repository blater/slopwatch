package openairesponses

import (
	"context"
	"errors"
	"io"
	"os"
	"path"
	"sort"
	"strings"
)

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
