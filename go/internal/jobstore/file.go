package jobstore

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
)

const (
	journalName        = "jobs.journal"
	maxRecordBytes     = 16 << 20
	recordChunkBytes   = 8 << 20
	headerBytes        = 4 + sha256.Size
	recordChunkFrameV1 = "record-chunk-v1"
)

// recordChunkFrame keeps every physical journal frame inside the fixed
// corruption-safe envelope while allowing a logical recovery record to be any
// size supported by the configured application retention policy. The digest
// authenticates the reassembled logical record, not just each physical frame.
type recordChunkFrame struct {
	Frame          string `json:"_jobstore_frame"`
	RecordSequence uint64 `json:"record_sequence"`
	Index          uint64 `json:"index"`
	Final          bool   `json:"final"`
	Digest         string `json:"digest"`
	Data           []byte `json:"data"`
}

type pendingRecord struct {
	sequence uint64
	next     uint64
	digest   string
	data     bytes.Buffer
}

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
	if err := writeRecord(ctx, store.file, record); err != nil {
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
		if err := writeRecord(ctx, temporary, record); err != nil {
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
	var consumedBytes, validBytes int64
	var pending *pendingRecord
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
		consumedBytes += int64(headerBytes) + int64(length)
		var marker struct {
			Frame string `json:"_jobstore_frame"`
		}
		if err := json.Unmarshal(payload, &marker); err != nil {
			return nil, validBytes, fmt.Errorf("decode job frame %d: %w", len(result)+1, err)
		}
		if marker.Frame != "" {
			if marker.Frame != recordChunkFrameV1 {
				return nil, validBytes, fmt.Errorf("unsupported job frame %q", marker.Frame)
			}
			var frame recordChunkFrame
			if err := json.Unmarshal(payload, &frame); err != nil {
				return nil, validBytes, fmt.Errorf("decode job chunk %d: %w", len(result)+1, err)
			}
			if frame.RecordSequence != uint64(len(result)+1) || frame.Digest == "" || len(frame.Data) == 0 {
				return nil, validBytes, fmt.Errorf("invalid job chunk identity at record %d", len(result)+1)
			}
			if pending == nil {
				if frame.Index != 0 {
					return nil, validBytes, fmt.Errorf("job record %d does not begin with chunk zero", len(result)+1)
				}
				pending = &pendingRecord{sequence: frame.RecordSequence, digest: frame.Digest}
			}
			if frame.RecordSequence != pending.sequence || frame.Index != pending.next || frame.Digest != pending.digest {
				return nil, validBytes, fmt.Errorf("non-contiguous job chunks at record %d", len(result)+1)
			}
			_, _ = pending.data.Write(frame.Data)
			pending.next++
			if !frame.Final {
				continue
			}
			logical := pending.data.Bytes()
			digest := sha256.Sum256(logical)
			if !equalChecksum([]byte(pending.digest), []byte(hex.EncodeToString(digest[:]))) {
				return nil, validBytes, fmt.Errorf("job record %d chunk digest mismatch", len(result)+1)
			}
			var record Record
			if err := json.Unmarshal(logical, &record); err != nil {
				return nil, validBytes, fmt.Errorf("decode chunked job record %d: %w", len(result)+1, err)
			}
			if record.Version != RecordVersion || record.Sequence != uint64(len(result)+1) {
				return nil, validBytes, fmt.Errorf("invalid chunked job record sequence/version at %d", len(result)+1)
			}
			result = append(result, record)
			pending = nil
			validBytes = consumedBytes
			continue
		}
		if pending != nil {
			return nil, validBytes, fmt.Errorf("job record %d chunk sequence was interrupted", len(result)+1)
		}
		var record Record
		if err := json.Unmarshal(payload, &record); err != nil {
			return nil, validBytes, fmt.Errorf("decode job record %d: %w", len(result)+1, err)
		}
		if record.Version != RecordVersion || record.Sequence != uint64(len(result)+1) {
			return nil, validBytes, fmt.Errorf("invalid job record sequence/version at %d", len(result)+1)
		}
		result = append(result, record)
		validBytes = consumedBytes
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

func writeRecord(ctx context.Context, writer io.Writer, record Record) error {
	payload, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("encode job record: %w", err)
	}
	if len(payload) <= maxRecordBytes {
		return writeFrame(writer, payload)
	}
	digest := sha256.Sum256(payload)
	digestText := hex.EncodeToString(digest[:])
	var index uint64
	for offset := 0; offset < len(payload); offset += recordChunkBytes {
		if err := ctx.Err(); err != nil {
			return err
		}
		end := min(len(payload), offset+recordChunkBytes)
		frame, err := json.Marshal(recordChunkFrame{
			Frame: recordChunkFrameV1, RecordSequence: record.Sequence, Index: index,
			Final: end == len(payload), Digest: digestText, Data: payload[offset:end],
		})
		if err != nil {
			return fmt.Errorf("encode job record chunk %d: %w", index, err)
		}
		if len(frame) > maxRecordBytes {
			return fmt.Errorf("encoded job record chunk %d exceeds protocol envelope", index)
		}
		if err := writeFrame(writer, frame); err != nil {
			return fmt.Errorf("write job record chunk %d: %w", index, err)
		}
		index++
	}
	return nil
}

func writeFrame(writer io.Writer, payload []byte) error {
	if len(payload) == 0 || len(payload) > maxRecordBytes {
		return fmt.Errorf("job frame exceeds %d bytes", maxRecordBytes)
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
