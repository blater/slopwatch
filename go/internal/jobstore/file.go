package jobstore

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"github.com/blater/slopmochi/internal/fix"
)

// File stores each job as a plain JSON file in one directory.
type File struct {
	mu        sync.Mutex
	directory string
	closed    bool
}

func Open(directory string) (*File, error) {
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return nil, fmt.Errorf("create job directory: %w", err)
	}
	return &File{directory: directory}, nil
}

func (store *File) Lock(job fix.JobID) (Lock, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.closed {
		return nil, errors.New("job store closed")
	}
	return acquireStoreLease(filepath.Join(store.directory, string(job)+".lock"))
}

func (store *File) Save(ctx context.Context, record Record) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	path := filepath.Join(store.directory, string(record.JobID)+".json")
	var formatted bytes.Buffer
	if err := json.Indent(&formatted, record.State, "", "  "); err != nil {
		return fmt.Errorf("format job state: %w", err)
	}
	formatted.WriteByte('\n')
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.closed {
		return errors.New("job store closed")
	}
	if err := os.WriteFile(path, formatted.Bytes(), 0o600); err != nil {
		return fmt.Errorf("write job state: %w", err)
	}
	return nil
}

func (store *File) Load(ctx context.Context) ([]Record, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.closed {
		return nil, errors.New("job store closed")
	}
	entries, err := os.ReadDir(store.directory)
	if err != nil {
		return nil, fmt.Errorf("read job directory: %w", err)
	}
	result := make([]Record, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		payload, err := os.ReadFile(filepath.Join(store.directory, entry.Name()))
		if err != nil {
			continue
		}
		if !json.Valid(payload) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		name := entry.Name()
		result = append(result, Record{JobID: fix.JobID(name[:len(name)-len(".json")]), UpdatedAt: info.ModTime(), State: payload})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].UpdatedAt.Before(result[j].UpdatedAt) })
	return result, nil
}

func (store *File) Close() error {
	store.mu.Lock()
	if store.closed {
		store.mu.Unlock()
		return nil
	}
	store.closed = true
	store.mu.Unlock()
	return nil
}
