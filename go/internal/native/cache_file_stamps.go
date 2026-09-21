package native

import (
	"context"
	"os"
	"path/filepath"

	"github.com/blater/slopwatch/internal/analysiscache"
	"github.com/blater/slopwatch/internal/report"
)

func workspaceStamp(root, path string) (analysiscache.FileStamp, error) {
	info, err := os.Stat(filepath.Join(root, filepath.FromSlash(path)))
	if err != nil {
		return analysiscache.FileStamp{}, err
	}
	if !info.Mode().IsRegular() {
		return analysiscache.FileStamp{}, ErrWorkspaceChanged
	}
	return analysiscache.FileStamp{Size: info.Size(), Modified: info.ModTime().UnixNano(), Changed: fileChangeTime(info)}, nil
}

// Reuse content digests only when filesystem metadata still matches the bytes
// previously hashed. Capture stamps before reading and verify them afterwards.
func cachedWorkspaceDigests(analyzer *analysisEngine, ctx context.Context, paths map[string]bool, previous analysiscache.Generation, captured ...map[string][]byte) (map[string]analysiscache.Digest, map[string]analysiscache.FileStamp, error) {
	stamps := make(map[string]analysiscache.FileStamp, len(paths))
	digests := make(map[string]analysiscache.Digest, len(paths))
	changed := make([]string, 0)
	for path := range paths {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		if len(captured) > 0 {
			if data, ok := captured[0][path]; ok {
				digests[path] = analysiscache.DigestBytes(data)
				continue
			}
		}
		stamp, err := workspaceStamp(analyzer.workspace, path)
		if err != nil {
			return nil, nil, err
		}
		stamps[path] = stamp
		if old, ok := previous.InputStamps[path]; ok && old == stamp && previous.InputDigests[path] != "" {
			digests[path] = previous.InputDigests[path]
		} else {
			changed = append(changed, path)
		}
	}
	fresh, err := hashWorkspacePaths(analyzer, ctx, changed)
	if err != nil {
		return nil, nil, err
	}
	for path, digest := range fresh {
		digests[path] = digest
	}
	return digests, stamps, nil
}

func verifyWorkspaceStamps(analyzer *analysisEngine, ctx context.Context, expected map[string]analysiscache.FileStamp) (bool, error) {
	for path, stamp := range expected {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		actual, err := workspaceStamp(analyzer.workspace, path)
		if os.IsNotExist(err) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		if actual != stamp {
			return false, nil
		}
	}
	return true, nil
}

type startupProjectionKey struct{}

// AnalyzeStartup verifies normal dependency keys but reuses the compact report
// when unchanged. Ordinary Analyze still returns complete evidence.
func (analyzer *Analyzer) AnalyzeStartup(ctx context.Context, targets []string) (report.Document, error) {
	return analyzer.Analyze(context.WithValue(ctx, startupProjectionKey{}, true), targets, nil)
}
