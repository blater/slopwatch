package native

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"

	"github.com/blater/slopmochi/internal/analysiscache"
)

type workspaceHashResult struct {
	digest analysiscache.Digest
	err    error
}

func verifyWorkspaceInputs(analyzer *analysisEngine, ctx context.Context, expected map[string]analysiscache.Digest) (bool, error) {
	paths := mapKeys(expected)
	actual, err := hashWorkspacePaths(analyzer, ctx, paths)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	for _, path := range paths {
		if actual[path] != expected[path] {
			return false, nil
		}
	}
	return true, nil
}

func hashWorkspacePaths(analyzer *analysisEngine, ctx context.Context, paths []string) (map[string]analysiscache.Digest, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	results := make([]workspaceHashResult, len(paths))
	jobs := make(chan int)
	workers := min(runtime.GOMAXPROCS(0), 8)
	if workers < 1 {
		workers = 1
	}
	var group sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		group.Add(1)
		go func() {
			defer group.Done()
			for index := range jobs {
				if err := ctx.Err(); err != nil {
					results[index].err = err
					continue
				}
				contents, err := os.ReadFile(filepath.Join(analyzer.workspace, filepath.FromSlash(paths[index])))
				if err != nil {
					results[index].err = err
					continue
				}
				results[index].digest = analysiscache.DigestBytes(contents)
			}
		}()
	}
	for index := range paths {
		jobs <- index
	}
	close(jobs)
	group.Wait()
	return workspaceDigests(paths, results)
}

func workspaceDigests(paths []string, results []workspaceHashResult) (map[string]analysiscache.Digest, error) {
	digests := make(map[string]analysiscache.Digest, len(paths))
	for index, item := range results {
		if item.err != nil {
			return nil, fmt.Errorf("hash workspace input %s: %w", paths[index], item.err)
		}
		digests[paths[index]] = item.digest
	}
	return digests, nil
}
