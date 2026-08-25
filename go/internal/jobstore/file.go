package jobstore

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
)

const (
	journalName    = "jobs.journal"
	maxRecordBytes = 16 << 20
	headerBytes    = 4 + sha256.Size
)

type File struct {
	mu       sync.Mutex
	file     *os.File
	lease    *storeLease
	path     string
	sequence uint64
	closed   bool
	failed   error
}

func OpenFile(directory string) (*File, error) {
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return nil, fmt.Errorf("create job store: %w", err)
	}
	if err := os.Chmod(directory, 0o700); err != nil {
		return nil, fmt.Errorf("secure job store directory: %w", err)
	}
	info, err := os.Lstat(directory)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("job store path is not a real directory")
	}
	lease, err := acquireStoreLease(directory)
	if err != nil {
		return nil, err
	}
	path := filepath.Join(directory, journalName)
	file, err := openJournal(path)
	if err != nil {
		_ = lease.Close()
		return nil, err
	}
	if err := syncDirectory(directory); err != nil {
		_ = file.Close()
		_ = lease.Close()
		return nil, fmt.Errorf("sync job store directory: %w", err)
	}
	store := &File{file: file, lease: lease, path: path}
	records, validBytes, err := store.loadLocked()
	if err != nil {
		file.Close()
		_ = lease.Close()
		return nil, err
	}
	info, err = file.Stat()
	if err != nil {
		file.Close()
		_ = lease.Close()
		return nil, err
	}
	if info.Size() != validBytes {
		if err := file.Truncate(validBytes); err != nil {
			file.Close()
			_ = lease.Close()
			return nil, fmt.Errorf("discard torn job journal tail: %w", err)
		}
		if err := file.Sync(); err != nil {
			file.Close()
			_ = lease.Close()
			return nil, fmt.Errorf("sync repaired job journal: %w", err)
		}
		if err := syncDirectory(directory); err != nil {
			file.Close()
			_ = lease.Close()
			return nil, fmt.Errorf("sync repaired job store directory: %w", err)
		}
	}
	if len(records) > 0 {
		store.sequence = records[len(records)-1].Sequence
	}
	if _, err := file.Seek(0, io.SeekEnd); err != nil {
		file.Close()
		_ = lease.Close()
		return nil, err
	}
	return store, nil
}

