package analysiscache

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
)

const maxDecodedArtifact = 128 << 20

// encodeArtifactStorage preserves the canonical bytes used to name an artifact.
func encodeArtifactStorage(plain []byte) ([]byte, error) {
	if len(plain) > maxDecodedArtifact {
		// The ceiling bounds compressed decoding only. Preserve legacy caching
		// for larger artifacts by storing their canonical plain representation.
		return plain, nil
	}
	var out bytes.Buffer
	writer, err := gzip.NewWriterLevel(&out, gzip.BestSpeed)
	if err != nil {
		return nil, err
	}
	if _, err = writer.Write(plain); err != nil {
		return nil, err
	}
	if err = writer.Close(); err != nil {
		return nil, err
	}
	if out.Len() < len(plain) {
		return out.Bytes(), nil
	}
	return plain, nil
}

func decodeArtifactStorage(data []byte) ([]byte, error) {
	if len(data) < 2 || data[0] != 0x1f || data[1] != 0x8b {
		return data, nil
	}
	source := bytes.NewReader(data)
	reader, err := gzip.NewReader(source)
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	reader.Multistream(false)
	decoded, err := io.ReadAll(io.LimitReader(reader, maxDecodedArtifact+1))
	if err != nil {
		return nil, err
	}
	if len(decoded) > maxDecodedArtifact {
		return nil, fmt.Errorf("decoded artifact exceeds limit")
	}
	if source.Len() != 0 {
		return nil, fmt.Errorf("trailing artifact storage data")
	}
	return decoded, nil
}

var artifactWriteLocks [64]sync.Mutex

func writeArtifact(path string, plain, stored []byte) error {
	_, _, _, err := writeArtifactResult(context.Background(), path, plain, stored)
	return err
}

func writeArtifactResult(ctx context.Context, path string, plain, stored []byte) (before, after int64, changed bool, err error) {
	// Serialize representation changes with other artifact writers in this process.
	var stripe uint64
	for i := range path {
		stripe = stripe*31 + uint64(path[i])
	}
	lock := &artifactWriteLocks[stripe%uint64(len(artifactWriteLocks))]
	lock.Lock()
	defer lock.Unlock()
	if existing, err := os.ReadFile(path); err == nil {
		before = int64(len(existing))
		after = before
		decoded, err := decodeArtifactStorage(existing)
		if err != nil || !bytes.Equal(decoded, plain) {
			return before, after, false, fmt.Errorf("immutable artifact path contains different or corrupt data")
		}
		if len(existing) <= len(stored) {
			return before, after, false, nil
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return before, after, false, err
	}
	if err := ctx.Err(); err != nil {
		return before, after, false, err
	}
	if err := writeAtomic(path, stored); err != nil {
		return before, after, false, err
	}
	return before, int64(len(stored)), true, nil
}

// CompactionStats describes only artifacts examined by an explicit compaction.
type CompactionStats struct {
	Examined    int
	Compacted   int
	Skipped     int
	Errors      int
	BeforeBytes int64
	AfterBytes  int64
}

// CompactArtifacts losslessly rewrites valid artifact representations. It never
// rewrites references, removes entries, or traverses symbolic links.
func (store *Store) CompactArtifacts(ctx context.Context) (stats CompactionStats, err error) {
	root := filepath.Join(store.root, "artifacts")
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if walkErr != nil {
			stats.Errors++
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		stats.Examined++
		info, err := entry.Info()
		if err != nil {
			stats.Errors++
			return err
		}
		if !info.Mode().IsRegular() {
			stats.Skipped++
			return nil
		}
		stats.BeforeBytes += info.Size()
		stats.AfterBytes += info.Size()
		if info.Size() > maxDecodedArtifact {
			stats.Skipped++
			return nil
		}
		digest := Digest(filepath.Base(filepath.Dir(path)) + filepath.Base(path))
		canonical, ok := cachePath(store.root, "artifacts", digest)
		if !ok || canonical != path {
			stats.Skipped++
			return nil
		}
		stored, err := os.ReadFile(path)
		if err != nil {
			stats.Errors++
			return err
		}
		plain, err := decodeArtifactStorage(stored)
		if err != nil || DigestBytes(plain) != digest || !validArtifactEnvelope(plain) {
			stats.Skipped++
			return nil
		}
		compressed, err := encodeArtifactStorage(plain)
		if err != nil {
			stats.Skipped++
			return nil
		}
		if len(compressed) >= len(stored) {
			return nil
		}
		before, after, changed, err := writeArtifactResult(ctx, path, plain, compressed)
		if err != nil {
			stats.Errors++
			return fmt.Errorf("compact %s: %w", path, err)
		}
		if changed {
			stats.Compacted++
		}
		stats.BeforeBytes += before - info.Size()
		stats.AfterBytes += after - info.Size()
		return nil
	})
	return stats, err
}

func validArtifactEnvelope(data []byte) bool {
	var header envelope
	if json.Unmarshal(data, &header) != nil || !validDigest(header.Key) {
		return false
	}
	var payload json.RawMessage
	switch header.Kind {
	case "unit":
		return decodeEnvelope(data, "unit", unitSchemaVersion, header.Key, &payload)
	case "projection":
		return decodeEnvelope(data, "projection", projectionSchemaVersion, header.Key, &payload)
	default:
		return false
	}
}
