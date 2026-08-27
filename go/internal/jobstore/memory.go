package jobstore

import (
	"context"
	"errors"
	"sort"
	"sync"

	"github.com/blater/slopmochi/internal/fix"
)

type Memory struct {
	mu      sync.Mutex
	records map[string]Record
	closed  bool
}

func NewMemory() *Memory { return &Memory{records: map[string]Record{}} }

type memoryLock struct{}

func (memoryLock) Close() error { return nil }

func (memory *Memory) Lock(fix.JobID) (Lock, error) { return memoryLock{}, nil }

func (memory *Memory) Save(ctx context.Context, record Record) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	memory.mu.Lock()
	defer memory.mu.Unlock()
	if memory.closed {
		return errors.New("job store closed")
	}
	record.State = append(record.State[:0:0], record.State...)
	memory.records[string(record.JobID)] = record
	return nil
}

func (memory *Memory) Load(ctx context.Context) ([]Record, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	memory.mu.Lock()
	defer memory.mu.Unlock()
	result := make([]Record, 0, len(memory.records))
	for _, record := range memory.records {
		record.State = append(record.State[:0:0], record.State...)
		result = append(result, record)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].UpdatedAt.Before(result[j].UpdatedAt) })
	return result, nil
}

func (memory *Memory) Close() error {
	memory.mu.Lock()
	memory.closed = true
	memory.mu.Unlock()
	return nil
}