func (store *File) Append(ctx context.Context, record Record) (Record, error) {
	if err := ctx.Err(); err != nil {
		return Record{}, err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.closed {
		return Record{}, errors.New("job store closed")
	}
	if store.failed != nil {
		return Record{}, fmt.Errorf("job store is unusable after a previous durable write failure: %w", store.failed)
	}
	record.Version = RecordVersion
	record.Sequence = store.sequence + 1
	if err := writeRecord(store.file, record); err != nil {
		store.failed = err
		return Record{}, err
	}
	if err := store.file.Sync(); err != nil {
		store.failed = err
		return Record{}, fmt.Errorf("sync job record: %w", err)
	}
	store.sequence = record.Sequence
	return record, nil
}

// Compact writes a complete checkpoint beside the live journal, syncs it,
// atomically replaces the journal, switches the live descriptor, then syncs
// the containing directory. The store lease remains held throughout.
func (store *File) Compact(ctx context.Context, records []Record) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.closed {
		return errors.New("job store closed")
	}
	if store.failed != nil {
		return fmt.Errorf("job store is unusable after a previous durable write failure: %w", store.failed)
	}
	directory := filepath.Dir(store.path)
	temporary, err := os.CreateTemp(directory, ".jobs.checkpoint-*")
	if err != nil {
		return fmt.Errorf("create job checkpoint: %w", err)
	}
	temporaryPath := temporary.Name()
	committed := false
	defer func() {
		_ = temporary.Close()
		if !committed {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(0o600); err != nil {
		return fmt.Errorf("secure job checkpoint: %w", err)
	}
	for index, record := range records {
		if err := ctx.Err(); err != nil {
			return err
		}
		record.Version = RecordVersion
		record.Sequence = uint64(index + 1)
		if err := writeRecord(temporary, record); err != nil {
			return fmt.Errorf("write job checkpoint: %w", err)
		}
	}
	if err := temporary.Sync(); err != nil {
		return fmt.Errorf("sync job checkpoint: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close job checkpoint: %w", err)
	}
	if err := replaceJournal(temporaryPath, store.path); err != nil {
		return fmt.Errorf("replace job journal: %w", err)
	}
	committed = true
	replacement, err := openJournal(store.path)
	if err != nil {
		store.failed = err
		return fmt.Errorf("reopen compacted job journal: %w", err)
	}
	if _, err := replacement.Seek(0, io.SeekEnd); err != nil {
		_ = replacement.Close()
		store.failed = err
		return fmt.Errorf("seek compacted job journal: %w", err)
	}
	previous := store.file
	store.file = replacement
	store.sequence = uint64(len(records))
	if err := syncDirectory(directory); err != nil {
		store.failed = err
		_ = previous.Close()
		return fmt.Errorf("sync compacted job store directory: %w", err)
	}
	if err := previous.Close(); err != nil {
		store.failed = err
		return fmt.Errorf("close replaced job journal: %w", err)
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
	if store.failed != nil {
		return nil, fmt.Errorf("job store is unusable after a previous durable write failure: %w", store.failed)
	}
	records, _, err := store.loadLocked()
	return records, err
}

func (store *File) loadLocked() ([]Record, int64, error) {
	if _, err := store.file.Seek(0, io.SeekStart); err != nil {
		return nil, 0, err
	}
	reader := bufio.NewReader(store.file)
	var result []Record
	var validBytes int64
	for {
		header := make([]byte, headerBytes)
		if _, err := io.ReadFull(reader, header); err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				break
			}
			return nil, validBytes, fmt.Errorf("read job record header: %w", err)
		}
		length := binary.BigEndian.Uint32(header[:4])
		if length == 0 || length > maxRecordBytes {
			return nil, validBytes, fmt.Errorf("invalid job record length %d", length)
		}
		payload := make([]byte, length)
		if _, err := io.ReadFull(reader, payload); err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				break
			}
			return nil, validBytes, fmt.Errorf("read job record: %w", err)
		}
		checksum := sha256.Sum256(payload)
		if !equalChecksum(header[4:], checksum[:]) {
			return nil, validBytes, fmt.Errorf("job record %d checksum mismatch", len(result)+1)
		}
		var record Record
		if err := json.Unmarshal(payload, &record); err != nil {
			return nil, validBytes, fmt.Errorf("decode job record %d: %w", len(result)+1, err)
		}
		if record.Version != RecordVersion || record.Sequence != uint64(len(result)+1) {
			return nil, validBytes, fmt.Errorf("invalid job record sequence/version at %d", len(result)+1)
		}
		result = append(result, record)
		validBytes += int64(headerBytes) + int64(length)
	}
	return result, validBytes, nil
}

func (store *File) Close() error {
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.closed {
		return nil
	}
	store.closed = true
	return errors.Join(store.file.Close(), store.lease.Close())
}

func writeAll(writer io.Writer, data []byte) error {
	for len(data) > 0 {
		count, err := writer.Write(data)
		if err != nil {
			return err
		}
		data = data[count:]
	}
	return nil
}

func writeRecord(writer io.Writer, record Record) error {
	payload, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("encode job record: %w", err)
	}
	if len(payload) > maxRecordBytes {
		return fmt.Errorf("job record exceeds %d bytes", maxRecordBytes)
	}
	checksum := sha256.Sum256(payload)
	header := make([]byte, headerBytes)
	binary.BigEndian.PutUint32(header[:4], uint32(len(payload)))
	copy(header[4:], checksum[:])
	if err := writeAll(writer, header); err != nil {
		return fmt.Errorf("write job record header: %w", err)
	}
	if err := writeAll(writer, payload); err != nil {
		return fmt.Errorf("write job record: %w", err)
	}
	return nil
}

func equalChecksum(left, right []byte) bool {
	if len(left) != len(right) {
		return false
	}
	var difference byte
	for index := range left {
		difference |= left[index] ^ right[index]
	}
	return difference == 0
}
