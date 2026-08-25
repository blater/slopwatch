package jobstore

import (
	"context"
	"errors"
	"sync"
)

type Memory struct {
	mu      sync.Mutex
	records []Record
	closed  bool
}

func NewMemory() *Memory { return &Memory{} }

func (memory *Memory) Append(ctx context.Context, record Record) (Record, error) {
	if err := ctx.Err(); err != nil {
		return Record{}, err
	}
	memory.mu.Lock()
	defer memory.mu.Unlock()
	if memory.closed {
		return Record{}, errors.New("job store closed")
	}
	record.Version = RecordVersion
	record.Sequence = uint64(len(memory.records) + 1)
	record.Data = append(record.Data[:0:0], record.Data...)
	memory.records = append(memory.records, record)
	return record, nil
}

func (memory *Memory) Load(ctx context.Context) ([]Record, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	memory.mu.Lock()
	defer memory.mu.Unlock()
	result := make([]Record, len(memory.records))
	copy(result, memory.records)
	for index := range result {
		result[index].Data = append(result[index].Data[:0:0], result[index].Data...)
	}
	return result, nil
}

func (memory *Memory) Compact(ctx context.Context, records []Record) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	memory.mu.Lock()
	defer memory.mu.Unlock()
	if memory.closed {
		return errors.New("job store closed")
	}
	memory.records = make([]Record, len(records))
	for index, record := range records {
		record.Version = RecordVersion
		record.Sequence = uint64(index + 1)
		record.Data = append(record.Data[:0:0], record.Data...)
		memory.records[index] = record
	}
	return nil
}

func (memory *Memory) Close() error {
	memory.mu.Lock()
	memory.closed = true
	memory.mu.Unlock()
	return nil
}
