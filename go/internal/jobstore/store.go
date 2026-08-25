// Package jobstore persists sanitized lifecycle facts needed to reconstruct
// fix jobs and reconcile side effects after a crash.
package jobstore

import (
	"context"
	"encoding/json"
	"time"

	"github.com/blater/slopwatch/internal/fix"
)

const RecordVersion = 1

type Record struct {
	Version  int             `json:"version"`
	Sequence uint64          `json:"sequence"`
	At       time.Time       `json:"at"`
	JobID    fix.JobID       `json:"job_id"`
	Revision uint64          `json:"revision"`
	Kind     string          `json:"kind"`
	Data     json.RawMessage `json:"data,omitempty"`
}

type Store interface {
	Append(context.Context, Record) (Record, error)
	Load(context.Context) ([]Record, error)
	// Compact atomically replaces the journal with the supplied complete
	// recovery records. Implementations must retain exclusive ownership while
	// replacing the journal and must not expose a partially written checkpoint.
	Compact(context.Context, []Record) error
	Close() error
}
